package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// BitcoinNodeRole describes a Bitcoin Core node's topology role.
type BitcoinNodeRole string

const (
	// BitcoinNodeMiner is eligible for an external mining policy.
	BitcoinNodeMiner BitcoinNodeRole = "miner"
	// BitcoinNodeFollower follows the declared Bitcoin peer graph.
	BitcoinNodeFollower BitcoinNodeRole = "follower"
)

// BitcoinNode is a compiled Bitcoin Core actor workload.
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:validation:XValidation:rule="self.metadata.name.size() <= 63 && self.metadata.name.matches('^[a-z0-9]([-a-z0-9]*[a-z0-9])?$')",message="metadata.name must be a DNS label no longer than 63 characters"
// +kubebuilder:printcolumn:name="Actor",type=string,JSONPath=`.spec.actorName`
// +kubebuilder:printcolumn:name="Role",type=string,JSONPath=`.spec.role`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
type BitcoinNode struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              BitcoinNodeSpec `json:"spec"`
	Status            ActorStatus     `json:"status,omitempty"`
}

// BitcoinNodeList contains BitcoinNode resources.
// +kubebuilder:object:root=true
type BitcoinNodeList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []BitcoinNode `json:"items"`
}

// BitcoinNodeSpec contains the fully resolved Bitcoin actor declaration.
type BitcoinNodeSpec struct {
	NetworkRef LocalObjectReference `json:"networkRef"`
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern=`^[a-z0-9](?:[-a-z0-9]{0,61}[a-z0-9])?$`
	ActorName string `json:"actorName"`
	// +kubebuilder:validation:Enum=miner;follower
	Role             BitcoinNodeRole        `json:"role"`
	Image            string                 `json:"image"`
	ImagePullPolicy  corev1.PullPolicy      `json:"imagePullPolicy,omitempty"`
	ImagePullSecrets []LocalObjectReference `json:"imagePullSecrets,omitempty"`
	Config           ConfigSource           `json:"config"`
	// PeerRefs contain compiled BitcoinNode resource names.
	// +listType=set
	PeerRefs []string `json:"peerRefs,omitempty"`
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	RPCPort int32 `json:"rpcPort"`
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	P2PPort         int32              `json:"p2pPort"`
	DependencyImage string             `json:"dependencyImage,omitempty"`
	Workload        WorkloadSpec       `json:"workload,omitempty"`
	Container       *ContainerOverride `json:"container,omitempty"`
	Suspended       bool               `json:"suspended,omitempty"`
}
