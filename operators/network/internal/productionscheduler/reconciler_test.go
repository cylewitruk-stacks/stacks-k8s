package productionscheduler

import (
	"context"
	"fmt"
	bitcoinv1 "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha1"
	networkv1 "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"testing"
	"time"
)

func TestWeightedSelectionUsesDeclaredShares(t *testing.T) {
	targets := []bitcoinv1.ProductionTarget{{Weight: 1}, {Weight: 3}, {Weight: 2}}
	counts := make([]int, 3)
	for draw := 0; draw < 6; draw++ {
		i, err := choose(targets, func(n int) int {
			if n != 6 {
				t.Fatal(n)
			}
			return draw
		})
		if err != nil {
			t.Fatal(err)
		}
		counts[i]++
	}
	if counts[0] != 1 || counts[1] != 3 || counts[2] != 2 {
		t.Fatal(counts)
	}
	for _, weight := range []int32{0, -1, 1001} {
		if _, err := choose([]bitcoinv1.ProductionTarget{{Weight: weight}}, func(int) int { return 0 }); err == nil {
			t.Fatal("invalid weight accepted")
		}
	}
}

type schedulerFixture struct {
	r    *Reconciler
	n    *networkv1.StacksNetwork
	p    *bitcoinv1.BitcoinBlockProduction
	now  time.Time
	draw int
}

func scheduler(t *testing.T) *schedulerFixture {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := networkv1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := bitcoinv1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	f := &schedulerFixture{now: time.Unix(1000, 0)}
	policy := bitcoinv1.ProductionPolicy{IntervalSeconds: 5, Targets: []bitcoinv1.ProductionTarget{{Name: "a", Weight: 1, Address: "mipcBbFg9gMiCh81Kj8tqqdgoZub1ZJRfn"}, {Name: "b", Weight: 3, Address: "mipcBbFg9gMiCh81Kj8tqqdgoZub1ZJRfn"}}}
	f.n = &networkv1.StacksNetwork{ObjectMeta: metav1.ObjectMeta{Name: "net", Namespace: "test", UID: "network"}, Spec: networkv1.StacksNetworkSpec{BitcoinBlockProduction: policy.DeepCopy()}, Status: networkv1.StacksNetworkStatus{BitcoinProductionUID: "root"}}
	f.p = &bitcoinv1.BitcoinBlockProduction{ObjectMeta: metav1.ObjectMeta{Name: "net", Namespace: "test", UID: "root", Generation: 1, Finalizers: []string{finalizer}}, Spec: bitcoinv1.BitcoinBlockProductionSpec{NetworkName: "net", NetworkUID: "network", Policy: policy}}
	if err := controllerutil.SetControllerReference(f.n, f.p, scheme); err != nil {
		t.Fatal(err)
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(f.p, f.n, &bitcoinv1.BitcoinProductionTarget{}).WithObjects(f.n, f.p).WithInterceptorFuncs(interceptor.Funcs{Create: func(ctx context.Context, c client.WithWatch, o client.Object, opts ...client.CreateOption) error {
		o.SetUID(types.UID(o.GetName() + "-uid"))
		return c.Create(ctx, o, opts...)
	}}).Build()
	f.r = &Reconciler{Client: c, APIReader: c, Now: func() time.Time { return f.now }, Draw: func(int) int { return f.draw }}
	return f
}
func (f *schedulerFixture) step(t *testing.T) {
	t.Helper()
	if _, err := f.r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(f.p)}); err != nil {
		t.Fatal(err)
	}
	if err := f.r.Get(context.Background(), client.ObjectKeyFromObject(f.p), f.p); err != nil {
		t.Fatal(err)
	}
}

func TestCadenceRestartAndNoCatchup(t *testing.T) {
	f := scheduler(t)
	f.step(t)
	if f.p.Status.Opportunities != 0 || len(f.p.Status.Targets) != 2 {
		t.Fatal(f.p.Status)
	}
	f.now = f.now.Add(5 * time.Second)
	f.step(t)
	f.step(t)
	if f.p.Status.Opportunities != 1 || f.p.Ledger("a").Offered != 1 {
		t.Fatal(f.p.Status)
	}
	// Restart retains the stored due time, with no process-local scheduling state.
	f.r = &Reconciler{Client: f.r.Client, APIReader: f.r.APIReader, Now: func() time.Time { return f.now }, Draw: func(int) int { return 3 }}
	f.now = f.now.Add(time.Hour)
	f.step(t)
	f.step(t)
	if f.p.Status.Opportunities != 2 || f.p.Ledger("b").Offered != 1 || !f.p.Status.NextOpportunityAt.Equal(&metav1.MicroTime{Time: f.now.Add(5 * time.Second)}) {
		t.Fatal(f.p.Status)
	}
}

