package production

import (
	"context"
	"fmt"
	"testing"
	"time"

	actionv1 "github.com/cylewitruk-stacks/stacks-k8s/apis/network/actions/v1alpha1"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/reorganization"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// chainRPC models ordinary competing regtest branches and receipt loss after applied effects.
type chainRPC struct {
	base                      *fakeRPC
	blocks                    map[string]actionv1.BitcoinChainPoint
	tip, original             actionv1.BitcoinChainPoint
	invalidations, cleanups   int
	fail                      string
	badTips                   bool
	tipErrorAfterInvalidation bool
	cleanupGate               func()
	invalidateGate            func()
}

func (c *chainRPC) Check(ctx context.Context, e, a string) error { return c.base.Check(ctx, e, a) }
func (c *chainRPC) Tip(context.Context, string) (actionv1.BitcoinChainPoint, error) {
	if c.tipErrorAfterInvalidation && c.invalidations > 0 && c.cleanups == 0 {
		return actionv1.BitcoinChainPoint{}, fmt.Errorf("read unavailable")
	}
	return c.tip, nil
}
func (c *chainRPC) Header(_ context.Context, _ string, hash string) (actionv1.BitcoinChainPoint, error) {
	p, ok := c.blocks[hash]
	if !ok {
		return p, fmt.Errorf("missing header")
	}
	return p, nil
}
func (c *chainRPC) HashAt(_ context.Context, _ string, height int64) (string, error) {
	p := c.tip
	for p.Height > height {
		p = c.blocks[p.PreviousBlockHash]
	}
	if p.Height != height {
		return "", fmt.Errorf("height unavailable")
	}
	return p.Hash, nil
}
func (c *chainRPC) CheckTips(context.Context, string) error {
	if c.badTips {
		return fmt.Errorf("invalid existing tip")
	}
	return nil
}
func (c *chainRPC) Invalidate(_ context.Context, _ string, hash, _ string) error {
	c.invalidations++
	pivot := c.blocks[hash]
	c.tip = c.blocks[pivot.PreviousBlockHash]
	if c.invalidateGate != nil {
		c.invalidateGate()
	}
	if c.fail == "Invalidate" {
		return fmt.Errorf("lost invalidation receipt")
	}
	return nil
}
func (c *chainRPC) Reconsider(context.Context, string, string, string) error {
	c.cleanups++
	if c.cleanupGate != nil {
		c.cleanupGate()
	}
	if c.original.Chainwork > c.tip.Chainwork {
		c.tip = c.original
	}
	if c.fail == "Reconsider" {
		return fmt.Errorf("lost cleanup receipt")
	}
	return nil
}
func (c *chainRPC) Generate(ctx context.Context, e, a, id string) (string, error) {
	hash, err := c.base.Generate(ctx, e, a, id)
	if err != nil {
		return "", err
	}
	p := actionv1.BitcoinChainPoint{Hash: hash, Height: c.tip.Height + 1, PreviousBlockHash: c.tip.Hash, Chainwork: fmt.Sprintf("%064x", c.tip.Height+2)}
	c.blocks[hash] = p
	c.tip = p
	if c.fail == "Generate" {
		return "", fmt.Errorf("lost generation receipt")
	}
	return hash, nil
}

// reorgFixture creates an admitted original chain and one small replacement request.
func reorgFixture(t *testing.T) (*productionFixture, *chainRPC, *actionv1.BitcoinReorganization) {
	t.Helper()
	f := fixture(t)
	f.r.ReorganizationEnabled = true
	rpc := &chainRPC{base: f.rpc, blocks: map[string]actionv1.BitcoinChainPoint{}}
	for height := int64(0); height < 4; height++ {
		p := actionv1.BitcoinChainPoint{Hash: fmt.Sprintf("%064x", 100+height), Height: height, Chainwork: fmt.Sprintf("%064x", height+1)}
		if height > 0 {
			p.PreviousBlockHash = rpc.tip.Hash
		}
		rpc.blocks[p.Hash] = p
		rpc.tip = p
	}
	rpc.original = rpc.tip
	f.r.RPC = rpc
	a := &actionv1.BitcoinReorganization{ObjectMeta: metav1.ObjectMeta{Name: "replace", Namespace: f.parent.Namespace, UID: types.UID("replace"), Generation: 1, CreationTimestamp: metav1.NewTime(f.now)}, Spec: actionv1.BitcoinReorganizationSpec{NetworkRef: actionv1.LocalReference{Name: f.parent.Name}, BitcoinNodeRef: actionv1.LocalReference{Name: f.actor.Name}, Depth: 2, Address: f.policy.Spec.Policy.Address, Timeout: metav1.Duration{Duration: time.Minute}, BoundaryPolicy: actionv1.ReorganizationBoundaryPolicy{AllowEpochBoundaryCrossing: true, AllowRewardCycleBoundaryCrossing: true, AllowPreparePhaseBoundaryCrossing: true}}}
	if err := f.r.Create(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	return f, rpc, a
}

// reorgStep projects one ledger observation and returns false after ordinary deletion completes.
func reorgStep(t *testing.T, f *productionFixture, a *actionv1.BitcoinReorganization) bool {
	t.Helper()
	r := &reorganization.Reconciler{Client: f.r.Client, APIReader: f.r.APIReader, Now: func() time.Time { return f.now }}
	if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(a)}); err != nil {
		t.Fatal(err)
	}
	err := f.r.Get(context.Background(), client.ObjectKeyFromObject(a), a)
	if apierrors.IsNotFound(err) {
		return false
	}
	if err != nil {
		t.Fatal(err)
	}
	return true
}

