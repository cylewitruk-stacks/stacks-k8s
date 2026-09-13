package networkruntime

import (
	"context"
	"fmt"
	"time"

	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/foundation"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// frozenGenesis validates the exact immutable artifact before interpreting any gate.
func (r *Reconciler) frozenGenesis(ctx context.Context, root *api.StacksNetwork) (*api.StacksGenesis, error) {
	if root.Status.GenesisRef == nil {
		return nil, fmt.Errorf("genesis unavailable")
	}
	var g api.StacksGenesis
	if err := r.Reader.Get(ctx, client.ObjectKey{Namespace: root.Namespace, Name: root.Status.GenesisRef.Name}, &g); err != nil {
		return nil, err
	}
	if g.UID != root.Status.GenesisRef.UID || g.DeletionTimestamp != nil || !metav1.IsControlledBy(&g, root) || g.Spec.Source.NetworkUID != root.UID || foundation.Digest(g.Spec) != root.Status.GenesisRef.Fingerprint || foundation.Digest(g.Spec.Chain) != root.Status.GenesisDigest {
		return nil, fmt.Errorf("frozen genesis identity differs")
	}
	return &g, nil
}

// initializeGates seeds or verifies the aggregate's monotonic frozen progress record.
func initializeGates(root *api.StacksNetwork, g *api.StacksGenesis) error {
	gates := g.Spec.Bootstrap.Gates
	if len(gates) == 0 || len(gates) > 16 {
		return fmt.Errorf("frozen gates unavailable")
	}
	state := root.Status.Initialization
	if state == nil {
		state = &api.InitializationStatus{GenesisUID: g.UID, GenesisDigest: root.Status.GenesisDigest, AuthorizedCeiling: gates[0].BitcoinCeiling}
		for _, gate := range gates {
			state.Gates = append(state.Gates, api.GateObservation{Name: gate.Name})
		}
		root.Status.Initialization = state
	}
	if state.GenesisUID != g.UID || state.GenesisDigest != root.Status.GenesisDigest || len(state.Gates) != len(gates) || state.GateIndex < 0 || int(state.GateIndex) > len(gates) {
		return fmt.Errorf("gate identity changed")
	}
	for i, gate := range gates {
		if state.Gates[i].Name != gate.Name || i < int(state.GateIndex) && state.Gates[i].CompletedAt == nil || i >= int(state.GateIndex) && state.Gates[i].CompletedAt != nil {
			return fmt.Errorf("gate order differs")
		}
	}
	if state.Completed != (int(state.GateIndex) == len(gates)) {
		return fmt.Errorf("gate completion differs")
	}
	if !state.Completed && state.AuthorizedCeiling != gates[state.GateIndex].BitcoinCeiling {
		return fmt.Errorf("gate ceiling differs")
	}
	return nil
}

// projectGates advances only independently supported gates and retains first-ceiling deadlines.
func (r *Reconciler) projectGates(ctx context.Context, root *api.StacksNetwork, record *bitcoin.BitcoinInitialization, participants []api.StacksNetworkParticipant) (bool, error) {
	genesis, err := r.frozenGenesis(ctx, root)
	if err != nil {
		return false, err
	}
	if err := initializeGates(root, genesis); err != nil {
		return false, err
	}
	state := root.Status.Initialization
	if state.Completed {
		return false, nil
	}
	gate := genesis.Spec.Bootstrap.Gates[state.GateIndex]
	observation := &state.Gates[state.GateIndex]
	now := time.Now()
	if gate.Name != api.GatePrepareBitcoin && gate.Name != api.GateEnrollPoX4 && gate.Name != api.GatePrepareNakamoto && gate.Name != api.GatePreparePoX5 && gate.Name != api.GateEnrollPoX5 && gate.Name != api.GatePrepareWaterfall {
		set(root, api.ConditionInitialized, metav1.ConditionFalse, reasonGateRuntimeNotImplemented, "The next frozen protocol gate is not implemented yet")
		return false, nil
	}
	// The first gate owns its own exact observation boundary in its execution record.
	if gate.Name == api.GatePrepareBitcoin && record.Status.FirstCeilingObservedAt != nil && observation.FirstCeilingObservedAt == nil {
		observation.FirstCeilingObservedAt = record.Status.FirstCeilingObservedAt.DeepCopy()
	}
	height, known, err := r.initializationHeight(ctx, root, record, now)
	if err != nil {
		return false, err
	}
	if known && height > gate.BitcoinCeiling {
		root.Status.Phase = api.NetworkPhaseFailed
		set(root, api.ConditionFailed, metav1.ConditionTrue, api.ReasonFrozenCeilingExceeded, "The frozen initialization ceiling was exceeded; recreate the network")
		return true, nil
	}
	if known && height == gate.BitcoinCeiling && observation.FirstCeilingObservedAt == nil {
		at := metav1.NewTime(now)
		observation.FirstCeilingObservedAt = &at
	}
	preparedInTime := gate.Name == api.GatePrepareBitcoin && record.Status.PreparedAt != nil && !record.Status.PreparedAt.Time.After(now) && (record.Status.FirstCeilingObservedAt == nil || record.Status.PreparedAt.Time.Before(record.Status.FirstCeilingObservedAt.Add(120*time.Second)))
	if !preparedInTime && observation.FirstCeilingObservedAt != nil && !now.Before(observation.FirstCeilingObservedAt.Add(120*time.Second)) {
		root.Status.Phase = api.NetworkPhaseFailed
		set(root, api.ConditionFailed, metav1.ConditionTrue, reasonBootstrapObservationDeadline, "The frozen gate observation deadline expired; recreate the network")
		return true, nil
	}
	complete := false
	switch gate.Name {
	case api.GatePrepareBitcoin:
		complete = preparedInTime
	case api.GateEnrollPoX4:
		complete = known && pox4CohortSatisfied(root, genesis, participants, gate, now)
	case api.GatePrepareNakamoto:
		complete = known && pox4CohortSatisfied(root, genesis, participants, gate, now) && preparedCohortSatisfied(root, genesis, participants, gate, now)
	case api.GatePreparePoX5:
		complete = known && contractCohortSatisfied(root, genesis, participants, now)
	case api.GateEnrollPoX5:
		complete = known && pox5CohortSatisfied(root, genesis, participants, gate, now)
	case api.GatePrepareWaterfall:
		complete = known && pox5CohortSatisfied(root, genesis, participants, gate, now) && preparedCohortSatisfied(root, genesis, participants, gate, now)
	default:
		set(root, api.ConditionInitialized, metav1.ConditionFalse, reasonGateRuntimeNotImplemented, "The next frozen protocol gate is not implemented yet")
		return false, nil
	}
	if !complete {
		return false, nil
	}
	at := metav1.NewTime(now)
	if preparedInTime {
		at = *record.Status.PreparedAt
	}
	observation.CompletedAt = &at
	state.GateIndex++
	if int(state.GateIndex) == len(state.Gates) {
		state.Completed = true
	} else {
		state.AuthorizedCeiling = genesis.Spec.Bootstrap.Gates[state.GateIndex].BitcoinCeiling
	}
	return false, nil
}

// initializationHeight uses the exact initial target's successful, fresh read observation.
func (r *Reconciler) initializationHeight(ctx context.Context, root *api.StacksNetwork, record *bitcoin.BitcoinInitialization, now time.Time) (int64, bool, error) {
	if root.Status.Bitcoin == nil {
		return 0, false, nil
	}
	for _, binding := range root.Status.Bitcoin.ExecutionRefs {
		var execution bitcoin.BitcoinExecution
		if err := r.observations().Get(ctx, client.ObjectKey{Namespace: root.Namespace, Name: binding.Name}, &execution); err != nil {
			return 0, false, err
		}
		if execution.UID != binding.UID || execution.Spec.NetworkUID != root.UID || !metav1.IsControlledBy(&execution, root) {
			return 0, false, fmt.Errorf("execution identity unavailable")
		}
		if execution.Spec.Participant.UID != record.Spec.Target.UID {
			continue
		}
		o := execution.Status.Observation
		if o == nil || o.ObservedAt.Time.After(now) || now.Sub(o.ObservedAt.Time) > 16*time.Second {
			return 0, false, nil
		}
		current, err := r.currentBitcoinObservation(ctx, root, &execution)
		if err != nil || !current {
			return 0, false, err
		}
		return o.Height, true, nil
	}
	return 0, false, nil
}

// reconcileGates reports current uncertainty without erasing completed initialization.
func (r *Reconciler) reconcileGates(ctx context.Context, root *api.StacksNetwork, record *bitcoin.BitcoinInitialization, participants []api.StacksNetworkParticipant) (bool, error) {
	trial := root.DeepCopy()
	failed, err := r.projectGates(ctx, trial, record, participants)
	if err == nil && (failed || !equality.Semantic.DeepEqual(root.Status.Initialization, trial.Status.Initialization)) {
		// A durable gate transition changes authorization or starts a failure deadline.
		// Re-evaluate only this boundary with current inputs; routine projection stays cached.
		var current *bitcoin.BitcoinInitialization
		current, err = r.currentInitialization(ctx, root, record)
		freshParticipants := make([]api.StacksNetworkParticipant, len(participants))
		for i := range participants {
			if err != nil {
				break
			}
			err = r.Reader.Get(ctx, client.ObjectKeyFromObject(&participants[i]), &freshParticipants[i])
			if err == nil && freshParticipants[i].UID != participants[i].UID {
				err = fmt.Errorf("gate participant identity changed")
			}
		}
		if err == nil {
			confirmation := *r
			confirmation.Client = nil // observations use Reader for this transition only.
			trial = root.DeepCopy()
			failed, err = confirmation.projectGates(ctx, trial, current, freshParticipants)
		}
	}
	if err == nil {
		root.Status = trial.Status
	}
	if err != nil {
		if !meta.IsStatusConditionTrue(root.Status.Conditions, api.ConditionInitialized) {
			set(root, api.ConditionInitialized, metav1.ConditionUnknown, reasonGateObservationUnavailable, "Current frozen gate evidence is unavailable")
		}
		observationUnavailable(root, reasonGateObservationUnavailable, "Current frozen gate evidence is unavailable")
	}
	return failed, err
}

// currentInitialization confirms the exact record before a durable gate transition.
func (r *Reconciler) currentInitialization(ctx context.Context, root *api.StacksNetwork, record *bitcoin.BitcoinInitialization) (*bitcoin.BitcoinInitialization, error) {
	var current bitcoin.BitcoinInitialization
	if err := r.Reader.Get(ctx, client.ObjectKeyFromObject(record), &current); err != nil {
		return nil, err
	}
	if current.UID != record.UID || current.Spec.NetworkUID != root.UID || !metav1.IsControlledBy(&current, root) {
		return nil, fmt.Errorf("initialization record identity changed")
	}
	return &current, nil
}
