package v1alpha2

import (
	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// BurnchainObservations projects independent greatest available heights with exact provenance.
// Samples need not describe the same fork or connected peers; absence means unavailable.
// Neither their difference nor their presence determines Operational.
type BurnchainObservations struct {
	// Bitcoin is the greatest fresh height among selected ready Bitcoin actors.
	Bitcoin *BurnHeightSample `json:"bitcoin,omitempty"`
	// Miner is the greatest fresh processed burn height among selected verified unsuspended miners.
	Miner *BurnHeightSample `json:"miner,omitempty"`
}

// BurnHeightSample binds one source observation to the actor process that produced it.
type BurnHeightSample struct {
	// Participant identifies the admitted instance.
	Participant common.Binding `json:"participant"`
	// Pod identifies the observed workload.
	Pod common.Binding `json:"pod"`
	// ContainerID identifies the exact actor process.
	// +kubebuilder:validation:MaxLength=256
	ContainerID string `json:"containerID"`
	// ConfigurationDigest identifies the public runtime configuration.
	// +kubebuilder:validation:MaxLength=71
	ConfigurationDigest string `json:"configurationDigest"`
	// Height is the observed Bitcoin or processed burn height.
	Height uint64 `json:"height"`
	// FirstObservedAt is the first source observation in the currently retained sequence
	// of equal height and identity. It is not a freshness timestamp or proof of continuous observation.
	FirstObservedAt metav1.Time `json:"firstObservedAt"`
}
