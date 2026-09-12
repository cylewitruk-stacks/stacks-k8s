package v1alpha2

import common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"

// BitcoinRuntimeStatus retains record identities independently of production membership.
type BitcoinRuntimeStatus struct {
	// ExecutionRefs pins each allocated per-node journal until network deletion.
	// +kubebuilder:validation:MaxItems=1000
	// +listType=map
	// +listMapKey=name
	ExecutionRefs []common.Binding `json:"executionRefs,omitempty"`
	// InitializationRef pins the unique frozen Bitcoin preparation and scheduling state.
	InitializationRef *common.Binding `json:"initializationRef,omitempty"`
}
