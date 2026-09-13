package bitcoincontrol

import (
	"time"

	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/foundation"
	"k8s.io/apimachinery/pkg/api/equality"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

// schedulerDelay preserves due cadence while bounding observation repair polls.
func schedulerDelay(record *bitcoin.BitcoinInitialization, now time.Time) time.Duration {
	delay := time.Duration(foundation.ObservationPolicy().PollIntervalSeconds) * time.Second
	if due := record.Status.NextOpportunityAt; due != nil && due.After(now) {
		delay = min(delay, due.Sub(now))
	}
	return delay
}

// schedulerEvents ignores its own status and heartbeat-only events, retaining authority and receipt changes.
func schedulerEvents() predicate.Predicate {
	return predicate.Funcs{UpdateFunc: func(e event.UpdateEvent) bool {
		a, b := e.ObjectOld, e.ObjectNew
		if a == nil || b == nil {
			return false
		}
		same := equality.Semantic.DeepEqual
		if a.GetUID() != b.GetUID() || a.GetGeneration() != b.GetGeneration() || !same(a.GetDeletionTimestamp(), b.GetDeletionTimestamp()) || !same(a.GetOwnerReferences(), b.GetOwnerReferences()) || !same(a.GetFinalizers(), b.GetFinalizers()) {
			return true
		}
		switch old := a.(type) {
		case *bitcoin.BitcoinExecution:
			current, ok := b.(*bitcoin.BitcoinExecution)
			return !ok || foundation.BitcoinExecutionChanged(old, current)
		case *bitcoin.BitcoinInitialization:
			return false // This controller publishes status and explicitly schedules its next continuation.
		case *api.StacksNetwork:
			current, ok := b.(*api.StacksNetwork)
			return !ok || !same(old.Status.Bitcoin, current.Status.Bitcoin) || !same(old.Status.Initialization, current.Status.Initialization) || failed(old) != failed(current)
		case *api.StacksNetworkParticipant:
			current, ok := b.(*api.StacksNetworkParticipant)
			if ok && current.Spec.Kind != api.ParticipantBitcoinNode && current.Spec.Kind != api.ParticipantBitcoinBlockProduction {
				return false // Protocol gates are observed by the bounded continuation timer.
			}
			return !ok || !same(old.Status.Admission, current.Status.Admission) || !same(old.Status.Runtime, current.Status.Runtime) || !same(old.Status.BitcoinControl, current.Status.BitcoinControl) || !same(old.Status.Conditions, current.Status.Conditions)
		}
		return true
	}}
}
