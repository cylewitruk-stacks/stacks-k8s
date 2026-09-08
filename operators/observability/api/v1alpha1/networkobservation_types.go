package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// ObservationPhase describes one observation attempt.
// +kubebuilder:validation:Enum=Pending;Ready;Inconclusive
type ObservationPhase string

const (
	// ObservationPending waits for a complete admitted topology.
	ObservationPending ObservationPhase = "Pending"
	// ObservationReady contains a fully verified identity snapshot.
	ObservationReady ObservationPhase = "Ready"
	// ObservationInconclusive records identity drift or a binding mismatch.
	ObservationInconclusive ObservationPhase = "Inconclusive"
)

// LocalObjectReference names an object in the observation namespace.
type LocalObjectReference struct {
	// Name is the referenced resource name.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
	Name string `json:"name"`
}

// NetworkObservationSpec requests one identity-bound observation.
type NetworkObservationSpec struct {
	// NetworkRef selects a StacksNetwork in this namespace.
	NetworkRef LocalObjectReference `json:"networkRef"`
	// ExpectedInventoryDigest fails closed when the admitted inventory differs.
	// +optional
	// +kubebuilder:validation:Pattern=`^sha256:[0-9a-f]{64}$`
	ExpectedInventoryDigest string `json:"expectedInventoryDigest,omitempty"`
	// TimeoutSeconds bounds the complete direct-read observation.
	// +optional
	// +kubebuilder:default=10
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=30
	TimeoutSeconds *int32 `json:"timeoutSeconds,omitempty"`
	// PendingTimeoutSeconds bounds how long the observation waits for a ready topology.
	// +optional
	// +kubebuilder:default=300
	// +kubebuilder:validation:Minimum=10
	// +kubebuilder:validation:Maximum=86400
	PendingTimeoutSeconds *int32 `json:"pendingTimeoutSeconds,omitempty"`
}

// NetworkBinding identifies the exact admitted topology observed.
type NetworkBinding struct {
	// Name is the topology resource name.
	Name string `json:"name"`
	// UID prevents same-name topology replacement from being hidden.
	UID types.UID `json:"uid"`
	// ObservedGeneration is the reconciled topology generation.
	ObservedGeneration int64 `json:"observedGeneration"`
	// InventoryDigest binds the admitted actor set.
	// +kubebuilder:validation:Pattern=`^sha256:[0-9a-f]{64}$`
	InventoryDigest string `json:"inventoryDigest"`
}

// ObservedActorIdentity contains orchestrator-observed runtime facts.
type ObservedActorIdentity struct {
	// Kind is the admitted leaf resource kind.
	Kind string `json:"kind"`
	// Name is the logical actor name.
	Name string `json:"name"`
	// Role is the Stacks node or signer role; Bitcoin nodes have none.
	Role string `json:"role,omitempty"`
	// ResourceName is the admitted leaf resource name.
	ResourceName string `json:"resourceName"`
	// ServiceName is the actor's stable Service identity.
	ServiceName string `json:"serviceName"`
	// StatefulSetName is the actor workload name.
	StatefulSetName string `json:"statefulSetName"`
	// StatefulSetUID prevents same-name workload replacement from being hidden.
	StatefulSetUID types.UID `json:"statefulSetUID"`
	// ControllerRevision is the converged StatefulSet revision.
	ControllerRevision string `json:"controllerRevision"`
	// PodName is the admitted actor Pod name.
	PodName string `json:"podName"`
	// PodUID prevents same-name Pod replacement from being hidden.
	PodUID types.UID `json:"podUID"`
	// RequestedImage is the declared image reference.
	RequestedImage string `json:"requestedImage"`
	// RuntimeImageID is the immutable image digest reported by Kubernetes.
	RuntimeImageID string `json:"runtimeImageID"`
	// ConfigDigest binds the admitted configuration declaration or bytes.
	ConfigDigest string `json:"configDigest"`
	// SpecDigest binds the complete admitted leaf specification.
	SpecDigest string `json:"specDigest"`
	// EvidenceClass distinguishes Kubernetes-observed facts from actor reports.
	// +kubebuilder:validation:Enum=orchestrator-observed
	EvidenceClass string `json:"evidenceClass"`
}

// NetworkObservationStatus records one terminal or pending observation.
type NetworkObservationStatus struct {
	// ObservedGeneration is the observation generation evaluated.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// Phase is Pending, Ready, or Inconclusive.
	Phase ObservationPhase `json:"phase,omitempty"`
	// Binding identifies the stable topology when Ready.
	Binding *NetworkBinding `json:"binding,omitempty"`
	// Actors contains directly verified identities when Ready.
	// +optional
	// +listType=map
	// +listMapKey=kind
	// +listMapKey=name
	// +kubebuilder:validation:MaxItems=232
	Actors []ObservedActorIdentity `json:"actors,omitempty"`
	// StartedAt records when this generation first attempted observation.
	StartedAt *metav1.Time `json:"startedAt,omitempty"`
	// CompletedAt records terminal completion.
	CompletedAt *metav1.Time `json:"completedAt,omitempty"`
	// Conditions provide Kubernetes-standard diagnostics.
	// +optional
	// +listType=map
	// +listMapKey=type
	// +kubebuilder:validation:MaxItems=4
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=nobs
// +kubebuilder:printcolumn:name="Network",type=string,JSONPath=`.spec.networkRef.name`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`

// NetworkObservation requests one compact trusted identity observation.
type NetworkObservation struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              NetworkObservationSpec   `json:"spec,omitempty"`
	Status            NetworkObservationStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// NetworkObservationList contains observation resources.
type NetworkObservationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []NetworkObservation `json:"items"`
}

func init() {
	SchemeBuilder.Register(&NetworkObservation{}, &NetworkObservationList{})
}