// admitReorg requires both action admission writes before any invalidation.
func admitReorg(t *testing.T, f *productionFixture, a *actionv1.BitcoinReorganization) {
	t.Helper()
	f.reconcile(t, context.Background())
	reorgStep(t, f, a)
	f.reconcile(t, context.Background())
	reorgStep(t, f, a)
	if a.Status.Phase != "Admitted" {
		t.Fatalf("not admitted: %#v", a.Status)
	}
}

// finishReorg progresses bounded test time until the reservation is safely released.
func finishReorg(t *testing.T, f *productionFixture, a *actionv1.BitcoinReorganization) {
	t.Helper()
	for i := 0; i < 30; i++ {
		f.reconcile(t, context.Background())
		reorgStep(t, f, a)
		if f.ledger(t).Status.Reorganization == nil {
			return
		}
		f.now = f.now.Add(time.Second)
	}
	t.Fatal("reservation did not finish")
}

func TestReorganizationAccountsBranchAndCleanupBeforeLatestBaseline(t *testing.T) {
	f, rpc, a := reorgFixture(t)
	admitReorg(t, f, a)
	if rpc.invalidations != 0 {
		t.Fatal("admission performed a mutation")
	}
	if err := f.r.Get(context.Background(), client.ObjectKeyFromObject(f.parent), f.parent); err != nil {
		t.Fatal(err)
	}
	f.parent.Spec.BitcoinBlockProduction.Paused = true
	if err := f.r.Update(context.Background(), f.parent); err != nil {
		t.Fatal(err)
	}
	finishReorg(t, f, a)
	if a.Status.Phase != "Completed" || a.Status.BlocksGenerated != 3 || len(a.Status.ReplacementBlockHashes) != 3 || !a.Status.CleanupAcknowledged || a.Status.FinalChain == nil || a.Status.FinalChain.Chainwork <= rpc.original.Chainwork || rpc.invalidations != 1 || rpc.cleanups != 1 || f.ledger(t).Status.BlocksProduced != 0 {
		t.Fatalf("incorrect branch completion: %#v", a.Status)
	}
	f.reconcile(t, context.Background())
	if f.ledger(t).Status.Phase != "Paused" || f.rpc.sends != 3 {
		t.Fatal("latest baseline pause lost")
	}
}

func TestReorganizationKnownPartialDeadlineCompensates(t *testing.T) {
	f, rpc, a := reorgFixture(t)
	admitReorg(t, f, a)
	for f.ledger(t).Status.Reorganization.BlocksGenerated == 0 {
		f.reconcile(t, context.Background())
		reorgStep(t, f, a)
		f.now = f.now.Add(time.Second)
	}
	f.now = a.CreationTimestamp.Add(a.Spec.Timeout.Duration)
	finishReorg(t, f, a)
	if a.Status.Phase != "Failed" || a.Status.BlocksGenerated != 1 || rpc.cleanups != 1 || !a.Status.CleanupAcknowledged || f.rpc.sends != 1 {
		t.Fatal("partial deadline skipped cleanup or generated more blocks")
	}
}

