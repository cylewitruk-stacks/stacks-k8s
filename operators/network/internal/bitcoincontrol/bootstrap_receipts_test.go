package bitcoincontrol

import (
	"context"
	"errors"
	"testing"
	"time"

	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/foundation"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

// bootstrapReceiptFixture retains one real worker receipt before scheduler accounting.
func bootstrapReceiptFixture(t *testing.T) *testFixture {
	t.Helper()
	f := newFixture(t)
	f.worker.Now = func() time.Time { return f.now }
	f.offer(t, 200)
	if err := f.worker.Step(t.Context()); err != nil {
		t.Fatal(err)
	}
	f.worker.workers.Wait()
	if err := f.worker.Step(t.Context()); err != nil {
		t.Fatal(err)
	}
	if f.rpc.count() != 1 || f.readRecord(t).Status.LastReceipt == nil {
		t.Fatal("receipt missing")
	}
	return f
}

// Retained receipts unblock subsequent generation without replaying expired selections.
func TestBootstrapReceiptSurvivesReplacedSelection(t *testing.T) {
	for _, cleared := range []bool{false, true} {
		t.Run(map[bool]string{false: "newer-selection", true: "cleared-selection"}[cleared], func(t *testing.T) {
			f := bootstrapReceiptFixture(t)
			receipt := f.readRecord(t).Status.LastReceipt.DeepCopy()
			initial := f.readInitial(t)
			initial.Status.Offer.Number = 9
			initial.Status.Offer.ExpectedHeight = 201
			initial.Status.Offer.ExpectedTip = f.readRecord(t).Status.Observation.Tip
			initial.Status.Offer.ExpiresAt = metav1.NewTime(f.now.Add(time.Minute))
			if cleared {
				initial.Status.Offer = nil
			}
			if err := f.c.Status().Update(t.Context(), initial); err != nil {
				t.Fatal(err)
			}
			scheduler := f.schedulerFor()
			for range 2 {
				f.reconcile(t, scheduler)
			}
			initial = f.readInitial(t)
			if initial.Status.LastAccountedOffer != 1 || len(initial.Status.Funded) != 1 || initial.Status.Funded[0].Outputs != 1 || f.rpc.count() != 1 || !generationAccounted(initial, f.readRecord(t)) {
				t.Fatalf("retained receipt not accounted exactly once: %+v", initial.Status)
			}
			if f.readRecord(t).Status.LastReceipt.Request.ID != receipt.Request.ID {
				t.Fatal("receipt replaced during accounting")
			}
			if !cleared {
				if err := f.worker.Step(t.Context()); err != nil {
					t.Fatal(err)
				}
				f.worker.workers.Wait()
				if f.rpc.count() != 2 || f.readRecord(t).Status.CompletedOffer != 9 {
					t.Fatal("new authorized opportunity did not progress")
				}
			}
		})
	}
}

// Foreign or incomplete receipt evidence cannot advance funding or the cursor.
func TestBootstrapReceiptIdentityAndFunding(t *testing.T) {
	for name, change := range map[string]func(*bitcoin.BitcoinExecution){
		"foreign-initialization": func(e *bitcoin.BitcoinExecution) { e.Status.LastReceipt.Request.Offer.Initialization.UID = "other" },
		"foreign-target":         func(e *bitcoin.BitcoinExecution) { e.Status.LastReceipt.Request.Target.Participant.UID = "other" },
		"missing-production":     func(e *bitcoin.BitcoinExecution) { e.Status.LastReceipt.Request.Offer.Production.UID = "" },
		"foreign-wallet":         func(e *bitcoin.BitcoinExecution) { e.Status.LastReceipt.Request.Offer.Wallet.UID = "other" },
		"wrong-address":          func(e *bitcoin.BitcoinExecution) { e.Status.LastReceipt.Request.Offer.Address = "other" },
		"baseline":               func(e *bitcoin.BitcoinExecution) { e.Status.LastReceipt.Request.Offer.Mode = "Baseline" },
		"uncompleted":            func(e *bitcoin.BitcoinExecution) { e.Status.CompletedOffer++ },
		"no-receipt":             func(e *bitcoin.BitcoinExecution) { e.Status.LastReceipt = nil },
	} {
		t.Run(name, func(t *testing.T) {
			f := bootstrapReceiptFixture(t)
			initial, execution := f.readInitial(t), f.readRecord(t)
			change(execution)
			if accountBootstrapReceipt(f.root, initial, execution) || initial.Status.LastAccountedOffer != 0 || len(initial.Status.Funded) != 0 {
				t.Fatal("invalid evidence counted")
			}
		})
	}
}

// Committed and uncommitted accounting responses recover without duplicate funding.
func TestBootstrapReceiptAccountingWriteLoss(t *testing.T) {
	for _, committed := range []bool{false, true} {
		t.Run(map[bool]string{false: "uncommitted", true: "committed"}[committed], func(t *testing.T) {
			f := bootstrapReceiptFixture(t)
			initial := f.readInitial(t)
			initial.Status.Offer = nil
			if err := f.c.Status().Update(t.Context(), initial); err != nil {
				t.Fatal(err)
			}
			s := f.schedulerFor()
			lost := false
			s.Client = interceptor.NewClient(f.c.(client.WithWatch), interceptor.Funcs{SubResourceUpdate: func(ctx context.Context, c client.Client, sub string, obj client.Object, opts ...client.SubResourceUpdateOption) error {
				if state, ok := obj.(*bitcoin.BitcoinInitialization); ok && !lost && state.Status.LastAccountedOffer == 1 {
					lost = true
					if committed {
						if err := c.SubResource(sub).Update(ctx, obj, opts...); err != nil {
							return err
						}
					}
					return errors.New("accounting write response lost")
				}
				return c.SubResource(sub).Update(ctx, obj, opts...)
			}})
			if _, err := s.Reconcile(t.Context(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(initial)}); err == nil || !lost {
				t.Fatal("write loss not exercised")
			}
			if generationAccounted(f.readInitial(t), f.readRecord(t)) != committed {
				t.Fatal("write outcome changed release authority")
			}
			s = f.schedulerFor()
			for range 2 {
				f.reconcile(t, s)
			}
			state := f.readInitial(t).Status
			if state.LastAccountedOffer != 1 || len(state.Funded) != 1 || state.Funded[0].Outputs != 1 || f.rpc.count() != 1 {
				t.Fatal("recovery lost or duplicated receipt", state)
			}
		})
	}
}

// Baseline handoff preserves bootstrap attribution and sequence continuity.
func TestBaselineTransitionAccountsFinalBootstrapReceipt(t *testing.T) {
	f := bootstrapReceiptFixture(t)
	initial := f.readInitial(t)
	initial.Status.Offer = nil
	initial.Status.Baseline = &bitcoin.BitcoinBaselineStatus{}
	s := f.schedulerFor()
	for range 2 {
		s.accountBaselineReceipts(t.Context(), f.root, initial)
	}
	if initial.Status.LastAccountedOffer != 1 || initial.Status.Baseline.Sequence != 1 || initial.Status.Baseline.Scheduling.Acknowledged != 0 || len(initial.Status.Funded) != 1 || initial.Status.Funded[0].Outputs != 1 || !generationAccounted(initial, f.readRecord(t)) {
		t.Fatal("transition lost or recategorized bootstrap receipt", initial.Status)
	}
}

// replaceBootstrapProducer updates public membership while leaving frozen provenance intact.
func replaceBootstrapProducer(t *testing.T, f *testFixture, name string) {
	t.Helper()
	root := f.root.DeepCopy()
	if err := f.c.Get(t.Context(), client.ObjectKeyFromObject(root), root); err != nil {
		t.Fatal(err)
	}
	root.Spec.Participants[1].Name = name
	if err := f.c.Update(t.Context(), root); err != nil {
		t.Fatal(err)
	}
	root.Status.Identities[1] = api.InstanceIdentity{Name: name, UID: types.UID(name)}
	if err := f.c.Status().Update(t.Context(), root); err != nil {
		t.Fatal(err)
	}
	p := f.production.DeepCopy()
	p.Name = foundation.ParticipantName(string(root.UID), name)
	p.UID = types.UID(name)
	p.ResourceVersion = ""
	p.Spec.ParticipantName = name
	if err := f.c.Create(t.Context(), p); err != nil {
		t.Fatal(err)
	}
}

// A retired producer receipt remains accountable before the next producer sends.
func TestReplacementBootstrapProducerReceiptSurvivesFurtherReplacement(t *testing.T) {
	f := newFixture(t)
	f.worker.Now = func() time.Time { return f.now }
	s := f.schedulerFor()
	replaceBootstrapProducer(t, f, "replacement-a")
	// Select and send with A, not the frozen original producer.
	if err := f.worker.Step(t.Context()); err != nil {
		t.Fatal(err)
	}
	for range 4 {
		f.reconcile(t, s)
		f.now = f.now.Add(time.Second)
	}
	f.reconcile(t, s)
	f.reconcile(t, s)
	if err := f.worker.Step(t.Context()); err != nil {
		t.Fatal(err)
	}
	f.worker.workers.Wait()
	record := f.readRecord(t)
	if f.rpc.count() != 1 || record.Status.LastReceipt.Request.Offer.Production.UID != "replacement-a" {
		t.Fatal("replacement did not generate", record.Status)
	}
	// A's receipt remains unaccounted when membership moves to B.
	replaceBootstrapProducer(t, f, "replacement-b")
	if err := f.worker.Step(t.Context()); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		f.reconcile(t, s)
	}
	initial := f.readInitial(t)
	if !generationAccounted(initial, f.readRecord(t)) || len(initial.Status.Funded) != 1 || initial.Status.Funded[0].Outputs != 1 {
		t.Fatal("retired replacement receipt lost", initial.Status)
	}
	for range 3 {
		f.now = f.now.Add(time.Second)
		f.reconcile(t, s)
		f.reconcile(t, s)
	}
	if err := f.worker.Step(t.Context()); err != nil {
		t.Fatal(err)
	}
	f.worker.workers.Wait()
	record = f.readRecord(t)
	if f.rpc.count() != 2 || record.Status.LastReceipt.Request.Offer.Production.UID != "replacement-b" {
		t.Fatal("next producer did not progress", record.Status)
	}
}
