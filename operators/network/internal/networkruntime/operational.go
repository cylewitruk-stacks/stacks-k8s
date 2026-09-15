package networkruntime

import (
	"context"
	"time"

	vocabulary "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/bitcoincontrol"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/foundation"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

// predicate keeps known absence separate from insufficient current observations.
type predicate struct {
	status metav1.ConditionStatus
	reason string
}

// selectedMember resolves one required singleton through the root's allocated UID.
func selectedMember(
	root *api.StacksNetwork,
	kind api.ParticipantKind,
	participants []api.StacksNetworkParticipant,
) (*api.StacksNetworkParticipant, predicate) {
	for _, entry := range root.Spec.Participants {
		if entry.Kind != kind {
			continue
		}
		for _, id := range root.Status.Identities {
			if id.Name != entry.Name || id.Removing {
				continue
			}
			for i := range participants {
				p := &participants[i]
				if p.UID == id.UID && p.Spec.Kind == kind && p.Spec.ParticipantName == entry.Name &&
					p.Spec.NetworkUID == root.UID &&
					metav1.IsControlledBy(p, root) &&
					p.DeletionTimestamp == nil {
					if p.Status.Admission == nil {
						return nil, predicate{metav1.ConditionUnknown, string(kind) + api.ReasonAdmissionUnavailable}
					}
					return p, predicate{metav1.ConditionTrue, reasonSelected}
				}
			}
		}
		return nil, predicate{metav1.ConditionUnknown, string(kind) + reasonInstanceUnavailable}
	}
	return nil, predicate{metav1.ConditionFalse, string(kind) + reasonMissing}
}

// capabilityPaused includes newer root intent before control projection reaches the instance.
func capabilityPaused(root *api.StacksNetwork, p *api.StacksNetworkParticipant) bool {
	if p.Spec.Control != nil && ptr.Deref(p.Spec.Control.Paused, false) {
		return true
	}
	for _, entry := range root.Spec.Participants {
		if entry.Name == p.Spec.ParticipantName && entry.Control != nil && ptr.Deref(entry.Control.Paused, false) {
			return true
		}
	}
	return false
}

// currentWorker requires a current execution observation from the bound surviving process.
func currentWorker(root *api.StacksNetwork, p *api.StacksNetworkParticipant, now time.Time) bool {
	e := p.Status.Execution
	if e == nil || e.ProcessNonce == "" || e.ObservedGeneration != p.Generation ||
		e.NetworkGeneration != root.Generation ||
		!fresh(e.ObservedAt, now) ||
		e.Phase == api.WorkerPhaseInactive ||
		e.Phase == api.WorkerPhaseFailed ||
		e.Phase == api.WorkerPhaseUnknown ||
		e.Phase == api.WorkerPhaseDraining ||
		e.Phase == api.WorkerPhaseSettled ||
		e.Phase == api.WorkerPhaseUnsettled {
		return false
	}
	for _, id := range root.Status.Identities {
		if id.UID == p.UID && id.Name == p.Spec.ParticipantName && !id.Removing && id.Worker != nil &&
			id.Worker.Shutdown == nil &&
			id.Worker.Disposal == nil &&
			id.Worker.Pod.UID == e.PodUID &&
			id.Worker.ProfileDigest == e.ProfileDigest {
			return true
		}
	}
	return false
}

// bitcoinOperational separates fresh target availability from original receipt progress.
func bitcoinOperational(root *api.StacksNetwork, participants []api.StacksNetworkParticipant, now time.Time) predicate {
	p, result := selectedMember(root, api.ParticipantBitcoinBlockProduction, participants)
	if p == nil {
		return result
	}
	if capabilityPaused(root, p) {
		return predicate{metav1.ConditionFalse, reasonBitcoinProductionPaused}
	}
	s := p.Status.Scheduling
	if s == nil || s.ObservedAt == nil || !fresh(*s.ObservedAt, now) ||
		s.PolicyDigest != p.Status.Admission.PolicyDigest ||
		s.Schedule == nil {
		return predicate{metav1.ConditionUnknown, reasonBitcoinObservationUnavailable}
	}
	if s.EligibleTargets == 0 {
		return predicate{metav1.ConditionFalse, reasonBitcoinTargetsUnavailable}
	}
	_, window, valid := bitcoinTiming(s)
	if !valid {
		return predicate{metav1.ConditionUnknown, reasonBitcoinTimingUnavailable}
	}
	if s.LastAcknowledgedAt == nil || s.LastAcknowledgedAt.After(now) ||
		now.Sub(s.LastAcknowledgedAt.Time) > window {
		return predicate{metav1.ConditionFalse, reasonBitcoinProgressOverdue}
	}
	return predicate{metav1.ConditionTrue, reasonBitcoinProgressObserved}
}

// bitcoinTiming validates the current scheduler's claimed cadence and progress window.
func bitcoinTiming(s *vocabulary.BitcoinSchedulingStatus) (time.Duration, time.Duration, bool) {
	if s == nil || s.Schedule == nil {
		return 0, 0, false
	}
	var bound time.Duration
	var err error
	switch s.Schedule.Cadence.Mode {
	case vocabulary.CadenceFixed:
		if s.Schedule.Cadence.Interval != nil {
			bound, err = time.ParseDuration(string(*s.Schedule.Cadence.Interval))
		}
	case vocabulary.CadenceUniform:
		if s.Schedule.Cadence.MaximumInterval != nil {
			bound, err = time.ParseDuration(string(*s.Schedule.Cadence.MaximumInterval))
		}
	}
	window := foundation.ProgressWindow(bound)
	return bound, window, err == nil && bound >= time.Second && bound <= time.Hour &&
		s.ProgressWindowSeconds == int64(window/time.Second)
}

// minerOperational excludes intentionally unverified and suspended peers from the required miner set.
func minerOperational(root *api.StacksNetwork, participants []api.StacksNetworkParticipant) predicate {
	waiting := false
	for _, entry := range root.Spec.Participants {
		if entry.Kind != api.ParticipantStacksNode {
			continue
		}
		for i := range participants {
			p := &participants[i]
			if p.Spec.ParticipantName != entry.Name || !selectedInstance(root, p) ||
				p.Spec.Kind != api.ParticipantStacksNode ||
				p.Status.Admission == nil ||
				p.DeletionTimestamp != nil ||
				!metav1.IsControlledBy(p, root) ||
				actorSuspended(root, p) {
				continue
			}
			policy := p.Status.Admission.Configuration.StacksNode
			if policy == nil || policy.Mining == nil || !ptr.Deref(policy.Mining.Enabled, false) ||
				policy.Config != nil && ptr.Deref(policy.Config.Compatibility, "") == common.CompatibilityUnverified {
				continue
			}
			if currentActorReady(p) {
				return predicate{metav1.ConditionTrue, reasonMinerReady}
			}
			c := meta.FindStatusCondition(p.Status.Conditions, api.ConditionWorkloadReady)
			if c == nil || c.ObservedGeneration != p.Generation || c.Status == metav1.ConditionUnknown {
				waiting = true
			}
		}
	}
	if waiting {
		return predicate{metav1.ConditionUnknown, reasonMinerObservationUnavailable}
	}
	return predicate{metav1.ConditionFalse, reasonNoReadyMiner}
}

// trafficOperational requires current canonical inclusion, exact original ingress, and recent progress.
func trafficOperational(
	root *api.StacksNetwork,
	g *api.StacksGenesis,
	participants []api.StacksNetworkParticipant,
	now time.Time,
) predicate {
	p, result := selectedMember(root, api.ParticipantStacksTransactionProduction, participants)
	if p == nil {
		return result
	}
	if capabilityPaused(root, p) {
		return predicate{metav1.ConditionFalse, reasonTrafficPaused}
	}
	if !currentWorker(root, p, now) || p.Status.Execution.Traffic == nil || p.Status.Execution.Transactions == nil {
		return predicate{metav1.ConditionUnknown, reasonTrafficObservationUnavailable}
	}
	o := p.Status.Execution.Traffic
	if !o.Available || !fresh(o.ObservedAt, now) || o.GenesisUID != g.UID {
		return predicate{metav1.ConditionUnknown, reasonTrafficObservationUnavailable}
	}
	if !o.Found || !o.Success {
		return predicate{metav1.ConditionFalse, reasonCanonicalTransferUnavailable}
	}
	ingress := false
	for i := range participants {
		target := &participants[i]
		if target.UID != o.TargetParticipantUID || !selectedInstance(root, target) ||
			target.Spec.Kind != api.ParticipantStacksNode ||
			target.Spec.NetworkUID != root.UID ||
			!metav1.IsControlledBy(target, root) ||
			target.DeletionTimestamp != nil ||
			!currentActorReady(target) {
			continue
		}
		for _, entry := range root.Spec.Participants {
			if entry.Kind == target.Spec.Kind && entry.Name == target.Spec.ParticipantName {
				ingress = target.Status.Runtime.PodRef.UID == o.TargetPodUID &&
					target.Status.Runtime.ContainerID == o.TargetContainerID &&
					target.Status.Runtime.ConfigurationDigest == o.TargetConfigurationDigest
			}
		}
	}
	if !ingress {
		return predicate{metav1.ConditionUnknown, reasonTrafficIngressUnavailable}
	}
	if o.EffectiveIntervalSeconds < 1 || o.EffectiveIntervalSeconds > 3600 {
		return predicate{metav1.ConditionUnknown, reasonTrafficTimingUnavailable}
	}
	interval := time.Duration(o.EffectiveIntervalSeconds) * time.Second
	if interval < time.Second || interval > time.Hour ||
		// #nosec G115 -- The interval is bounded to 1s..1h; ProgressWindow returns 130s..10810s.
		o.ProgressWindowSeconds != uint64(foundation.ProgressWindow(interval)/time.Second) {
		return predicate{metav1.ConditionUnknown, reasonTrafficTimingUnavailable}
	}
	inclusion := p.Status.Execution.Transactions.LastInclusion
	if inclusion == nil || inclusion.TxID != o.TxID || !inclusion.Success || inclusion.ObservedAt.After(now) ||
		now.Sub(inclusion.ObservedAt.Time) > foundation.ProgressWindow(interval) {
		return predicate{metav1.ConditionFalse, reasonTrafficProgressOverdue}
	}
	return predicate{metav1.ConditionTrue, reasonCanonicalTransferObserved}
}

// contractsOperational uses current membership and frozen public contract inputs without replaying gates.
func contractsOperational(
	root *api.StacksNetwork,
	g *api.StacksGenesis,
	participants []api.StacksNetworkParticipant,
	now time.Time,
) predicate {
	p, result := selectedMember(root, api.ParticipantStacksContractSet, participants)
	if p == nil {
		return result
	}
	if !currentWorker(root, p, now) {
		return predicate{metav1.ConditionUnknown, api.ReasonContractObservationUnavailable}
	}
	o := p.Status.Execution.Contracts
	if o == nil || !fresh(o.ObservedAt, now) {
		return predicate{metav1.ConditionUnknown, api.ReasonContractObservationUnavailable}
	}
	policy := p.Status.Admission.Configuration.StacksContractSet
	if policy == nil {
		return predicate{metav1.ConditionUnknown, reasonContractAdmissionUnavailable}
	}
	for _, req := range g.Spec.Bootstrap.Requirements {
		if req.Kind == api.ParticipantStacksContractSet &&
			foundation.Digest(req.RegistryInitialization) == foundation.Digest(policy.Initialization) &&
			contractObservationMatches(o, g, req, now) {
			return predicate{metav1.ConditionTrue, reasonContractsObserved}
		}
	}
	return predicate{metav1.ConditionFalse, reasonContractPostconditionsDiffer}
}

// combinePredicates gives known unmet requirements precedence over stale observations.
func combinePredicates(values ...predicate) predicate {
	for _, p := range values {
		if p.status == metav1.ConditionFalse {
			return p
		}
	}
	for _, p := range values {
		if p.status != metav1.ConditionTrue {
			return p
		}
	}
	return predicate{metav1.ConditionTrue, reasonBaselineProgressObserved}
}

// projectOperation distinguishes completed initialization, requested operation and recent protocol progress.
func (r *Reconciler) projectOperation(
	ctx context.Context,
	root *api.StacksNetwork,
	participants []api.StacksNetworkParticipant,
	now time.Time,
) error {
	state := root.Status.Initialization
	if state == nil || !state.Completed {
		root.Status.BurnchainObservations = nil
		return nil
	}
	g, err := r.frozenGenesis(ctx, root)
	if err != nil {
		root.Status.BurnchainObservations = nil
		return err
	}
	root.Status.BurnchainObservations = r.burnchainObservations(ctx, root, g, participants, now)
	traffic := trafficOperational(root, g, participants, now)
	if !meta.IsStatusConditionTrue(root.Status.Conditions, api.ConditionInitialized) {
		if traffic.status != metav1.ConditionTrue || !postWaterfallObserved(root, g, participants, now) {
			return nil
		}
		set(
			root,
			api.ConditionInitialized,
			metav1.ConditionTrue,
			reasonBootstrapCompleted,
			"Frozen gates and post-waterfall canonical production are observed",
		)
	}
	if root.Spec.Operation != api.NetworkOperationRunning {
		return nil
	}
	root.Status.Phase = api.NetworkPhaseRunning
	set(
		root,
		api.ConditionRunning,
		metav1.ConditionTrue,
		api.ReasonRunning,
		"Initialization is complete and running operation is requested",
	)
	bitcoin := bitcoinOperational(root, participants, now)
	if bitcoin.status == metav1.ConditionTrue {
		production, _ := selectedMember(root, api.ParticipantBitcoinBlockProduction, participants)
		if err := bitcoincontrol.ValidateSchedulingOverride(
			ctx,
			r.Reader,
			root,
			production,
			production.Status.Scheduling,
			now,
		); err != nil {
			bitcoin = predicate{metav1.ConditionUnknown, reasonBitcoinTimingUnavailable}
		}
	}
	miner := minerOperational(root, participants)
	result := combinePredicates(
		bitcoin,
		miner,
		traffic,
		contractsOperational(root, g, participants, now),
	)
	set(
		root,
		api.ConditionOperational,
		result.status,
		result.reason,
		"Recent baseline availability and canonical progress are assessed independently of experiment controls",
	)
	return nil
}

// postWaterfallObserved requires a newly submitted transfer and the initial nodes beyond the final ceiling.
func postWaterfallObserved(
	root *api.StacksNetwork,
	g *api.StacksGenesis,
	participants []api.StacksNetworkParticipant,
	now time.Time,
) bool {
	gates := g.Spec.Bootstrap.Gates
	if len(gates) == 0 {
		return false
	}
	ceiling := gates[len(gates)-1].BitcoinCeiling
	if ceiling < 0 {
		return false
	}
	boundary := uint64(ceiling) + 1
	traffic, _ := selectedMember(root, api.ParticipantStacksTransactionProduction, participants)
	if traffic == nil || traffic.Status.Execution == nil || traffic.Status.Execution.Traffic == nil ||
		traffic.Status.Execution.Traffic.SubmittedBurnHeight < boundary {
		return false
	}
	found := false
	for _, req := range g.Spec.Bootstrap.Requirements {
		if req.Kind != api.ParticipantStacksNode {
			continue
		}
		found = true
		p := requiredParticipant(root, req, participants)
		if p == nil || !currentActorReady(p) {
			return false
		}
		o := p.Status.Runtime.Protocol
		if o == nil || !o.Available || !o.FullySynced || !fresh(o.ObservedAt, now) || o.GenesisUID != g.UID ||
			o.NetworkID != 0x80000000 ||
			o.PodUID != p.Status.Runtime.PodRef.UID ||
			o.ContainerID != p.Status.Runtime.ContainerID ||
			o.ConfigurationDigest != p.Status.Runtime.ConfigurationDigest ||
			o.BurnHeight < boundary ||
			o.PoXContract != "ST000000000000000000002AMW42H.pox-5" {
			return false
		}
	}
	return found
}
