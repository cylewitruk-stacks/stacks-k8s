package v1alpha2

import common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"

// BitcoinControlRuntimeStatus records control workload identity and process termination.
type BitcoinControlRuntimeStatus struct {
	// ObservedGeneration identifies the evaluated participant generation.
	ObservedGeneration int64 `json:"observedGeneration"`
	// NetworkGeneration identifies the evaluated root control generation.
	NetworkGeneration int64 `json:"networkGeneration"`
	// DeploymentRef pins the actual control workload incarnation.
	DeploymentRef *common.Binding `json:"deploymentRef,omitempty"`
	// Pods retains unresolved process identities and terminal evidence until disposal.
	// +kubebuilder:validation:MaxItems=128
	// +listType=map
	// +listMapKey=uid
	Pods []BitcoinControlPodStatus `json:"pods,omitempty"`
	// Terminated requires confirmed exit of all processes and no remaining replica intent.
	Terminated bool `json:"terminated"`
	// Reason distinguishes observed shutdown from unavailable termination evidence.
	Reason string `json:"reason"`
}

// BitcoinControlPodStatus binds termination evidence to one exact Pod and ReplicaSet.
type BitcoinControlPodStatus struct {
	// UID is the immutable Pod identity and list key.
	UID string `json:"uid"`
	// Pod names the exact observed process host.
	Pod common.Binding `json:"pod"`
	// ReplicaSet identifies the verified Deployment-owned replica controller.
	ReplicaSet common.Binding `json:"replicaSet"`
	// Deployment preserves the verified owner epoch across control workload replacement.
	Deployment common.Binding `json:"deployment"`
	// Terminated records confirmed exit, never inferred from object disappearance.
	Terminated bool `json:"terminated"`
}
