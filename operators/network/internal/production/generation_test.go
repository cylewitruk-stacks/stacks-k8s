package production

import (
	"context"
	"fmt"
	"testing"
	"time"

	actionv1 "github.com/cylewitruk-stacks/stacks-k8s/apis/network/actions/v1alpha1"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/action/controllers/generation"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// actionFixture creates one immutable request against the admitted test actor.
func actionFixture(t *testing.T, f *productionFixture, name string, count int32) *actionv1.BitcoinBlockGeneration {
	t.Helper()
	f.r.ActionsEnabled = true
	a := &actionv1.BitcoinBlockGeneration{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: f.parent.Namespace, UID: types.UID(name), Generation: 1, CreationTimestamp: metav1.NewTime(f.now)}, Spec: actionv1.BitcoinBlockGenerationSpec{NetworkRef: actionv1.LocalReference{Name: f.parent.Name}, BitcoinNodeRef: actionv1.LocalReference{Name: f.actor.Name}, Count: count, Cadence: actionv1.GenerationCadence{Mode: "Fixed", IntervalSeconds: 1}, Address: f.policy.Spec.Policy.Address, Timeout: metav1.Duration{Duration: time.Minute}}}
	if err := f.r.Create(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	return a
}

// actionStep synchronizes action lifecycle facts from the production ledger.
func actionStep(t *testing.T, f *productionFixture, a *actionv1.BitcoinBlockGeneration) {
	t.Helper()
	r := &generation.Reconciler{Client: f.r.Client, APIReader: f.r.APIReader, Now: func() time.Time { return f.now }}
	if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(a)}); err != nil {
		t.Fatal(err)
	}
	if err := f.r.Get(context.Background(), client.ObjectKeyFromObject(a), a); err != nil {
		t.Fatal(err)
	}
}

