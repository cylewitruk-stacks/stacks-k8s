package bitcoincontrol

import (
	"testing"
	"time"

	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

func TestSchedulerFiltersHeartbeatsButRetainsAuthorityAndReceipts(t *testing.T) {
	old := &bitcoin.BitcoinExecution{
		Status: bitcoin.BitcoinExecutionStatus{Observation: &bitcoin.BitcoinObservation{ObservedAt: metav1.Now()}},
	}
	current := old.DeepCopy()
	current.Status.Observation.ObservedAt = metav1.NewTime(time.Now().Add(time.Second))
	pred := schedulerEvents()
	if pred.Update(event.UpdateEvent{ObjectOld: old, ObjectNew: current}) {
		t.Fatal("heartbeat enqueued")
	}
	current.Status.Observation.Height++
	if !pred.Update(event.UpdateEvent{ObjectOld: old, ObjectNew: current}) {
		t.Fatal("block progress suppressed")
	}
	current = old.DeepCopy()
	current.Generation++
	if !pred.Update(event.UpdateEvent{ObjectOld: old, ObjectNew: current}) {
		t.Fatal("authority change suppressed")
	}
	p := &api.StacksNetworkParticipant{Spec: api.StacksNetworkParticipantSpec{Kind: "StacksNode"}}
	changed := p.DeepCopy()
	changed.Status.Runtime = &api.ParticipantRuntimeStatus{Terminated: true}
	if pred.Update(event.UpdateEvent{ObjectOld: p, ObjectNew: changed}) {
		t.Fatal("unrelated actor status enqueued Bitcoin scheduler")
	}
	changed.Generation++
	if !pred.Update(event.UpdateEvent{ObjectOld: p, ObjectNew: changed}) {
		t.Fatal("declaration change suppressed")
	}
}

func TestSchedulerTimerPreservesShortCadenceAndRepairsWithoutEvents(t *testing.T) {
	now := time.Now()
	for _, d := range []time.Duration{time.Second, 10 * time.Second} {
		due := metav1.NewTime(now.Add(d))
		record := &bitcoin.BitcoinInitialization{Status: bitcoin.BitcoinInitializationStatus{NextOpportunityAt: &due}}
		if got := schedulerDelay(record, now); got != min(d, 2*time.Second) {
			t.Fatalf("delay=%s", got)
		}
	}
	if got := schedulerDelay(&bitcoin.BitcoinInitialization{}, now); got != 2*time.Second {
		t.Fatalf("repair delay=%s", got)
	}
}
