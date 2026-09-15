package bitcoincontrol

import (
	"testing"
	"time"

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
	f.rpc.height = 1
	// Native preflight observes the changed chain and refuses the stale offer.
	if err := f.worker.Step(t.Context()); err == nil {
		t.Fatal("stale chain authorized")
	}
	f.reconcile(t, s)
	f.reconcile(t, s)
	next := f.readInitial(t).Status.Offer
	if next.Number != old.Number+1 || next.ExpectedHeight != 1 || f.rpc.count() != 0 {
		t.Fatal("stale offer retained or sent", next)
	}
	if err := f.worker.Step(t.Context()); err != nil {
		t.Fatal(err)
	}
	f.worker.workers.Wait()
	if f.rpc.count() != 1 {
		t.Fatal("replacement did not send once")
	}
}
