package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// StacksSigner is a compiled Stacks signer workload.
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:validation:XValidation:rule="self.metadata.name.size() <= 63 && self.metadata.name.matches('^[a-z0-9]([-a-z0-9]*[a-z0-9])?$')",message="metadata.name must be a DNS label no longer than 63 characters"
// +kubebuilder:printcolumn:name="Actor",type=string,JSONPath=`.spec.actorName`
// +kubebuilder:printcolumn:name="Node",type=string,JSONPath=`.spec.nodeRef.name`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
type StacksSigner struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              StacksSignerSpec `json:"spec"`
	Status            ActorStatus      `json:"status,omitempty"`
}

// StacksSignerList contains StacksSigner resources.
// +kubebuilder:object:root=true
type StacksSignerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []StacksSigner `json:"items"`
}

// StacksSignerSpec contains the fully resolved signer declaration.
// +kubebuilder:validation:XValidation:rule="has(self.config.secretRef)",message="signer configuration must use a Secret reference"
type StacksSignerSpec struct {
	NetworkRef LocalObjectReference `json:"networkRef"`
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern=`^[a-z0-9](?:[-a-z0-9]{0,61}[a-z0-9])?$`
	ActorName        string                 `json:"actorName"`
	Image            string                 `json:"image"`
	ImagePullPolicy  corev1.PullPolicy      `json:"imagePullPolicy,omitempty"`
	ImagePullSecrets []LocalObjectReference `json:"imagePullSecrets,omitempty"`
	NodeRef          LocalObjectReference   `json:"nodeRef"`
	// +kubebuilder:validation:Minimum=0
	Index int32 `json:"index"`
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=9007199254740991
	Weight    int64        `json:"weight"`
	PublicKey string       `json:"publicKey,omitempty"`
	Config    ConfigSource `json:"config"`
	// ServiceMap resolves only the actor's declared and direct dependencies.
	// +kubebuilder:validation:MaxProperties=232
	ServiceMap      map[string]string  `json:"serviceMap,omitempty"`
	DependencyImage string             `json:"dependencyImage,omitempty"`
	Workload        WorkloadSpec       `json:"workload,omitempty"`
	Container       *ContainerOverride `json:"container,omitempty"`
	Suspended       bool               `json:"suspended,omitempty"`
}