// admitAction commits both the executor reservation and action admission before dispatch.
func admitAction(t *testing.T, f *productionFixture, a *actionv1.BitcoinBlockGeneration) {
	t.Helper()
	f.reconcile(t, context.Background())
	actionStep(t, f, a)
	actionStep(t, f, a)
	if a.Status.Phase != "Admitted" || f.rpc.sends != 0 {
		t.Fatal("action did not admit before its first side effect")
	}
}
func TestFiniteGenerationExcludesBaselineAndResumesLatestPolicy(t *testing.T) {
	f := fixture(t)
	a := actionFixture(t, f, "finite", 2)
	admitAction(t, f, a)
	f.reconcile(t, context.Background())
	actionStep(t, f, a)
	if a.Status.BlocksGenerated != 1 || f.ledger(t).Status.BlocksProduced != 0 {
		t.Fatal("action receipt attributed to baseline")
	}
	// A concurrent baseline pause/cadence edit does not alter the admitted action.
	if err := f.r.Get(context.Background(), client.ObjectKeyFromObject(f.parent), f.parent); err != nil {
		t.Fatal(err)
	}
	f.parent.Spec.BitcoinBlockProduction.Paused = true
	f.parent.Spec.BitcoinBlockProduction.IntervalSeconds = 30
	if err := f.r.Update(context.Background(), f.parent); err != nil {
		t.Fatal(err)
	}
	f.now = f.now.Add(time.Second)
	f.reconcile(t, context.Background())
	actionStep(t, f, a)
	if a.Status.Phase != "Completed" || a.Status.BlocksGenerated != 2 || a.Status.LastBlockHash == "" {
		t.Fatalf("finite progress: %#v", a.Status)
	}
	f.reconcile(t, context.Background())
	f.now = f.now.Add(time.Minute)
	f.reconcile(t, context.Background())
	if f.rpc.sends != 2 || f.ledger(t).Status.Action != nil || f.ledger(t).Status.Phase != "Paused" {
		t.Fatal("latest paused baseline was not preserved")
	}
}
func TestInvalidWaitingActionsDoNotStarveBaselineOrEligibleAction(t *testing.T) {
	f := fixture(t)
	invalid := actionFixture(t, f, "invalid", 1)
	invalid.Spec.BitcoinNodeRef.Name = "absent"
	if err := f.r.Update(context.Background(), invalid); err != nil {
		t.Fatal(err)
	}
	f.reconcile(t, context.Background())
	if f.ledger(t).Status.BlocksProduced != 1 || f.ledger(t).Status.Action != nil {
		t.Fatal("unavailable request stalled baseline")
	}
	valid := actionFixture(t, f, "valid", 1)
	f.reconcile(t, context.Background())
	actionStep(t, f, valid)
	actionStep(t, f, valid)
	if f.ledger(t).Status.Action == nil || f.ledger(t).Status.Action.UID != string(valid.UID) {
		t.Fatal("invalid waiting action starved eligible request")
	}
}
func TestFiniteAmbiguityRemainsExcludedAcrossRestartAndDeadline(t *testing.T) {
	f := fixture(t)
	a := actionFixture(t, f, "lost", 2)
	admitAction(t, f, a)
	f.rpc.generate = func(context.Context) error { return fmt.Errorf("lost response") }
	f.reconcile(t, context.Background())
	actionStep(t, f, a)
	if a.Status.Phase != "Inconclusive" || f.ledger(t).Status.DispatchState != "Armed" {
		t.Fatal("ambiguous call was not retained")
	}
	replacement := *f.r
	replacement.ProcessNonce = "replacement"
	replacement.collectors = newCollectorPool(&replacement, collectorLimit, collectorDrain)
	f.r = &replacement
	f.now = f.now.Add(2 * time.Minute)
	f.reconcile(t, context.Background())
	actionStep(t, f, a)
	if f.rpc.sends != 1 || f.ledger(t).Status.Action == nil || a.Status.Phase != "Inconclusive" {
		t.Fatal("restart/deadline released or retried ambiguous work")
	}
}
func TestFiniteDeadlineWithKnownPartialProgressReleasesReservation(t *testing.T) {
	f := fixture(t)
	a := actionFixture(t, f, "partial", 3)
	admitAction(t, f, a)
	f.reconcile(t, context.Background())
	actionStep(t, f, a)
	f.now = f.now.Add(time.Minute)
	f.reconcile(t, context.Background())
	actionStep(t, f, a)
	f.reconcile(t, context.Background())
	if a.Status.Phase != "Failed" || a.Status.BlocksGenerated != 1 || f.ledger(t).Status.Action != nil || f.rpc.sends != 1 {
		t.Fatal("known partial timeout did not stop and release")
	}
}
func TestAdmissionWriteMustPrecedeAnyActionRPC(t *testing.T) {
	f := fixture(t)
	a := actionFixture(t, f, "not-admitted", 1)
	f.reconcile(t, context.Background())
	f.reconcile(t, context.Background())
	if f.rpc.sends != 0 || f.ledger(t).Status.Action == nil {
		t.Fatal("executor did not wait for action admission")
	}
	actionStep(t, f, a)
	f.reconcile(t, context.Background())
	if f.rpc.sends != 0 {
		t.Fatal("finalizer alone authorized execution")
	}
}

func TestFiniteLateReceiptPreservesOutcomeAndReleasesOnlyAfterAcknowledgement(t *testing.T) {
	f := fixture(t)
	a := actionFixture(t, f, "late", 2)
	admitAction(t, f, a)
	entered, release := make(chan struct{}), make(chan struct{})
	f.rpc.generate = func(context.Context) error { close(entered); <-release; return nil }
	_, err := f.r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(f.policy)})
	if err != nil {
		t.Fatal(err)
	}
	<-entered
	f.now = f.now.Add(2 * time.Minute)
	actionStep(t, f, a)
	if a.Status.Phase != "Inconclusive" || f.ledger(t).Status.Action == nil {
		t.Fatal("deadline released outstanding work")
	}
	finished := a.Status.FinishedAt.DeepCopy()
	close(release)
	f.waitCollectors(t)
	// A durable receipt does not release the reservation until the action has copied it.
	f.reconcile(t, context.Background())
	if f.ledger(t).Status.Action == nil {
		t.Fatal("receipt released before action acknowledgement")
	}
	actionStep(t, f, a)
	f.reconcile(t, context.Background())
	if a.Status.Phase != "Inconclusive" || !a.Status.FinishedAt.Equal(finished) || a.Status.BlocksGenerated != 1 || f.ledger(t).Status.Action != nil {
		t.Fatal("late receipt revised outcome or failed to release acknowledged work")
	}
}

