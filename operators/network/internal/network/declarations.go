package network

import (
	"fmt"

	networkv1alpha1 "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha1"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/canonical"
)

// declarations binds the compiled actor set to the current parent identity.
func declarations(network *networkv1alpha1.StacksNetwork, desired DesiredTopology) (*networkv1alpha1.TargetDeclarations, error) {
	result := &networkv1alpha1.TargetDeclarations{
		SchemaVersion: networkv1alpha1.TargetDeclarationsVersion, NetworkUID: string(network.UID),
		ObservedGeneration: network.Generation, Actors: make([]networkv1alpha1.TargetDeclaration, 0, len(desired.Objects())),
	}
	for _, object := range desired.Objects() {
		entry := networkv1alpha1.TargetDeclaration{Kind: object.kind, Name: object.name}
		var spec any
		switch actor := object.value.(type) {
		case *networkv1alpha1.BitcoinNode:
			entry.ActorName, entry.Suspended, spec = actor.Spec.ActorName, actor.Spec.Suspended, actor.Spec
		case *networkv1alpha1.StacksNode:
			entry.ActorName, entry.Suspended, spec = actor.Spec.ActorName, actor.Spec.Suspended, actor.Spec
		case *networkv1alpha1.StacksSigner:
			entry.ActorName, entry.Suspended, spec = actor.Spec.ActorName, actor.Spec.Suspended, actor.Spec
		default:
			return nil, fmt.Errorf("unsupported declaration kind %q", object.kind)
		}
		digest, err := canonical.Digest(spec)
		if err != nil {
			return nil, fmt.Errorf("digest %s %s: %w", entry.Kind, entry.Name, err)
		}
		entry.SpecDigest = digest
		result.Actors = append(result.Actors, entry)
	}
	return result, nil
}
