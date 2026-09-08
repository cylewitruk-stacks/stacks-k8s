// Package ports defines the network operator's actor Service interfaces.
package ports

import networkv1alpha1 "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha1"

// Bitcoin returns the configured Bitcoin RPC and P2P ports.
func Bitcoin(rpc, p2p int32) []networkv1alpha1.PortSpec {
	return []networkv1alpha1.PortSpec{{Name: "rpc", Port: rpc}, {Name: "p2p", Port: p2p}}
}

// StacksNode returns the fixed Stacks node ports.
func StacksNode() []networkv1alpha1.PortSpec {
	return []networkv1alpha1.PortSpec{{Name: "rpc", Port: 20443}, {Name: "p2p", Port: 20444}, {Name: "metrics", Port: 20446}}
}

// StacksSigner returns the fixed Stacks signer ports.
func StacksSigner() []networkv1alpha1.PortSpec {
	return []networkv1alpha1.PortSpec{{Name: "events", Port: 30000}, {Name: "metrics", Port: 31000}}
}
