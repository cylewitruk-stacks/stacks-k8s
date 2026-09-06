package production

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	bitcoinv1 "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

// removeProductionTarget keeps the actor and pinned ledger while removing baseline selection.
func removeProductionTarget(t *testing.T, f *productionFixture) {
	t.Helper()
	ctx := context.Background()
	secondTarget(t, f)
	f.parent.Spec.BitcoinBlockProduction.Targets = f.parent.Spec.BitcoinBlockProduction.Targets[1:]
	f.parent.Generation++
	if err := f.r.Update(ctx, f.parent); err != nil {
		t.Fatal(err)
	}
	f.parent.Status.TargetDeclarations.ObservedGeneration = f.parent.Generation
	if err := f.r.Status().Update(ctx, f.parent); err != nil {
		t.Fatal(err)
	}
	f.root.Spec.Policy = *f.parent.Spec.BitcoinBlockProduction.DeepCopy()
	f.root.Generation++
	if err := f.r.Update(ctx, f.root); err != nil {
		t.Fatal(err)
	}
}

func TestRemovedTargetReportsRetainedAndRejectsNewActions(t *testing.T) {
	for _, kind := range []string{"generation", "reorganization"} {
		t.Run(kind, func(t *testing.T) {
			f := fixture(t)
			if kind == "generation" {
				actionFixture(t, f, "waiting", 1)
			} else {
				f, _, _ = reorgFixture(t)
			}
			removeProductionTarget(t, f)
			for range 3 {
				f.reconcile(t, context.Background())
			}
			got := f.ledger(t).Status
			if got.Phase != "Paused" || !strings.Contains(got.Message, "not selected") || got.Action != nil || got.Reorganization != nil || got.DispatchID != "" || f.rpc.sends != 0 {
				t.Fatalf("inactive target admitted work or misreported state: %#v", got)
			}
		})
	}
}

func TestRemovalPreservesAdmittedGeneration(t *testing.T) {
	f := fixture(t)
	a := actionFixture(t, f, "admitted", 1)
	admitAction(t, f, a)
	removeProductionTarget(t, f)
	f.reconcile(t, context.Background())
	actionStep(t, f, a)
	if a.Status.Phase != "Completed" || a.Status.BlocksGenerated != 1 || f.rpc.sends != 1 {
		t.Fatal("removal discarded an admitted generation obligation")
	}
	f.reconcile(t, context.Background())
	f.reconcile(t, context.Background())
	if got := f.ledger(t).Status; got.Action != nil || got.Phase != "Paused" || got.BlocksProduced != 0 {
		t.Fatal(got)
	}
}

func TestRemovalPreservesAdmittedReorganization(t *testing.T) {
	f, rpc, a := reorgFixture(t)
	admitReorg(t, f, a)
	removeProductionTarget(t, f)
	for range 12 {
		f.reconcile(t, context.Background())
		reorgStep(t, f, a)
		f.now = f.now.Add(time.Second)
	}
	if a.Status.Phase != "Completed" || !a.Status.CleanupAcknowledged || rpc.invalidations != 1 || rpc.cleanups != 1 || f.rpc.sends != 3 {
		t.Fatalf("admitted replacement did not complete: %#v", a.Status)
	}
	if got := f.ledger(t).Status; got.Reorganization != nil || got.Phase != "Paused" {
		t.Fatal(got)
	}
}

func TestPolicyChangedOfferIsConsumedWithoutArming(t *testing.T) {
	f := fixture(t)
	ctx := context.Background()
	if err := f.r.Get(ctx, client.ObjectKeyFromObject(f.root), f.root); err != nil {
		t.Fatal(err)
	}
	f.root.Generation++
	if err := f.r.Update(ctx, f.root); err != nil {
		t.Fatal(err)
	}
	f.reconcile(t, ctx)
	if got := f.ledger(t).Status; got.OpportunitiesConsumed != 1 || got.OpportunitiesSkipped != 1 || got.LastSkipReason != "PolicyChanged" || got.DispatchID != "" || f.rpc.sends != 0 {
		t.Fatal(got)
	}
}

func TestCapacitySkipsOfferWithoutLeavingAuthorization(t *testing.T) {
	f := fixture(t)
	f.r.collectors.limit = 1
	if !f.r.collectors.reserve("occupied", "another-ledger") {
		t.Fatal("failed to occupy capacity")
	}
	defer f.r.collectors.release("occupied")
	_, err := f.r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(f.policy)})
	if err != nil {
		t.Fatal(err)
	}
	if got := f.ledger(t).Status; got.OpportunitiesSkipped != 1 || got.LastSkipReason != "Capacity" || got.DispatchID != "" || got.DispatchState == "Armed" || f.rpc.sends != 0 {
		t.Fatal(got)
	}
	f.r.collectors.release("occupied")
	f.offer(t)
	f.reconcile(t, context.Background())
	if got := f.ledger(t).Status; got.BlocksProduced != 1 || got.OpportunitiesConsumed != 2 || got.OpportunitiesSkipped != 1 {
		t.Fatal(got)
	}
}

