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

func TestUnsupportedCadenceDoesNotReserveOrStarve(t *testing.T) {
	for _, tc := range []struct {
		name    string
		count   int32
		cadence actionv1.GenerationCadence
	}{
		{"absent", 2, actionv1.GenerationCadence{}},
		{"unknown-one", 1, actionv1.GenerationCadence{Mode: "Future"}},
		{"invalid-later-delay", 3, actionv1.GenerationCadence{Mode: "Explicit", DelaysSeconds: []int32{0, 61}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := fixture(t)
			ctx := context.Background()
			a := actionFixture(t, f, "a-invalid", tc.count)
			a.Spec.Cadence = tc.cadence
			if err := f.r.Update(ctx, a); err != nil {
				t.Fatal(err)
			} // Fake client deliberately bypasses current CEL.
			f.rpc.check = func(context.Context) error { t.Error("unsupported request reached RPC preflight"); return nil }
			selected, err := f.r.selectAction(ctx, f.ledger(t), f.parent)
			if err != nil || selected || f.ledger(t).Status.Action != nil || f.rpc.sends != 0 {
				t.Fatalf("unsupported cadence reserved: %v", err)
			}
			f.rpc.check = nil
			f.reconcile(t, ctx)
			if f.rpc.sends != 1 || f.ledger(t).Status.BlocksProduced != 1 || f.ledger(t).Status.Action != nil {
				t.Fatal("invalid request blocked baseline")
			}
			valid := actionFixture(t, f, "z-valid", 1)
			f.reconcile(t, ctx)
			actionStep(t, f, valid)
			actionStep(t, f, valid)
			if record := f.ledger(t).Status.Action; record == nil || record.UID != string(valid.UID) {
				t.Fatal("invalid oldest request starved eligible action")
			}
			f.reconcile(t, ctx)
			actionStep(t, f, valid)
			if valid.Status.Phase != "Completed" || valid.Status.BlocksGenerated != 1 || f.rpc.sends != 2 {
				t.Fatal("valid action failed after invalid request")
			}
		})
	}
}

// skewReservation simulates an incompatible stored spec without changing admitted identities.
func skewReservation(t *testing.T, f *productionFixture, a *actionv1.BitcoinBlockGeneration, count int32, cadence actionv1.GenerationCadence) *bitcoinv1.BitcoinProductionTarget {
	t.Helper()
	ctx := context.Background()
	a.Spec.Count, a.Spec.Cadence = count, cadence
	if err := f.r.Update(ctx, a); err != nil {
		t.Fatal(err)
	}
	p := f.ledger(t)
	p.Status.Action.Spec = a.Spec
	if err := f.r.Status().Update(ctx, p); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestUnsupportedReservedCadenceStopsBeforeArm(t *testing.T) {
	for _, count := range []int32{0, 1, 3} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			f := fixture(t)
			ctx := context.Background()
			a := actionFixture(t, f, "reserved", 3)
			admitAction(t, f, a)
			skewReservation(t, f, a, count, actionv1.GenerationCadence{Mode: "Future"})
			f.rpc.check = func(context.Context) error { t.Error("invalid reservation reached preflight"); return nil }
			f.reconcile(t, ctx)
			f.reconcile(t, ctx)
			if p := f.ledger(t); p.Status.Action == nil || p.Status.Action.StopReason != "MechanismFailed" || p.Status.DispatchState == "Armed" || f.rpc.sends != 0 {
				t.Fatal("invalid reservation armed or released without acknowledgement")
			}
			actionStep(t, f, a)
			if a.Status.Phase != "Failed" || a.Status.BlocksGenerated != 0 {
				t.Fatal(a.Status)
			}
			f.reconcile(t, ctx)
			if f.ledger(t).Status.Action != nil || f.rpc.sends != 0 {
				t.Fatal("stopped reservation did not release after acknowledgement")
			}
		})
	}
}

func TestUnsupportedArmedCadenceStillRetainsUnknownCall(t *testing.T) {
	f := fixture(t)
	ctx := context.Background()
	a := actionFixture(t, f, "armed", 3)
	admitAction(t, f, a)
	p := skewReservation(t, f, a, 3, actionv1.GenerationCadence{Mode: "Future"})
	p.Status.DispatchState, p.Status.DispatchID = "Armed", "unknown-old-call"
	if err := f.r.Status().Update(ctx, p); err != nil {
		t.Fatal(err)
	}
	f.now = f.now.Add(2 * time.Minute)
	f.reconcile(t, ctx)
	actionStep(t, f, a)
	f.reconcile(t, ctx)
	p = f.ledger(t)
	if p.Status.Action == nil || !p.Status.Action.EffectUncertain || p.Status.Action.StopReason != "" || p.Status.DispatchState != "Armed" || p.Status.DispatchID != "unknown-old-call" || a.Status.Phase != "Inconclusive" || f.rpc.sends != 0 {
		t.Fatal("invalid cadence cleared unknown execution")
	}
}

