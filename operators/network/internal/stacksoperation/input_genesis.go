package stacksoperation

import (
	"context"
	"errors"

	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/foundation"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/stacksworker"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// inputGenesis verifies current instance membership and the immutable public chain artifact.
func (r PublicInputs) inputGenesis(
	ctx context.Context,
	s stacksworker.Snapshot,
	kind api.ParticipantKind,
) (*api.StacksGenesis, error) {
	root, p := s.Network, s.Participant
	if r.Reader == nil || root == nil || p == nil || root.UID == "" || p.UID == "" || p.Status.Admission == nil ||
		p.Spec.Kind != kind ||
		p.Namespace != root.Namespace ||
		p.Spec.NetworkUID != root.UID ||
		p.DeletionTimestamp != nil ||
		!metav1.IsControlledBy(p, root) {
		return nil, errors.New("current worker admission unavailable")
	}
	selected, allocated := false, false
	for _, entry := range root.Spec.Participants {
		selected = selected || entry.Name == p.Spec.ParticipantName && entry.Kind == kind
	}
	for _, id := range root.Status.Identities {
		allocated = allocated || id.Name == p.Spec.ParticipantName && id.UID == p.UID && !id.Removing
	}
	if !selected || !allocated || root.Status.GenesisRef == nil {
		return nil, errors.New("current worker membership or genesis unavailable")
	}
	ref := root.Status.GenesisRef
	var genesis api.StacksGenesis
	if err := r.Reader.Get(ctx, client.ObjectKey{Namespace: root.Namespace, Name: ref.Name}, &genesis); err != nil {
		return nil, err
	}
	if genesis.UID != ref.UID || genesis.Spec.Source.NetworkUID != root.UID || genesis.DeletionTimestamp != nil ||
		!metav1.IsControlledBy(&genesis, root) ||
		foundation.Digest(genesis.Spec.Chain) != root.Status.GenesisDigest ||
		foundation.Digest(genesis.Spec) != ref.Fingerprint {
		return nil, errors.New("frozen genesis identity unavailable")
	}
	return &genesis, nil
}

// initialRequirement distinguishes original participants from independently admitted later instances.
func initialRequirement(
	genesis *api.StacksGenesis,
	p *api.StacksNetworkParticipant,
) (*api.BootstrapRequirement, error) {
	for i := range genesis.Spec.Bootstrap.Requirements {
		required := &genesis.Spec.Bootstrap.Requirements[i]
		if required.Participant.Name == p.Name && required.Participant.UID != p.UID {
			return nil, errors.New("captured participant name cannot bind a replacement")
		}
		if required.Participant.UID != p.UID {
			continue
		}
		if required.Kind != p.Spec.Kind || required.Participant.Name != p.Name {
			return nil, errors.New("captured participant binding differs")
		}
		return required, nil
	}
	return nil, nil
}
