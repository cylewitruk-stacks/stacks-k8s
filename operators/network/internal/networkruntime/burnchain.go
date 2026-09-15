package networkruntime

import (
	"context"
	"reflect"
	"time"

	vocabulary "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// burnchainObservations projects facts without letting observation failures inhibit health reporting.
func (r *Reconciler) burnchainObservations(
	ctx context.Context,
	root *api.StacksNetwork,
	g *api.StacksGenesis,
	participants []api.StacksNetworkParticipant,
	now time.Time,
) *api.BurnchainObservations {
	current := &api.BurnchainObservations{
		Bitcoin: r.currentBitcoinSample(ctx, root, participants, now),
		Miner:   currentMinerSample(root, g, participants, now),
	}
	if current.Bitcoin == nil && current.Miner == nil {
		return nil
	}
	if previous := root.Status.BurnchainObservations; previous != nil {
		current.Bitcoin = retainHeightSample(previous.Bitcoin, current.Bitcoin)
		current.Miner = retainHeightSample(previous.Miner, current.Miner)
	}
	return current
}

// retainHeightSample preserves the sequence start only while fresh facts and identity match.
// A withdrawn observation never inherits a prior sequence on recovery.
func retainHeightSample(previous, current *api.BurnHeightSample) *api.BurnHeightSample {
	if previous == nil || current == nil || previous.FirstObservedAt.IsZero() ||
		previous.FirstObservedAt.After(current.FirstObservedAt.Time) {
		return current
	}
	facts := *previous
	facts.FirstObservedAt = current.FirstObservedAt
	if reflect.DeepEqual(facts, *current) {
		current.FirstObservedAt = previous.FirstObservedAt
	}
	return current
}

// burnHeightSample starts a sequence from a fresh source timestamp and exact runtime identity.
func burnHeightSample(p *api.StacksNetworkParticipant, height uint64, observed metav1.Time) *api.BurnHeightSample {
	return &api.BurnHeightSample{
		Participant:         common.Binding{Kind: api.KindStacksNetworkParticipant, Name: p.Name, UID: p.UID},
		Pod:                 *p.Status.Runtime.PodRef,
		ContainerID:         p.Status.Runtime.ContainerID,
		ConfigurationDigest: p.Status.Runtime.ConfigurationDigest,
		Height:              height,
		FirstObservedAt:     observed,
	}
}

// greatestSample uses participant UID as a stable tie-breaker, independent of list order.
func greatestSample(current, candidate *api.BurnHeightSample) *api.BurnHeightSample {
	if current == nil || candidate.Height > current.Height ||
		candidate.Height == current.Height && candidate.Participant.UID < current.Participant.UID {
		return candidate
	}
	return current
}

// actorSuspended honors current root intent and projected participant control.
func actorSuspended(root *api.StacksNetwork, p *api.StacksNetworkParticipant) bool {
	if p.Spec.Control != nil && ptr.Deref(p.Spec.Control.Suspended, false) {
		return true
	}
	for _, entry := range root.Spec.Participants {
		if entry.Name == p.Spec.ParticipantName && entry.Control != nil && ptr.Deref(entry.Control.Suspended, false) {
			return true
		}
	}
	return false
}

// currentBitcoinSample returns the greatest fresh observation from a selected exact actor identity.
func (r *Reconciler) currentBitcoinSample(
	ctx context.Context,
	root *api.StacksNetwork,
	participants []api.StacksNetworkParticipant,
	now time.Time,
) *api.BurnHeightSample {
	if root.Status.Bitcoin == nil {
		return nil
	}
	byUID := make(map[string]*api.StacksNetworkParticipant, len(participants))
	for i := range participants {
		byUID[string(participants[i].UID)] = &participants[i]
	}
	var sample *api.BurnHeightSample
	for _, ref := range root.Status.Bitcoin.ExecutionRefs {
		var execution vocabulary.BitcoinExecution
		if err := r.observations().Get(
			ctx,
			client.ObjectKey{Namespace: root.Namespace, Name: ref.Name},
			&execution,
		); err != nil {
			return nil
		}
		if execution.UID != ref.UID || execution.Spec.NetworkUID != root.UID ||
			!metav1.IsControlledBy(&execution, root) {
			return nil
		}
		p := byUID[string(execution.Spec.Participant.UID)]
		o := execution.Status.Observation
		if p == nil || p.UID != execution.Spec.Participant.UID || !selectedInstance(root, p) ||
			p.Spec.Kind != api.ParticipantBitcoinNode || !currentActorReady(p) ||
			o == nil || o.Height < 0 || !fresh(o.ObservedAt, now) || !bitcoinTargetMatchesRuntime(p, o) ||
			!bitcoinEndpointMatches(p.Status.Runtime, &o.Target, p.Status.Runtime.PodIP) {
			continue
		}
		// #nosec G115 -- Negative observation heights are rejected above.
		candidate := burnHeightSample(p, uint64(o.Height), o.ObservedAt)
		sample = greatestSample(sample, candidate)
	}
	return sample
}

// currentMinerSample returns the greatest fresh exact processed height from verified enabled miners.
func currentMinerSample(
	root *api.StacksNetwork,
	g *api.StacksGenesis,
	participants []api.StacksNetworkParticipant,
	now time.Time,
) *api.BurnHeightSample {
	var sample *api.BurnHeightSample
	for i := range participants {
		p := &participants[i]
		if p.Spec.Kind != api.ParticipantStacksNode || !selectedInstance(root, p) || !currentActorReady(p) ||
			p.Status.Admission.Configuration.StacksNode == nil || actorSuspended(root, p) {
			continue
		}
		policy := p.Status.Admission.Configuration.StacksNode
		if policy.Mining == nil || !ptr.Deref(policy.Mining.Enabled, false) ||
			policy.Config != nil && ptr.Deref(policy.Config.Compatibility, "") == common.CompatibilityUnverified {
			continue
		}
		state := p.Status.Runtime
		o := state.Protocol
		if o == nil || !o.Available || !fresh(o.ObservedAt, now) || o.GenesisUID != g.UID ||
			o.NetworkID != 0x80000000 || o.PodUID != state.PodRef.UID ||
			o.ContainerID != state.ContainerID || o.ConfigurationDigest != state.ConfigurationDigest {
			continue
		}
		sample = greatestSample(sample, burnHeightSample(p, o.BurnHeight, o.ObservedAt))
	}
	return sample
}
