package production

import (
	"context"
	"fmt"
	"testing"
	"time"

	actionv1 "github.com/cylewitruk-stacks/stacks-k8s/apis/network/actions/v1alpha1"
	bitcoinv1 "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

func TestGenerationCadencesPersistAcrossRestartWithoutCatchup(t *testing.T) {
	for _, cadence := range []actionv1.GenerationCadence{
		{Mode: "Immediate"}, {Mode: "Fixed", IntervalSeconds: 2}, {Mode: "Uniform", MinSeconds: 1, MaxSeconds: 3}, {Mode: "Explicit", DelaysSeconds: []int32{0, 3}},
	} {
		t.Run(cadence.Mode, func(t *testing.T) {
			ctx := context.Background()
			f := fixture(t)
			f.now = f.now.Add(123456789 * time.Nanosecond)
			a := actionFixture(t, f, "cadence", 3)
			a.Spec.Cadence = cadence
			if err := f.r.Update(ctx, a); err != nil {
				t.Fatal(err)
			}
			admitAction(t, f, a)
			for count := int32(1); count <= 3; count++ {
				receiptAt := f.now
				f.reconcile(t, ctx)
				record := f.ledger(t).Status.Action
				if record.BlocksGenerated != count || f.rpc.sends != int(count) || f.ledger(t).Status.BlocksProduced != 0 {
					t.Fatal("cadence duplicated or misattributed a dispatch")
				}
				if count == 3 {
					if record.NextDispatchAt != nil {
						t.Fatal("completed action retained a timer")
					}
					break
				}
				if record.NextDispatchAt == nil {
					t.Fatal("receipt did not atomically persist a due time")
				}
				delay := record.NextDispatchAt.Sub(receiptAt)
				lower, upper := time.Duration(0), time.Duration(0)
				switch cadence.Mode {
				case "Fixed":
					lower, upper = 2*time.Second, 2*time.Second
				case "Uniform":
					lower, upper = time.Second, 3*time.Second
				case "Explicit":
					lower = time.Duration(cadence.DelaysSeconds[count-1]) * time.Second
					upper = lower
				}
				if delay < lower || delay >= upper+time.Microsecond {
					t.Fatalf("delay outside cadence: %v", delay)
				}
				due := record.NextDispatchAt.DeepCopy()
				replacement := *f.r
				replacement.ProcessNonce = fmt.Sprintf("restart-%d", count)
				replacement.collectors = newCollectorPool(&replacement, collectorLimit, collectorDrain)
				f.r = &replacement
				f.now = due.Add(-time.Nanosecond)
				f.reconcile(t, ctx)
				if f.rpc.sends != int(count) || !f.ledger(t).Status.Action.NextDispatchAt.Equal(due) {
					t.Fatal("restart shortened or redrew the delay")
				}
				// A late worker sends only the next block; its following gap anchors to the new receipt.
				f.now = due.Add(5 * time.Second)
			}
			actionStep(t, f, a)
			if a.Status.Phase != "Completed" {
				t.Fatal(a.Status)
			}
			f.reconcile(t, ctx)
			if f.ledger(t).Status.Action != nil || f.rpc.sends != 3 {
				t.Fatal("completion did not release acknowledged work")
			}
		})
	}
}

func TestCadenceReceiptAccountingRetryPreservesOriginalDueTime(t *testing.T) {
	for _, committed := range []bool{false, true} {
		t.Run(fmt.Sprint(committed), func(t *testing.T) {
			f := fixture(t)
			ctx := context.Background()
			a := actionFixture(t, f, "account-retry", 2)
			a.Spec.Cadence = actionv1.GenerationCadence{Mode: "Uniform", MinSeconds: 1, MaxSeconds: 60}
			if err := f.r.Update(ctx, a); err != nil {
				t.Fatal(err)
			}
			admitAction(t, f, a)
			armed := f.ledger(t)
			armed.Status.DispatchState, armed.Status.DispatchID = "Armed", "cadence-receipt"
			if err := f.r.Status().Update(ctx, armed); err != nil {
				t.Fatal(err)
			}
			receipt := receivedReceipt{hash: fmt.Sprintf("%064x", 42), completed: metav1.NewTime(f.now.Add(987654321 * time.Nanosecond))}
			var sampled *metav1.MicroTime
			injected := false
			f.r.Client = interceptor.NewClient(f.r.Client.(client.WithWatch), interceptor.Funcs{SubResourcePatch: func(ctx context.Context, c client.Client, sub string, o client.Object, patch client.Patch, opts ...client.SubResourcePatchOption) error {
				p, ok := o.(*bitcoinv1.BitcoinProductionTarget)
				if ok && !injected && p.Status.DispatchState == "Idle" {
					injected = true
					sampled = p.Status.Action.NextDispatchAt.DeepCopy()
					if committed {
						if err := c.SubResource(sub).Patch(ctx, o, patch, opts...); err != nil {
							return err
						}
					}
					return fmt.Errorf("accounting acknowledgement lost")
				}
				return c.SubResource(sub).Patch(ctx, o, patch, opts...)
			}})
			if err := f.r.account(ctx, armed, receipt); err == nil || sampled == nil {
				t.Fatal("failure not exercised")
			}
			f.now = f.now.Add(5 * time.Minute)
			for i := 0; i < 2; i++ {
				if err := f.r.account(ctx, armed, receipt); err != nil {
					t.Fatal(err)
				}
			}
			record := f.ledger(t).Status.Action
			if record.BlocksGenerated != 1 || !record.NextDispatchAt.Equal(sampled) || record.LastBlockHash != receipt.hash || f.rpc.sends != 0 {
				t.Fatal("retry redrew timing, reanchored to accounting, or duplicated progress")
			}
		})
	}
}

func TestFiniteDeadlinePrecedesPersistedDelay(t *testing.T) {
	f := fixture(t)
	ctx := context.Background()
	a := actionFixture(t, f, "deadline-gap", 2)
	a.Spec.Cadence = actionv1.GenerationCadence{Mode: "Explicit", DelaysSeconds: []int32{60}}
	a.Spec.Timeout.Duration = 5 * time.Second
	if err := f.r.Update(ctx, a); err != nil {
		t.Fatal(err)
	}
	admitAction(t, f, a)
	f.reconcile(t, ctx)
	if f.ledger(t).Status.Action.NextDispatchAt == nil {
		t.Fatal("no delay recorded")
	}
	f.now = f.now.Add(5 * time.Second)
	f.reconcile(t, ctx)
	actionStep(t, f, a)
	f.reconcile(t, ctx)
	if a.Status.Phase != "Failed" || a.Status.BlocksGenerated != 1 || f.rpc.sends != 1 || f.ledger(t).Status.Action != nil {
		t.Fatal("delay bypassed timeout or retained acknowledged reservation")
	}
}

func TestImmediateAmbiguityPreventsFurtherBlocks(t *testing.T) {
	f := fixture(t)
	ctx := context.Background()
	a := actionFixture(t, f, "immediate-lost", 100)
	a.Spec.Cadence = actionv1.GenerationCadence{Mode: "Immediate"}
	if err := f.r.Update(ctx, a); err != nil {
		t.Fatal(err)
	}
	admitAction(t, f, a)
	f.rpc.generate = func(context.Context) error { return fmt.Errorf("receipt lost") }
	f.reconcile(t, ctx)
	replacement := *f.r
	replacement.ProcessNonce = "restart"
	replacement.collectors = newCollectorPool(&replacement, collectorLimit, collectorDrain)
	f.r = &replacement
	f.now = f.now.Add(2 * time.Minute)
	f.reconcile(t, ctx)
	actionStep(t, f, a)
	f.reconcile(t, ctx)
	if f.rpc.sends != 1 || f.ledger(t).Status.DispatchState != "Armed" || f.ledger(t).Status.Action == nil || a.Status.Phase != "Inconclusive" {
		t.Fatal("immediate cadence bypassed unresolved exclusion")
	}
}