func TestReorganizationDeletionAfterInvalidationCompensates(t *testing.T) {
	f, rpc, a := reorgFixture(t)
	admitReorg(t, f, a)
	f.reconcile(t, context.Background())
	reorgStep(t, f, a)
	if err := f.r.Delete(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	finishReorg(t, f, a)
	if rpc.invalidations != 1 || rpc.cleanups != 1 || f.rpc.sends != 0 {
		t.Fatal("cancellation did not compensate exactly once")
	}
	if err := f.r.Get(context.Background(), client.ObjectKeyFromObject(a), a); !apierrors.IsNotFound(err) {
		t.Fatalf("cancelled action still retained: %v", err)
	}
}

func TestReorganizationUnknownCallsForbidCleanupAndBaseline(t *testing.T) {
	for _, step := range []string{"Invalidate", "Generate", "Reconsider"} {
		t.Run(step, func(t *testing.T) {
			f, rpc, a := reorgFixture(t)
			rpc.fail = step
			admitReorg(t, f, a)
			for i := 0; i < 25; i++ {
				f.reconcile(t, context.Background())
				reorgStep(t, f, a)
				if a.Status.Phase == "Inconclusive" {
					break
				}
				f.now = f.now.Add(time.Second)
			}
			if a.Status.Phase != "Inconclusive" || f.ledger(t).Status.DispatchState != "Armed" {
				t.Fatal("unknown effect did not retain exclusion")
			}
			sends, cleanups, invalidations := f.rpc.sends, rpc.cleanups, rpc.invalidations
			replacement := *f.r
			replacement.ProcessNonce = "new"
			replacement.collectors = newCollectorPool(&replacement, collectorLimit, collectorDrain)
			f.r = &replacement
			f.now = f.now.Add(2 * time.Minute)
			if err := f.r.Delete(context.Background(), a); err != nil {
				t.Fatal(err)
			}
			f.reconcile(t, context.Background())
			reorgStep(t, f, a)
			if f.rpc.sends != sends || rpc.cleanups != cleanups || rpc.invalidations != invalidations || f.ledger(t).Status.Reorganization == nil || len(a.Finalizers) == 0 {
				t.Fatal("unknown call reopened on restart/deadline/deletion")
			}
		})
	}
}

func TestReorganizationKnownRestartAndReadFailureCleanup(t *testing.T) {
	f, rpc, a := reorgFixture(t)
	admitReorg(t, f, a)
	f.reconcile(t, context.Background())
	reorgStep(t, f, a)
	replacement := *f.r
	replacement.ProcessNonce = "next"
	replacement.collectors = newCollectorPool(&replacement, collectorLimit, collectorDrain)
	f.r = &replacement
	rpc.tipErrorAfterInvalidation = true
	finishReorg(t, f, a)
	if rpc.invalidations != 1 || rpc.cleanups != 1 || a.Status.Phase != "Failed" || !a.Status.CleanupAcknowledged {
		t.Fatal("known idle restart repeated invalidation or skipped compensation")
	}
}

func TestReorganizationChangedRuntimeAndExpiredCleanupStayClosed(t *testing.T) {
	for _, mode := range []string{"runtime", "expiry"} {
		t.Run(mode, func(t *testing.T) {
			f, rpc, a := reorgFixture(t)
			admitReorg(t, f, a)
			f.reconcile(t, context.Background())
			reorgStep(t, f, a)
			if mode == "runtime" {
				if err := f.r.Get(context.Background(), client.ObjectKeyFromObject(f.pod), f.pod); err != nil {
					t.Fatal(err)
				}
				f.pod.Status.ContainerStatuses[0].ContainerID = "containerd://other"
				if err := f.r.Status().Update(context.Background(), f.pod); err != nil {
					t.Fatal(err)
				}
			} else {
				f.now = f.now.Add(2 * time.Minute)
			}
			for i := 0; i < 3; i++ {
				f.reconcile(t, context.Background())
				reorgStep(t, f, a)
			}
			if rpc.cleanups != 0 || f.rpc.sends != 0 || a.Status.Phase != "Inconclusive" || f.ledger(t).Status.Reorganization == nil {
				t.Fatal("unsafe cleanup granted")
			}
		})
	}
}

func TestReorganizationLateInvalidationReceiptAllowsOnlyCompensation(t *testing.T) {
	f, rpc, a := reorgFixture(t)
	admitReorg(t, f, a)
	entered, release := make(chan struct{}), make(chan struct{})
	rpc.invalidateGate = func() { close(entered); <-release }
	if _, err := f.r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(f.policy)}); err != nil {
		t.Fatal(err)
	}
	<-entered
	f.now = a.CreationTimestamp.Add(a.Spec.Timeout.Duration + time.Second)
	reorgStep(t, f, a)
	if a.Status.Phase != "Inconclusive" {
		t.Fatal("late authorization not reported uncertain")
	}
	finished := a.Status.FinishedAt.DeepCopy()
	close(release)
	f.waitCollectors(t)
	finishReorg(t, f, a)
	if a.Status.Phase != "Inconclusive" || !a.Status.FinishedAt.Equal(finished) || rpc.cleanups != 1 || f.rpc.sends != 0 || !a.Status.CleanupAcknowledged {
		t.Fatal("late receipt revised outcome or failed to compensate")
	}
}

