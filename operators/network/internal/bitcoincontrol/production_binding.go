package bitcoincontrol

import (
	"context"
	"fmt"

	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/foundation"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// productionBinding selects current authority without changing captured bootstrap provenance.
func productionBinding(record *bitcoin.BitcoinInitialization) common.Binding {
	if record.Status.Production != nil {
		return *record.Status.Production
	}
	return record.Spec.Production
}

// currentProduction resolves the sole selected producer while preserving frozen initialization.
func (s *Scheduler) currentProduction(
	ctx context.Context,
	root *api.StacksNetwork,
	record *bitcoin.BitcoinInitialization,
) (*api.StacksNetworkParticipant, error) {
	var entry *api.Participant
	for i := range root.Spec.Participants {
		candidate := &root.Spec.Participants[i]
		if candidate.Kind == api.ParticipantBitcoinBlockProduction {
			if entry != nil {
				return nil, fmt.Errorf("multiple selected Bitcoin producers")
			}
			entry = candidate
		}
	}
	if entry == nil {
		return nil, fmt.Errorf("production participant removed")
	}
	var uid string
	for _, identity := range root.Status.Identities {
		if identity.Name == entry.Name && !identity.Removing {
			uid = string(identity.UID)
		}
	}
	if uid == "" {
		return nil, fmt.Errorf("producer allocation unavailable")
	}
	bound := productionBinding(record)
	name := foundation.ParticipantName(string(root.UID), entry.Name)
	if string(bound.UID) == uid {
		name = bound.Name
	} else if string(record.Spec.Production.UID) == uid {
		name = record.Spec.Production.Name
	}
	p := &api.StacksNetworkParticipant{}
	if e := s.Reader.Get(ctx, client.ObjectKey{Namespace: root.Namespace, Name: name}, p); e != nil {
		return nil, e
	}
	if string(p.UID) != uid || !participantCurrent(root, p) || p.Spec.Kind != api.ParticipantBitcoinBlockProduction ||
		p.Status.Admission.Configuration.BitcoinBlockProduction == nil {
		return nil, fmt.Errorf("producer identity unavailable")
	}
	if initial := p.Status.Admission.Configuration.BitcoinBlockProduction.Initialization; initial != nil {
		if initial.MinimumHeight != record.Spec.MinimumHeight ||
			initial.MatureOutputsPerMiner != record.Spec.MatureOutputsPerMiner {
			return nil, fmt.Errorf("replacement changes frozen initialization")
		}
		target := false
		completed := root.Status.Initialization != nil && root.Status.Initialization.Completed
		for _, identity := range root.Status.Identities {
			target = target ||
				identity.Name == initial.TargetNodeRef.Name && identity.UID == record.Spec.Target.UID &&
					(!identity.Removing || completed)
		}
		if !target {
			return nil, fmt.Errorf("replacement changes frozen initial target")
		}
	}
	return p, nil
}