func TestReservationPreservesExistingOpportunitySkipReason(t *testing.T) {
	for _, kind := range []string{"generation", "reorganization"} {
		for _, reason := range []string{"Expired", "PolicyChanged"} {
			t.Run(kind+"/"+reason, func(t *testing.T) {
				f := fixture(t)
				if kind == "generation" {
					actionFixture(t, f, "reserve", 1)
				} else {
					f, _, _ = reorgFixture(t)
				}
				// Change only the opportunity during read-only preflight, after initial admission.
				f.rpc.check = func(context.Context) error {
					if reason == "Expired" {
						f.now = f.now.Add(6 * time.Second)
					} else {
						if err := f.r.Get(context.Background(), client.ObjectKeyFromObject(f.root), f.root); err != nil {
							return err
						}
						f.root.Status.Targets[0].Opportunity.PolicyGeneration = 0
						return f.r.Status().Update(context.Background(), f.root)
					}
					return nil
				}
				selected, err := f.r.selectAction(context.Background(), f.ledger(t), f.parent)
				if err != nil || !selected {
					t.Fatalf("reservation failed: %v", err)
				}
				got := f.ledger(t).Status
				if got.LastSkipReason != reason || got.OpportunitiesSkipped != 1 || f.rpc.sends != 0 {
					t.Fatal(got)
				}
			})
		}
	}
}

func TestExpiredOfferReasonSurvivesExistingReservationOrDispatch(t *testing.T) {
	for _, state := range []string{"Reserved", "Armed"} {
		t.Run(state, func(t *testing.T) {
			f := fixture(t)
			if state == "Reserved" {
				a := actionFixture(t, f, "reserve", 1)
				admitAction(t, f, a)
			} else {
				p := f.ledger(t)
				p.Status.DispatchState = "Armed"
				p.Status.DispatchID = "unresolved"
				p.Status.OpportunitiesConsumed = 1
				if err := f.r.Status().Update(context.Background(), p); err != nil {
					t.Fatal(err)
				}
			}
			f.offer(t)
			f.now = f.now.Add(6 * time.Second)
			f.reconcile(t, context.Background())
			if got := f.ledger(t).Status; got.LastSkipReason != "Expired" || got.OpportunitiesConsumed != 2 || f.rpc.sends != 0 {
				t.Fatal(got)
			}
		})
	}
}

func TestSkipAndReceiptAccountingConflictsPreserveBothFacts(t *testing.T) {
	for _, winner := range []string{"receipt", "skip"} {
		t.Run(winner, func(t *testing.T) {
			f := fixture(t)
			ctx := context.Background()
			armed := f.ledger(t)
			armed.Status.DispatchState = "Armed"
			armed.Status.DispatchID = "known-receipt"
			armed.Status.OpportunitiesConsumed = 1
			if err := f.r.Status().Update(ctx, armed); err != nil {
				t.Fatal(err)
			}
			f.offer(t)
			offer := f.root.Ledger("bitcoin").Opportunity.DeepCopy()
			receipt := receivedReceipt{hash: fmt.Sprintf("%064x", 42), completed: metav1.NewTime(f.now)}
			injected := false
			f.r.Client = interceptor.NewClient(f.r.Client.(client.WithWatch), interceptor.Funcs{SubResourcePatch: func(ctx context.Context, c client.Client, sub string, o client.Object, patch client.Patch, opts ...client.SubResourcePatchOption) error {
				p, ok := o.(*bitcoinv1.BitcoinProductionTarget)
				if ok && !injected && ((winner == "receipt" && p.Status.OpportunitiesSkipped > 0) || (winner == "skip" && p.Status.DispatchState == "Idle")) {
					injected = true
					if winner == "receipt" {
						if err := f.r.account(ctx, armed, receipt); err != nil {
							return err
						}
					} else {
						if _, err := f.r.skip(ctx, armed.DeepCopy(), offer, "Outstanding"); err != nil {
							return err
						}
					}
				}
				return c.SubResource(sub).Patch(ctx, o, patch, opts...)
			}})
			var err error
			if winner == "receipt" {
				_, err = f.r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(armed)})
			} else {
				err = f.r.account(ctx, armed, receipt)
			}
			if !injected || !apierrors.IsConflict(err) {
				t.Fatalf("expected competing CAS conflict: %v", err)
			}
			if winner == "receipt" {
				f.now = f.now.Add(6 * time.Second)
				f.reconcile(t, ctx)
			} else {
				if err := f.r.account(ctx, armed, receipt); err != nil {
					t.Fatal(err)
				}
			}
			got := f.ledger(t).Status
			if got.DispatchState != "Idle" || got.DispatchID != armed.Status.DispatchID || got.LastBlockHash != receipt.hash || got.BlocksProduced != 1 || got.OpportunitiesConsumed != 2 || got.OpportunitiesSkipped != 1 || f.rpc.sends != 0 {
				t.Fatalf("conflict lost receipt/skip or replayed RPC: %#v", got)
			}
		})
	}
}