func TestUnavailableSelectionIsNotRedistributed(t *testing.T) {
	f := scheduler(t)
	f.step(t)
	target := &bitcoinv1.BitcoinProductionTarget{}
	key := client.ObjectKey{Namespace: "test", Name: "net-a"}
	if err := f.r.Get(context.Background(), key, target); err != nil {
		t.Fatal(err)
	}
	if err := f.r.Delete(context.Background(), target); err != nil {
		t.Fatal(err)
	}
	f.now = f.now.Add(5 * time.Second)
	f.step(t)
	if f.p.Ledger("a").Offered != 1 || f.p.Ledger("b").Offered != 0 {
		t.Fatal("missing target silently redistributed")
	}
	if err := f.r.Get(context.Background(), key, target); err == nil {
		t.Fatal("missing pinned ledger recreated")
	}
	f.draw = 1
	f.now = f.now.Add(5 * time.Second)
	f.step(t)
	if f.p.Ledger("b").Offered != 1 {
		t.Fatal("missing target blocked healthy target")
	}
}

func TestRemovalAndReaddRetainsArmedLedger(t *testing.T) {
	f := scheduler(t)
	f.step(t)
	ctx := context.Background()
	target := &bitcoinv1.BitcoinProductionTarget{}
	if err := f.r.Get(ctx, client.ObjectKey{Namespace: "test", Name: "net-a"}, target); err != nil {
		t.Fatal(err)
	}
	target.Status.DispatchState = "Armed"
	target.Status.DispatchID = "unresolved"
	if err := f.r.Status().Update(ctx, target); err != nil {
		t.Fatal(err)
	}
	uid := target.UID
	original := f.p.Spec.Policy.DeepCopy()
	update := func(policy bitcoinv1.ProductionPolicy) {
		t.Helper()
		f.n.Spec.BitcoinBlockProduction = policy.DeepCopy()
		if err := f.r.Update(ctx, f.n); err != nil {
			t.Fatal(err)
		}
		f.p.Spec.Policy = policy
		f.p.Generation++
		if err := f.r.Update(ctx, f.p); err != nil {
			t.Fatal(err)
		}
		f.step(t)
	}
	removed := original.DeepCopy()
	removed.Targets = removed.Targets[1:]
	update(*removed)
	update(*original)
	if err := f.r.Get(ctx, client.ObjectKeyFromObject(target), target); err != nil {
		t.Fatal(err)
	}
	if target.UID != uid || target.Status.DispatchID != "unresolved" || f.p.Ledger("a").UID != string(uid) {
		t.Fatal("target identity or uncertainty reset")
	}
}

func TestLostSelectionAcknowledgementDoesNotResample(t *testing.T) {
	f := scheduler(t)
	f.step(t)
	f.now = f.now.Add(5 * time.Second)
	original := f.r.Client
	lost := false
	f.r.Client = interceptor.NewClient(original.(client.WithWatch), interceptor.Funcs{SubResourcePatch: func(ctx context.Context, c client.Client, sub string, o client.Object, patch client.Patch, opts ...client.SubResourcePatchOption) error {
		err := c.SubResource(sub).Patch(ctx, o, patch, opts...)
		if err == nil && !lost {
			lost = true
			return fmt.Errorf("lost acknowledgement")
		}
		return err
	}})
	if _, err := f.r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(f.p)}); err == nil {
		t.Fatal("expected lost acknowledgement")
	}
	f.draw = 3
	f.step(t)
	if f.p.Status.Opportunities != 1 || f.p.Ledger("a").Offered != 1 || f.p.Ledger("b").Offered != 0 {
		t.Fatal("resampled committed decision")
	}
}

