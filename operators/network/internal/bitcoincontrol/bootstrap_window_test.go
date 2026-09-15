package bitcoincontrol

import (
	"fmt"
	"testing"
	"time"

	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/foundation"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"k8s.io/apimachinery/pkg/api/equality"
)

// exerciseDelayedBootstrap admits one durable offer after several cadence ticks.
func exerciseDelayedBootstrap(t *testing.T, f *testFixture) {
	t.Helper()
	f.worker.Now = func() time.Time { return f.now }
	if err := f.worker.Step(t.Context()); err != nil {
		t.Fatal(err)
	}
	s := f.schedulerFor()
	f.reconcile(t, s)
	f.now = f.now.Add(time.Second)
	f.reconcile(t, s)
	f.reconcile(t, s)
	initial := f.readInitial(t)
	offer := initial.Status.Offer.DeepCopy()
	if initial.Status.NextOpportunityAt.Sub(f.now) != time.Second {
		t.Fatal("admission window changed cadence")
	}
	if offer == nil || offer.ExpiresAt.Sub(f.now) != 30*time.Second {
		t.Fatal("bootstrap window not independent of cadence", offer)
	}
	// The two-second control loop can miss a one-second admission window.
	for range 3 {
		f.now = f.now.Add(2 * time.Second)
		f.reconcile(t, s)
		if !equality.Semantic.DeepEqual(f.readRecord(t).Spec.Offer, offer) ||
			!equality.Semantic.DeepEqual(f.readInitial(t).Status.Offer, offer) {
			t.Fatal("unconsumed bootstrap offer replaced at a cadence tick")
		}
	}
	if err := f.worker.Step(t.Context()); err != nil {
		t.Fatal(err)
	}
	f.worker.workers.Wait()
	f.reconcile(t, s)
	if f.rpc.count() != 1 || f.readInitial(t).Status.LastAccountedOffer != offer.Number {
		t.Fatal("delayed offer did not generate and account exactly once")
	}
}

func TestBootstrapAdmissionOutlivesFastCadence(t *testing.T) {
	exerciseDelayedBootstrap(t, newFixture(t))
}

func TestBootstrapExpiredOfferCannotSend(t *testing.T) {
	f := newFixture(t)
	f.worker.Now = func() time.Time { return f.now }
	if err := f.worker.Step(t.Context()); err != nil {
		t.Fatal(err)
	}
	s := f.schedulerFor()
	f.reconcile(t, s)
	f.now = f.now.Add(time.Second)
	f.reconcile(t, s)
	f.reconcile(t, s)
	f.now = f.readInitial(t).Status.Offer.ExpiresAt.Time
	if err := f.worker.Step(t.Context()); err == nil {
		t.Fatal("expired offer accepted")
	}
	if f.rpc.count() != 0 || f.readRecord(t).Status.Armed != nil {
		t.Fatal("expired offer sent")
	}
	f.reconcile(t, s)
	f.reconcile(t, s)
	if f.readInitial(t).Status.Offer.Number != 2 {
		t.Fatal("expired unsent offer did not get a fresh opportunity")
	}
}

// A newer converged chain invalidates an unsent offer rather than waiting its full window.
func TestBootstrapReplacesUnsentOfferOnChangedChain(t *testing.T) {
	for _, sameHeight := range []bool{false, true} {
		t.Run(fmt.Sprint("same-height=", sameHeight), func(t *testing.T) {
			f := newFixture(t)
			f.worker.Now = func() time.Time { return f.now }
			if err := f.worker.Step(t.Context()); err != nil {
				t.Fatal(err)
			}
			s := f.schedulerFor()
			f.reconcile(t, s)
			f.now = f.now.Add(time.Second)
			f.reconcile(t, s)
			f.reconcile(t, s)
			old := f.readInitial(t).Status.Offer.DeepCopy()
			f.now = f.now.Add(2 * time.Second)
			if sameHeight {
				f.rpc.tip = fmt.Sprintf("%064x", 999)
			} else {
				f.rpc.height = 1
			}
			expectedTip := fmt.Sprintf("%064x", f.rpc.height)
			if sameHeight {
				expectedTip = f.rpc.tip
			}
			// Native preflight observes the changed chain and refuses the stale offer.
			if err := f.worker.Step(t.Context()); err == nil {
				t.Fatal("stale chain authorized")
			}
			f.reconcile(t, s)
			f.reconcile(t, s)
			next := f.readInitial(t).Status.Offer
			if next.Number != old.Number+1 || next.ExpectedHeight != f.rpc.height || next.ExpectedTip != expectedTip ||
				f.rpc.count() != 0 {
				t.Fatal("stale offer retained or sent", next)
			}
			if err := f.worker.Step(t.Context()); err != nil {
				t.Fatal(err)
			}
			f.worker.workers.Wait()
			if f.rpc.count() != 1 {
				t.Fatal("replacement did not send once")
			}
		})
	}
}

