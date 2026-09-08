// Package labels defines stable ownership and selection labels.
package labels

import "fmt"

const (
	// ManagedByKey identifies resources managed by this operator.
	ManagedByKey = "app.kubernetes.io/managed-by"
	// ManagedByValue identifies the standalone network operator.
	ManagedByValue = "stacks-network-operator"
	// NetworkKey identifies the aggregate network.
	NetworkKey = "network.stacks.org/network"
	// ActorKey identifies the logical actor.
	ActorKey = "network.stacks.org/actor"
	// ActorKindKey identifies the leaf actor kind.
	ActorKindKey = "network.stacks.org/actor-kind"
	// ActorResourceKey identifies the leaf custom resource by name.
	ActorResourceKey = "network.stacks.org/actor-resource"
)

// ActorKind is the canonical label value used by one actor controller.
type ActorKind string

const (
	// Bitcoin identifies BitcoinNode workloads.
	Bitcoin ActorKind = "bitcoin"
	// StacksNode identifies StacksNode workloads.
	StacksNode ActorKind = "stacks-node"
	// StacksSigner identifies StacksSigner workloads.
	StacksSigner ActorKind = "stacks-signer"
)

// FromResourceKind converts a Kubernetes leaf kind into its canonical label value.
func FromResourceKind(kind string) (ActorKind, error) {
	switch kind {
	case "BitcoinNode":
		return Bitcoin, nil
	case "StacksNode":
		return StacksNode, nil
	case "StacksSigner":
		return StacksSigner, nil
	default:
		return "", fmt.Errorf("unsupported actor resource kind %q", kind)
	}
}

// ForActor returns the labels shared by a leaf resource and its workloads.
func ForActor(network, actor string, kind ActorKind) map[string]string {
	return map[string]string{
		ManagedByKey: ManagedByValue,
		NetworkKey:   network,
		ActorKey:     actor,
		ActorKindKey: string(kind),
	}
}