func TestFiniteDeletionRetainsFinalizerUntilReceiptAndRelease(t *testing.T) {
	f := fixture(t)
	a := actionFixture(t, f, "cancel", 3)
	admitAction(t, f, a)
	entered, release := make(chan struct{}), make(chan struct{})
	f.rpc.generate = func(context.Context) error { close(entered); <-release; return nil }
	_, err := f.r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(f.policy)})
	if err != nil {
		t.Fatal(err)
	}
	<-entered
	if err := f.r.Delete(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	actionStep(t, f, a)
	if a.Status.Phase != "Inconclusive" || len(a.Finalizers) == 0 {
		t.Fatal("deletion lost exclusion")
	}
	close(release)
	f.waitCollectors(t)
	actionStep(t, f, a)
	f.reconcile(t, context.Background())
	r := &generation.Reconciler{Client: f.r.Client, APIReader: f.r.APIReader, Now: func() time.Time { return f.now }}
	if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(a)}); err != nil {
		t.Fatal(err)
	}
	if err := f.r.Get(context.Background(), client.ObjectKeyFromObject(a), a); !apierrors.IsNotFound(err) {
		t.Fatalf("cancelled action retained after receipt: %v", err)
	}
	if f.rpc.sends != 1 || f.ledger(t).Status.Action != nil {
		t.Fatal("cancellation dispatched more work")
	}
}

func TestFiniteRestartBetweenReceiptsAndCurrentIdentityChange(t *testing.T) {
	f := fixture(t)
	a := actionFixture(t, f, "restart", 3)
	admitAction(t, f, a)
	f.reconcile(t, context.Background())
	actionStep(t, f, a)
	replacement := *f.r
	replacement.ProcessNonce = "next-process"
	replacement.collectors = newCollectorPool(&replacement, collectorLimit, collectorDrain)
	f.r = &replacement
	f.now = f.now.Add(time.Second)
	f.reconcile(t, context.Background())
	actionStep(t, f, a)
	if a.Status.BlocksGenerated != 2 {
		t.Fatal("idle reservation did not survive restart")
	}
	if err := f.r.Get(context.Background(), client.ObjectKeyFromObject(f.pod), f.pod); err != nil {
		t.Fatal(err)
	}
	f.pod.Status.ContainerStatuses[0].ContainerID = "containerd://replacement"
	if err := f.r.Status().Update(context.Background(), f.pod); err != nil {
		t.Fatal(err)
	}
	f.now = f.now.Add(time.Second)
	f.reconcile(t, context.Background())
	actionStep(t, f, a)
	if a.Status.Phase != "Inconclusive" || f.rpc.sends != 2 {
		t.Fatal("action followed replacement process")
	}
	f.reconcile(t, context.Background())
	if f.ledger(t).Status.Action != nil {
		t.Fatal("known completed work unnecessarily retained")
	}
}

func TestFiniteQueueLimitAndEligibleOrdering(t *testing.T) {
	f := fixture(t)
	actionFixture(t, f, "z-first", 1)
	f.now = f.now.Add(time.Second)
	actionFixture(t, f, "a-second", 1)
	f.reconcile(t, context.Background())
	if f.ledger(t).Status.Action.Name != "z-first" {
		t.Fatal("queue ignored creation order")
	}
	g := fixture(t)
	for i := 0; i < 65; i++ {
		actionFixture(t, g, fmt.Sprintf("queued-%02d", i), 1)
	}
	g.reconcile(t, context.Background())
	if g.ledger(t).Status.Action != nil || g.ledger(t).Status.BlocksProduced != 1 {
		t.Fatal("unsupported oversized queue blocked baseline or scheduled truncated inventory")
	}
}
