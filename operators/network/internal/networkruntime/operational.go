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
func selectedMember(root *api.StacksNetwork, kind api.ParticipantKind, participants []api.StacksNetworkParticipant) (*api.StacksNetworkParticipant, predicate) {
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
				if p.UID == id.UID && p.Spec.Kind == kind && p.Spec.ParticipantName == entry.Name && p.Spec.NetworkUID == root.UID && metav1.IsControlledBy(p, root) && p.DeletionTimestamp == nil {
					if p.Status.Admission == nil {
						return nil, predicate{metav1.ConditionUnknown, string(kind) + "AdmissionUnavailable"}
					}
					return p, predicate{metav1.ConditionTrue, "Selected"}
				}
			}
		}
		return nil, predicate{metav1.ConditionUnknown, string(kind) + "InstanceUnavailable"}
	}
	return nil, predicate{metav1.ConditionFalse, string(kind) + "Missing"}
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
	if e == nil || e.ProcessNonce == "" || e.ObservedGeneration != p.Generation || e.NetworkGeneration != root.Generation || !fresh(e.ObservedAt, now) || e.Phase == api.WorkerPhaseInactive || e.Phase == api.WorkerPhaseFailed || e.Phase == api.WorkerPhaseUnknown || e.Phase == api.WorkerPhaseDraining || e.Phase == api.WorkerPhaseSettled || e.Phase == api.WorkerPhaseUnsettled {
		return false
	}
	for _, id := range root.Status.Identities {
		if id.UID == p.UID && id.Name == p.Spec.ParticipantName && !id.Removing && id.Worker != nil && id.Worker.Shutdown == nil && id.Worker.Disposal == nil && id.Worker.Pod.UID == e.PodUID && id.Worker.ProfileDigest == e.ProfileDigest {
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
		return predicate{metav1.ConditionFalse, "BitcoinProductionPaused"}
	}
	s := p.Status.Scheduling
	if s == nil || s.ObservedAt == nil || !fresh(*s.ObservedAt, now) || s.PolicyDigest != p.Status.Admission.PolicyDigest || s.Schedule == nil {
		return predicate{metav1.ConditionUnknown, "BitcoinObservationUnavailable"}
	}
	if s.EligibleTargets == 0 {
		return predicate{metav1.ConditionFalse, "BitcoinTargetsUnavailable"}
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
	if err != nil || bound < time.Second || bound > time.Hour || s.ProgressWindowSeconds != int64(foundation.ProgressWindow(bound)/time.Second) {
		return predicate{metav1.ConditionUnknown, "BitcoinTimingUnavailable"}
	}
	if s.LastAcknowledgedAt == nil || s.LastAcknowledgedAt.Time.After(now) || now.Sub(s.LastAcknowledgedAt.Time) > foundation.ProgressWindow(bound) {
		return predicate{metav1.ConditionFalse, "BitcoinProgressOverdue"}
	}
	return predicate{metav1.ConditionTrue, "BitcoinProgressObserved"}
}

// minerOperational excludes intentionally unverified and suspended peers from the required miner set.
func minerOperational(root *api.StacksNetwork, participants []api.StacksNetworkParticipant) predicate {
	waiting := false
	for _, entry := range root.Spec.Participants {
		if entry.Kind != api.ParticipantStacksNode || entry.Control != nil && ptr.Deref(entry.Control.Suspended, false) {
			continue
		}
		for i := range participants {
			p := &participants[i]
			if p.Spec.ParticipantName != entry.Name || !selectedInstance(root, p) || p.Spec.Kind != api.ParticipantStacksNode || p.Status.Admission == nil || p.DeletionTimestamp != nil || !metav1.IsControlledBy(p, root) || p.Spec.Control != nil && ptr.Deref(p.Spec.Control.Suspended, false) {
				continue
			}
			policy := p.Status.Admission.Configuration.StacksNode
			if policy == nil || policy.Mining == nil || !ptr.Deref(policy.Mining.Enabled, false) || policy.Config != nil && ptr.Deref(policy.Config.Compatibility, "") == common.CompatibilityUnverified {
				continue
			}
			if currentActorReady(p) {
				return predicate{metav1.ConditionTrue, "MinerReady"}
			}
			c := meta.FindStatusCondition(p.Status.Conditions, "WorkloadReady")
			if c == nil || c.ObservedGeneration != p.Generation || c.Status == metav1.ConditionUnknown {
				waiting = true
			}
		}
	}
	if waiting {
		return predicate{metav1.ConditionUnknown, "MinerObservationUnavailable"}
	}
	return predicate{metav1.ConditionFalse, "NoReadyMiner"}
}

// trafficOperational requires current canonical inclusion, exact original ingress, and recent progress.
func trafficOperational(root *api.StacksNetwork, g *api.StacksGenesis, participants []api.StacksNetworkParticipant, now time.Time) predicate {
	p, result := selectedMember(root, api.ParticipantStacksTransactionProduction, participants)
	if p == nil {
		return result
	}
	if capabilityPaused(root, p) {
		return predicate{metav1.ConditionFalse, "TrafficPaused"}
	}
	if !currentWorker(root, p, now) || p.Status.Execution.Traffic == nil || p.Status.Execution.Transactions == nil {
		return predicate{metav1.ConditionUnknown, "TrafficObservationUnavailable"}
	}
	o := p.Status.Execution.Traffic
	if !o.Available || !fresh(o.ObservedAt, now) || o.GenesisUID != g.UID {
		return predicate{metav1.ConditionUnknown, "TrafficObservationUnavailable"}
	}
	if !o.Found || !o.Success {
		return predicate{metav1.ConditionFalse, "CanonicalTransferUnavailable"}
	}
	ingress := false
	for i := range participants {
		target := &participants[i]
		if target.UID != o.TargetParticipantUID || !selectedInstance(root, target) || target.Spec.Kind != api.ParticipantStacksNode || target.Spec.NetworkUID != root.UID || !metav1.IsControlledBy(target, root) || target.DeletionTimestamp != nil || !currentActorReady(target) {
			continue
		}
		for _, entry := range root.Spec.Participants {
			if entry.Kind == target.Spec.Kind && entry.Name == target.Spec.ParticipantName {
				ingress = target.Status.Runtime.PodRef.UID == o.TargetPodUID && target.Status.Runtime.ContainerID == o.TargetContainerID && target.Status.Runtime.ConfigurationDigest == o.TargetConfigurationDigest
			}
		}
	}
	if !ingress {
		return predicate{metav1.ConditionUnknown, "TrafficIngressUnavailable"}
	}
	interval := time.Duration(o.EffectiveIntervalSeconds) * time.Second
	if interval < time.Second || interval > time.Hour || o.ProgressWindowSeconds != uint64(foundation.ProgressWindow(interval)/time.Second) {
		return predicate{metav1.ConditionUnknown, "TrafficTimingUnavailable"}
	}
	inclusion := p.Status.Execution.Transactions.LastInclusion
	if inclusion == nil || inclusion.TxID != o.TxID || !inclusion.Success || inclusion.ObservedAt.Time.After(now) || now.Sub(inclusion.ObservedAt.Time) > foundation.ProgressWindow(interval) {
		return predicate{metav1.ConditionFalse, "TrafficProgressOverdue"}
	}
	return predicate{metav1.ConditionTrue, "CanonicalTransferObserved"}
}

// contractsOperational uses current membership and frozen public contract inputs without replaying gates.
func contractsOperational(root *api.StacksNetwork, g *api.StacksGenesis, participants []api.StacksNetworkParticipant, now time.Time) predicate {
	p, result := selectedMember(root, api.ParticipantStacksContractSet, participants)
	if p == nil {
		return result
	}
	if !currentWorker(root, p, now) {
		return predicate{metav1.ConditionUnknown, "ContractObservationUnavailable"}
	}
	o := p.Status.Execution.Contracts
	if o == nil || !fresh(o.ObservedAt, now) {
		return predicate{metav1.ConditionUnknown, "ContractObservationUnavailable"}
	}
	policy := p.Status.Admission.Configuration.StacksContractSet
	if policy == nil {
		return predicate{metav1.ConditionUnknown, "ContractAdmissionUnavailable"}
	}
	for _, req := range g.Spec.Bootstrap.Requirements {
		if req.Kind == api.ParticipantStacksContractSet && foundation.Digest(req.RegistryInitialization) == foundation.Digest(policy.Initialization) && contractObservationMatches(o, g, req, now) {
			return predicate{metav1.ConditionTrue, "ContractsObserved"}
		}
	}
	return predicate{metav1.ConditionFalse, "ContractPostconditionsDiffer"}
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
	return predicate{metav1.ConditionTrue, "BaselineProgressObserved"}
}

// projectOperation distinguishes completed initialization, requested operation and recent protocol progress.
func (r *Reconciler) projectOperation(ctx context.Context, root *api.StacksNetwork, participants []api.StacksNetworkParticipant, now time.Time) error {
	state := root.Status.Initialization
	if state == nil || !state.Completed {
		return nil
	}
	g, err := r.frozenGenesis(ctx, root)
	if err != nil {
		return err
	}
	traffic := trafficOperational(root, g, participants, now)
	if !meta.IsStatusConditionTrue(root.Status.Conditions, "Initialized") {
		if traffic.status != metav1.ConditionTrue || !postWaterfallObserved(root, g, participants, now) {
			return nil
		}
		set(root, "Initialized", metav1.ConditionTrue, "BootstrapCompleted", "Frozen gates and post-waterfall canonical production are observed")
	}
	if root.Spec.Operation != api.NetworkOperationRunning {
		return nil
	}
	root.Status.Phase = api.NetworkPhaseRunning
	set(root, "Running", metav1.ConditionTrue, "Running", "Initialization is complete and running operation is requested")
	bitcoin := bitcoinOperational(root, participants, now)
	if bitcoin.status == metav1.ConditionTrue {
		production, _ := selectedMember(root, api.ParticipantBitcoinBlockProduction, participants)
		if err := bitcoincontrol.ValidateSchedulingOverride(ctx, r.Reader, root, production, production.Status.Scheduling, now); err != nil {
			bitcoin = predicate{metav1.ConditionUnknown, "BitcoinTimingUnavailable"}
		}
	}
	result := combinePredicates(bitcoin, minerOperational(root, participants), traffic, contractsOperational(root, g, participants, now))
	set(root, "Operational", result.status, result.reason, "Recent baseline availability and canonical progress are assessed independently of experiment controls")
	return nil
}

// postWaterfallObserved requires a newly submitted transfer and the initial nodes beyond the final ceiling.
func postWaterfallObserved(root *api.StacksNetwork, g *api.StacksGenesis, participants []api.StacksNetworkParticipant, now time.Time) bool {
	gates := g.Spec.Bootstrap.Gates
	if len(gates) == 0 {
		return false
	}
	boundary := uint64(gates[len(gates)-1].BitcoinCeiling + 1)
	traffic, _ := selectedMember(root, api.ParticipantStacksTransactionProduction, participants)
	if traffic == nil || traffic.Status.Execution == nil || traffic.Status.Execution.Traffic == nil || traffic.Status.Execution.Traffic.SubmittedBurnHeight < boundary {
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
		if o == nil || !o.Available || !o.FullySynced || !fresh(o.ObservedAt, now) || o.GenesisUID != g.UID || o.NetworkID != 0x80000000 || o.PodUID != p.Status.Runtime.PodRef.UID || o.ContainerID != p.Status.Runtime.ContainerID || o.ConfigurationDigest != p.Status.Runtime.ConfigurationDigest || o.BurnHeight < boundary || o.PoXContract != "ST000000000000000000002AMW42H.pox-5" {
			return false
		}
	}
	return found
}