func TestSchedulingFailureRetainsReceiptAcrossAccountingRetry(t *testing.T) {
	for _, tc := range []struct {
		committed bool
		stop      string
	}{{false, ""}, {true, ""}, {false, "DeadlineExceeded"}} {
		t.Run(fmt.Sprintf("committed=%t/stop=%s", tc.committed, tc.stop), func(t *testing.T) {
			committed := tc.committed
			expectedStop := tc.stop
			if expectedStop == "" {
				expectedStop = "MechanismFailed"
			}
			f := fixture(t)
			ctx := context.Background()
			a := actionFixture(t, f, "receipt", 3)
			admitAction(t, f, a)
			// Model a call already armed by an incompatible executor, then collect its real fake-RPC receipt.
			armed := skewReservation(t, f, a, 3, actionv1.GenerationCadence{Mode: "Explicit", DelaysSeconds: []int32{0, 61}})
			armed.Status.DispatchState, armed.Status.DispatchID = "Armed", "old-authority"
			started := metav1.NewTime(f.now)
			armed.Status.Action.StartedAt = &started
			armed.Status.Action.StopReason = tc.stop
			if err := f.r.Status().Update(ctx, armed); err != nil {
				t.Fatal(err)
			}
			injected := false
			f.r.Client = interceptor.NewClient(f.r.Client.(client.WithWatch), interceptor.Funcs{SubResourcePatch: func(ctx context.Context, c client.Client, sub string, o client.Object, patch client.Patch, opts ...client.SubResourcePatchOption) error {
				if p, ok := o.(*bitcoinv1.BitcoinProductionTarget); ok && !injected && p.Status.DispatchState == "Idle" && p.Status.Action.StopReason == expectedStop {
					injected = true
					if committed {
						if err := c.SubResource(sub).Patch(ctx, o, patch, opts...); err != nil {
							return err
						}
					}
					return fmt.Errorf("accounting acknowledgement lost")
				}
				return c.SubResource(sub).Patch(ctx, o, patch, opts...)
			}})
			if !f.r.collectors.reserve(armed.Status.DispatchID, armed.UID) {
				t.Fatal("collector slot unavailable")
			}
			f.r.collectors.collect(armed, "fixture-endpoint")
			f.waitCollectors(t)
			p := f.ledger(t)
			record := p.Status.Action
			if !injected || p.Status.DispatchState != "Idle" || p.Status.BlocksProduced != 0 || record.BlocksGenerated != 1 || record.LastBlockHash != fmt.Sprintf("%064x", 1) || record.LastDispatchID != armed.Status.DispatchID || record.LastCompletedAt == nil || !record.LastCompletedAt.Time.Equal(f.now) || record.StopReason != expectedStop || record.NextDispatchAt != nil || f.rpc.sends != 1 {
				t.Fatalf("known receipt lost or duplicated: %#v", record)
			}
			// The same receipt remains idempotent, and release still requires the action's acknowledgement.
			if err := f.r.account(ctx, armed, receivedReceipt{hash: record.LastBlockHash, completed: *record.LastCompletedAt}); err != nil {
				t.Fatal(err)
			}
			f.reconcile(t, ctx)
			if f.ledger(t).Status.Action == nil {
				t.Fatal("receipt released before acknowledgement")
			}
			actionStep(t, f, a)
			if a.Status.Phase != "Failed" || a.Status.BlocksGenerated != 1 || a.Status.LastDispatchID != armed.Status.DispatchID {
				t.Fatal(a.Status)
			}
			f.reconcile(t, ctx)
			if f.ledger(t).Status.Action != nil || f.rpc.sends != 1 {
				t.Fatal("failed mechanism replayed or failed to release")
			}
		})
	}
}

func TestMissingFiniteTimerStopsWithKnownProgress(t *testing.T) {
	f := fixture(t)
	ctx := context.Background()
	a := actionFixture(t, f, "missing-timer", 3)
	admitAction(t, f, a)
	f.reconcile(t, ctx)
	p := f.ledger(t)
	hash, dispatch := p.Status.Action.LastBlockHash, p.Status.Action.LastDispatchID
	p.Status.Action.NextDispatchAt = nil
	if err := f.r.Delete(ctx, f.pod); err != nil {
		t.Fatal(err)
	}
	if err := f.r.Status().Update(ctx, p); err != nil {
		t.Fatal(err)
	}
	f.reconcile(t, ctx)
	f.reconcile(t, ctx)
	if record := f.ledger(t).Status.Action; record == nil || record.StopReason != "MechanismFailed" || record.BlocksGenerated != 1 || record.LastBlockHash != hash || record.LastDispatchID != dispatch || f.rpc.sends != 1 {
		t.Fatal("missing timer did not retain and stop known progress")
	}
	actionStep(t, f, a)
	if a.Status.Phase != "Failed" || a.Status.BlocksGenerated != 1 {
		t.Fatal(a.Status)
	}
	f.reconcile(t, ctx)
	if f.ledger(t).Status.Action != nil || f.rpc.sends != 1 {
		t.Fatal("missing timer replayed or retained acknowledged work")
	}
}
