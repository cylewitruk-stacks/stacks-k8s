package networkruntime

import (
	"sort"
	"time"

	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/foundation"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// preparedCohortSatisfied compares canonical protocol-derived signer sets across required nodes.
func preparedCohortSatisfied(root *api.StacksNetwork, g *api.StacksGenesis, participants []api.StacksNetworkParticipant, gate api.Gate, now time.Time) bool {
	if gate.TargetCycle == nil || *gate.TargetCycle < 0 {
		return false
	}
	expected := map[string]bool{}
	nodes := []*api.StacksNetworkParticipant{}
	for _, requirement := range g.Spec.Bootstrap.Requirements {
		if requirement.Kind != api.ParticipantStacksNode && requirement.Kind != api.ParticipantStacksSigner {
			continue
		}
		var p *api.StacksNetworkParticipant
		for i := range participants {
			if participants[i].UID == requirement.Participant.UID {
				p = &participants[i]
				break
			}
		}
		if p == nil || p.Spec.Kind != requirement.Kind || p.Spec.NetworkUID != root.UID || !metav1.IsControlledBy(p, root) || p.DeletionTimestamp != nil || p.Status.Admission == nil || !foundation.BootstrapPolicyCompatible(requirement, p.Status.Admission.Configuration) || !currentActorReady(p) {
			return false
		}
		selected := false
		for _, entry := range root.Spec.Participants {
			if entry.Name == p.Spec.ParticipantName && entry.Kind == p.Spec.Kind {
				selected = true
			}
		}
		if !selected {
			return false
		}
		if requirement.Kind == api.ParticipantStacksNode {
			nodes = append(nodes, p)
			continue
		}
		signer := p.Status.Admission.Configuration.StacksSigner
		if signer == nil || signer.AccountRef == nil {
			return false
		}
		key := ""
		for _, account := range requirement.Accounts {
			if account.Binding.Name == signer.AccountRef.Name {
				key = account.Identity.PublicKey
			}
		}
		if key == "" {
			return false
		}
		expected[key] = true
	}
	if len(nodes) == 0 || len(expected) == 0 {
		return false
	}
	canonicalSet := ""
	for _, node := range nodes {
		state := node.Status.Runtime
		observation := state.Protocol
		if observation == nil || !observation.Available || !observation.FullySynced || observation.NetworkID != 0x80000000 || observation.GenesisUID != g.UID || observation.PodUID != state.PodRef.UID || observation.ContainerID != state.ContainerID || observation.ConfigurationDigest != state.ConfigurationDigest || !fresh(observation.ObservedAt, now) {
			return false
		}
		set := observation.PreparedSet
		if set == nil || !set.Available || set.Cycle != uint64(*gate.TargetCycle) || !fresh(set.ObservedAt, now) || len(set.Signers) != len(expected) || set.Threshold == "" {
			return false
		}
		seen := map[string]bool{}
		sorted := append([]api.PreparedSignerObservation(nil), set.Signers...)
		for _, signer := range sorted {
			if !expected[signer.PublicKey] || seen[signer.PublicKey] || signer.Weight == 0 || signer.StackedAmount == "" || signer.StackedAmount == "0" {
				return false
			}
			seen[signer.PublicKey] = true
		}
		sort.Slice(sorted, func(i, j int) bool { return sorted[i].PublicKey < sorted[j].PublicKey })
		digest := foundation.Digest(struct {
			Version   uint32
			Threshold string
			Signers   []api.PreparedSignerObservation
		}{set.Version, set.Threshold, sorted})
		if canonicalSet != "" && canonicalSet != digest {
			return false
		}
		canonicalSet = digest
	}
	return true
}

// currentActorReady requires current domain acknowledgement and exact process metadata.
func currentActorReady(p *api.StacksNetworkParticipant) bool {
	state := p.Status.Runtime
	if state == nil || state.Terminated || state.ObservedGeneration != p.Generation || state.PodRef == nil || state.ContainerID == "" || p.Status.Admission == nil || state.PolicyDigest != p.Status.Admission.PolicyDigest {
		return false
	}
	for _, kind := range []string{api.ConditionConfigVerified, api.ConditionWorkloadReady} {
		c := meta.FindStatusCondition(p.Status.Conditions, kind)
		if c == nil || c.Status != metav1.ConditionTrue || c.ObservedGeneration != p.Generation {
			return false
		}
	}
	return true
}

// fresh enforces the release's 2s poll and 10s RPC allowance observation window.
func fresh(at metav1.Time, now time.Time) bool {
	return !at.IsZero() && !at.Time.After(now) && now.Sub(at.Time) <= foundation.ObservationFreshness()
}