func TestRetainedIdentityBoundBlocksUnsupportedPolicy(t *testing.T) {
	f := scheduler(t)
	f.step(t)
	for len(f.p.Status.Targets) < 16 {
		i := len(f.p.Status.Targets)
		f.p.Status.Targets = append(f.p.Status.Targets, bitcoinv1.TargetLedger{Name: fmt.Sprintf("retired-%d", i), ResourceName: fmt.Sprintf("net-retired-%d", i), UID: fmt.Sprintf("uid-%d", i)})
	}
	ctx := context.Background()
	if err := f.r.Status().Update(ctx, f.p); err != nil {
		t.Fatal(err)
	}
	f.n.Spec.BitcoinBlockProduction.Targets[0].Name = "new-target"
	if err := f.r.Update(ctx, f.n); err != nil {
		t.Fatal(err)
	}
	f.p.Spec.Policy = *f.n.Spec.BitcoinBlockProduction.DeepCopy()
	f.p.Generation++
	if err := f.r.Update(ctx, f.p); err != nil {
		t.Fatal(err)
	}
	f.now = f.now.Add(time.Hour)
	f.step(t)
	if f.p.Status.Phase != "Blocked" || f.p.Status.Opportunities != 0 || len(f.p.Status.Targets) != 16 {
		t.Fatal("unsupported update escaped lifetime bound")
	}
}

func TestPolicyEditRestartsIntervalAndInvalidatesOldSelection(t *testing.T) {
	f := scheduler(t)
	f.step(t)
	f.now = f.now.Add(5 * time.Second)
	f.step(t)
	old := f.p.Ledger("a").Opportunity.DeepCopy()
	ctx := context.Background()
	f.n.Spec.BitcoinBlockProduction.IntervalSeconds = 30
	if err := f.r.Update(ctx, f.n); err != nil {
		t.Fatal(err)
	}
	f.p.Spec.Policy = *f.n.Spec.BitcoinBlockProduction.DeepCopy()
	f.p.Generation++
	if err := f.r.Update(ctx, f.p); err != nil {
		t.Fatal(err)
	}
	f.now = f.now.Add(5 * time.Second)
	f.step(t)
	if f.p.Status.Opportunities != 1 || f.p.Status.ObservedGeneration == old.PolicyGeneration || !f.p.Status.NextOpportunityAt.Equal(&metav1.MicroTime{Time: f.now.Add(30 * time.Second)}) {
		t.Fatal("policy edit replayed old cadence")
	}
}

func TestUnpinnedSelectionIsAccountedWithoutRedistributionOrReplay(t *testing.T) {
	for _, lostAcknowledgement := range []bool{false, true} {
		t.Run(fmt.Sprintf("lost-ack-%t", lostAcknowledgement), func(t *testing.T) {
			f := scheduler(t)
			original := f.r.Client.(client.WithWatch)
			unavailable, lost := true, false
			f.r.Client = interceptor.NewClient(original, interceptor.Funcs{
				Create: func(ctx context.Context, c client.WithWatch, o client.Object, opts ...client.CreateOption) error {
					if unavailable && o.GetName() == "net-a" {
						return fmt.Errorf("target creation unavailable")
					}
					return c.Create(ctx, o, opts...)
				},
				SubResourcePatch: func(ctx context.Context, c client.Client, sub string, o client.Object, patch client.Patch, opts ...client.SubResourcePatchOption) error {
					err := c.SubResource(sub).Patch(ctx, o, patch, opts...)
					if p, ok := o.(*bitcoinv1.BitcoinBlockProduction); ok && err == nil && lostAcknowledgement && !lost && p.Status.UnassignedOpportunities == 1 {
						lost = true
						return fmt.Errorf("lost selection acknowledgement")
					}
					return err
				},
			})
			f.step(t)
			f.now = f.now.Add(5 * time.Second)
			_, err := f.r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(f.p)})
			if (err != nil) != lostAcknowledgement {
				t.Fatalf("unexpected selection write: %v", err)
			}
			// Re-reading a committed selection must not redraw, including its unassigned outcome.
			f.draw = 1
			f.step(t)
			if f.p.Status.Opportunities != 1 || f.p.Status.UnassignedOpportunities != 1 || f.p.Status.LastUnassignedTarget != "a" || f.p.Ledger("a") != nil || f.p.Ledger("b").Offered != 0 {
				t.Fatal(f.p.Status)
			}
			f.now = f.now.Add(5 * time.Second)
			f.step(t)
			if f.p.Ledger("b").Offered != 1 {
				t.Fatal("unregistered target blocked healthy scheduling")
			}
			unavailable = false
			f.step(t)
			if record := f.p.Ledger("a"); record == nil || record.Offered != 0 || record.Opportunity != nil {
				t.Fatal("registration replayed past unassigned work")
			}
			f.draw = 0
			f.now = f.now.Add(5 * time.Second)
			f.step(t)
			if f.p.Status.Opportunities != 3 || f.p.Status.UnassignedOpportunities != 1 || f.p.Ledger("a").Offered != 1 || f.p.Ledger("b").Offered != 1 {
				t.Fatal("selection totals do not reconcile")
			}
		})
	}
}
