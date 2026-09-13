package v1alpha2

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// InitializationStatus records monotonic progress through the frozen bootstrap gates.
// Only the network controller writes these observations and generation limits.
type InitializationStatus struct {
	// GenesisUID pins the immutable requirements used for every gate.
	// +kubebuilder:validation:Type=string
	// +kubebuilder:validation:MaxLength=128
	GenesisUID types.UID `json:"genesisUID"`
	// GenesisDigest identifies the frozen chain profile.
	// +kubebuilder:validation:MaxLength=71
	// +kubebuilder:validation:Pattern=`^sha256:[0-9a-f]{64}$`
	GenesisDigest string `json:"genesisDigest"`
	// GateIndex identifies the currently active gate, or the gate count when complete.
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=16
	GateIndex int32 `json:"gateIndex"`
	// AuthorizedCeiling bounds managed Bitcoin generation before completion.
	// +kubebuilder:validation:Minimum=0
	AuthorizedCeiling int64 `json:"authorizedCeiling"`
	// Completed records that every immutable gate has been observed satisfied.
	Completed bool `json:"completed"`
	// Gates retains first ceiling observations and successful completion times.
	// +kubebuilder:validation:MaxItems=16
	// +listType=map
	// +listMapKey=name
	Gates []GateObservation `json:"gates"`
}

// GateObservation retains evidence timing independently of pause or later policy changes.
type GateObservation struct {
	// Name identifies one gate in the immutable genesis bootstrap requirements.
	// +kubebuilder:validation:MaxLength=64
	Name GateName `json:"name"`
	// FirstCeilingObservedAt starts the bounded observation window once, without pause extension.
	FirstCeilingObservedAt *metav1.Time `json:"firstCeilingObservedAt,omitempty"`
	// CompletedAt is the original successful gate observation time.
	CompletedAt *metav1.Time `json:"completedAt,omitempty"`
}