func TestReorganizationEligibilityAndCrossKindOrdering(t *testing.T) {
	f, _, a := reorgFixture(t)
	f.now = f.now.Add(time.Second)
	actionFixture(t, f, "generation", 1)
	f.reconcile(t, context.Background())
	if f.ledger(t).Status.Reorganization == nil || f.ledger(t).Status.Action != nil || f.ledger(t).Status.Reorganization.UID != string(a.UID) {
		t.Fatal("older eligible reorganization did not reserve exclusively")
	}
	g, rpc, _ := reorgFixture(t)
	rpc.badTips = true
	g.now = g.now.Add(time.Second)
	b := actionFixture(t, g, "eligible", 1)
	g.reconcile(t, context.Background())
	if g.ledger(t).Status.Action == nil || g.ledger(t).Status.Action.UID != string(b.UID) || g.ledger(t).Status.Reorganization != nil {
		t.Fatal("ineligible reorganization starved generation")
	}
}

func TestReorganizationAndGenerationCompeteThroughOneCAS(t *testing.T) {
	f, _, _ := reorgFixture(t)
	f.now = f.now.Add(time.Second)
	actionFixture(t, f, "other-kind", 1)
	first, stale := f.ledger(t), f.ledger(t)
	if selected, err := f.r.selectAction(context.Background(), first, f.parent); err != nil || !selected {
		t.Fatalf("first reservation: %v", err)
	}
	competing := *f.r
	competing.ReorganizationEnabled = false
	if _, err := competing.selectAction(context.Background(), stale, f.parent); !apierrors.IsConflict(err) {
		t.Fatalf("competing reservation did not conflict: %v", err)
	}
	if f.ledger(t).Status.Reorganization == nil || f.ledger(t).Status.Action != nil || f.rpc.sends != 0 {
		t.Fatal("competing kind stole reservation")
	}
}

func TestReorganizationLostArmAndAccountingAcknowledgements(t *testing.T) {
	for _, state := range []string{"Armed", "Idle"} {
		t.Run(state, func(t *testing.T) {
			f, rpc, a := reorgFixture(t)
			admitReorg(t, f, a)
			f.r.Client = &uncertainStatusClient{Client: f.r.Client, state: state}
			if state == "Armed" {
				if _, err := f.r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(f.policy)}); err == nil {
					t.Fatal("lost arm acknowledgement not modeled")
				}
				f.reconcile(t, context.Background())
				reorgStep(t, f, a)
				if rpc.invalidations != 0 || f.ledger(t).Status.Reorganization == nil || a.Status.Phase != "Inconclusive" {
					t.Fatal("unknown arm resent")
				}
			} else {
				finishReorg(t, f, a)
				if rpc.invalidations != 1 || rpc.cleanups != 1 || a.Status.BlocksGenerated != 3 {
					t.Fatal("lost accounting acknowledgement repeated a step")
				}
			}
		})
	}
}

func TestCleanupReceiptHasItsSeparateRecoveryWindow(t *testing.T) {
	f, rpc, a := reorgFixture(t)
	admitReorg(t, f, a)
	f.reconcile(t, context.Background())
	reorgStep(t, f, a)
	f.now = a.CreationTimestamp.Add(a.Spec.Timeout.Duration)
	f.reconcile(t, context.Background())
	reorgStep(t, f, a)
	entered, release := make(chan struct{}), make(chan struct{})
	rpc.cleanupGate = func() { close(entered); <-release }
	if _, err := f.r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(f.policy)}); err != nil {
		t.Fatal(err)
	}
	<-entered
	reorgStep(t, f, a)
	if a.Status.Phase != "Recovering" {
		t.Fatal("normal compensation window prematurely terminal")
	}
	f.now = f.now.Add(31 * time.Second)
	reorgStep(t, f, a)
	if a.Status.Phase != "Inconclusive" {
		t.Fatal("exhausted compensation window hid uncertainty")
	}
	close(release)
	f.waitCollectors(t)
	finishReorg(t, f, a)
	if a.Status.Phase != "Inconclusive" || !a.Status.CleanupAcknowledged {
		t.Fatal("late compensation revised outcome or lost its receipt")
	}
}