// exerciseBootstrapAuthorityChange keeps unsent work live while preserving unknown Armed work.
func exerciseBootstrapAuthorityChange(t *testing.T, f *testFixture, mode string, armed bool) {
	t.Helper()
	f.worker.Now = func() time.Time { return f.now }
	if err := f.worker.Step(t.Context()); err != nil {
		t.Fatal(err)
	}
	scheduler := f.schedulerFor()
	f.reconcile(t, scheduler)
	f.now = f.now.Add(time.Second)
	f.reconcile(t, scheduler)
	f.reconcile(t, scheduler)
	old := f.readInitial(t).Status.Offer.DeepCopy()
	f.now = f.now.Add(2 * time.Second)
	if mode == "policy" {
		production := f.production.DeepCopy()
		if err := f.c.Get(t.Context(), client.ObjectKeyFromObject(production), production); err != nil {
			t.Fatal(err)
		}
		production.Status.Admission.Configuration.BitcoinBlockProduction.Schedule.Cadence.Interval = ptr.To(
			common.Duration("2s"),
		)
		production.Status.Admission.PolicyDigest = foundation.Digest(production.Status.Admission.Configuration)
		if err := f.c.Status().Update(t.Context(), production); err != nil {
			t.Fatal(err)
		}
	} else {
		replaceBootstrapProducer(t, f, "replacement")
	}
	if armed {
		record := f.readRecord(t)
		record.Status.Armed = &bitcoin.BitcoinArmedRPC{ID: "unknown", ProcessNonce: "prior", Offer: old}
		if err := f.c.Status().Update(t.Context(), record); err != nil {
			t.Fatal(err)
		}
		f.reconcile(t, scheduler)
		if !equality.Semantic.DeepEqual(f.readInitial(t).Status.Offer, old) ||
			!equality.Semantic.DeepEqual(f.readRecord(t).Spec.Offer, old) ||
			f.rpc.count() != 0 {
			t.Fatal("authority change replaced Armed work")
		}
		return
	}
	if err := f.worker.Step(t.Context()); err == nil {
		t.Fatal("old authority accepted")
	}
	if f.rpc.count() != 0 || f.readRecord(t).Status.Armed != nil {
		t.Fatal("old authority sent or armed")
	}
	// Rebinding arms a fresh cadence; either case must progress well before old expiry.
	for range 4 {
		f.reconcile(t, scheduler)
		f.now = f.now.Add(time.Second)
	}
	f.reconcile(t, scheduler)
	next := f.readInitial(t).Status.Offer
	if !f.now.Before(old.ExpiresAt.Time) || next.Number != old.Number+1 ||
		next.PolicyDigest == old.PolicyDigest && next.Production == old.Production ||
		f.rpc.count() != 0 {
		t.Fatal("stale authority retained", next)
	}
	if mode == "policy" && next.Production != old.Production {
		t.Fatal("policy change rebound producer")
	}
	if mode == "production" && next.Production.UID != "replacement" {
		t.Fatal("replacement identity missing")
	}
	if err := f.worker.Step(t.Context()); err != nil {
		t.Fatal(err)
	}
	f.worker.workers.Wait()
	f.reconcile(t, scheduler)
	if f.rpc.count() != 1 || f.readInitial(t).Status.LastAccountedOffer != next.Number {
		t.Fatal("replacement did not send and account exactly once")
	}
}

func TestBootstrapAuthorityChange(t *testing.T) {
	for _, mode := range []string{"policy", "production"} {
		for _, armed := range []bool{false, true} {
			t.Run(
				fmt.Sprintf("%s/armed=%v", mode, armed),
				func(t *testing.T) { exerciseBootstrapAuthorityChange(t, newFixture(t), mode, armed) },
			)
		}
	}
}
