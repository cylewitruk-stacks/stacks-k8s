package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// StacksNodeRole describes a Stacks node process role.
type StacksNodeRole string

const (
	// StacksNodeMiner produces Stacks blocks.
	StacksNodeMiner StacksNodeRole = "miner"
	// StacksNodeFollower follows and relays the chain.
	StacksNodeFollower StacksNodeRole = "follower"
	// StacksNodeSigner serves one Stacks signer.
	StacksNodeSigner StacksNodeRole = "signer-node"
)

// StacksNode is a compiled Stacks node workload.
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:validation:XValidation:rule="self.metadata.name.size() <= 63 && self.metadata.name.matches('^[a-z0-9]([-a-z0-9]*[a-z0-9])?$')",message="metadata.name must be a DNS label no longer than 63 characters"
// +kubebuilder:printcolumn:name="Actor",type=string,JSONPath=`.spec.actorName`
// +kubebuilder:printcolumn:name="Role",type=string,JSONPath=`.spec.role`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
type StacksNode struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              StacksNodeSpec `json:"spec"`
	Status            ActorStatus    `json:"status,omitempty"`
}

// StacksNodeList contains StacksNode resources.
// +kubebuilder:object:root=true
type StacksNodeList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []StacksNode `json:"items"`
}

// StacksNodeSpec contains the fully resolved Stacks node declaration.
// +kubebuilder:validation:XValidation:rule="self.role != 'miner' || has(self.config.secretRef)",message="miner configuration must use a Secret reference"
type StacksNodeSpec struct {
	NetworkRef LocalObjectReference `json:"networkRef"`
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern=`^[a-z0-9](?:[-a-z0-9]{0,61}[a-z0-9])?$`
	ActorName string `json:"actorName"`
	// +kubebuilder:validation:Enum=miner;follower;signer-node
	Role             StacksNodeRole         `json:"role"`
	Image            string                 `json:"image"`
	ImagePullPolicy  corev1.PullPolicy      `json:"imagePullPolicy,omitempty"`
	ImagePullSecrets []LocalObjectReference `json:"imagePullSecrets,omitempty"`
	BitcoinNodeRef   LocalObjectReference   `json:"bitcoinNodeRef"`
	SignerRef        *LocalObjectReference  `json:"signerRef,omitempty"`
	SignerIndex      *int32                 `json:"signerIndex,omitempty"`
	Config           ConfigSource           `json:"config"`
	// ServiceMap resolves only the actor's declared and direct dependencies.
	// +kubebuilder:validation:MaxProperties=232
	ServiceMap      map[string]string  `json:"serviceMap,omitempty"`
	BootstrapPeers  []string           `json:"bootstrapPeers,omitempty"`
	Genesis         *GenesisSpec       `json:"genesis,omitempty"`
	DependencyImage string             `json:"dependencyImage,omitempty"`
	Workload        WorkloadSpec       `json:"workload,omitempty"`
	Container       *ContainerOverride `json:"container,omitempty"`
	Suspended       bool               `json:"suspended,omitempty"`
}
