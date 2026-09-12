package v1alpha2

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// TrafficObservation rechecks a past inclusion without refreshing its original progress time.
type TrafficObservation struct {
	// Available distinguishes a completed canonical read from unavailable native RPC.
	Available bool `json:"available"`
	// Found reports whether the exact transaction remains canonically included.
	Found bool `json:"found"`
	// Success reports successful native execution when Found is true.
	Success bool `json:"success"`
	// TxID identifies the last transaction whose inclusion is being rechecked.
	// +kubebuilder:validation:MaxLength=64
	TxID string `json:"txID"`
	// BlockID identifies its current canonical inclusion block when found.
	// +kubebuilder:validation:MaxLength=64
	BlockID string `json:"blockID,omitempty"`
	// IndexBlockID binds the coherent native canonical observation.
	// +kubebuilder:validation:MaxLength=64
	IndexBlockID string `json:"indexBlockID,omitempty"`
	// SubmittedBurnHeight is the native height observed before the original send.
	SubmittedBurnHeight uint64 `json:"submittedBurnHeight"`
	// BurnHeight is the native height at that observation.
	BurnHeight uint64 `json:"burnHeight"`
	// ObservedAt retains the original successful recheck time across RPC failures.
	ObservedAt metav1.Time `json:"observedAt"`
	// TargetParticipantUID pins the ingress participant used for the original transfer.
	// +kubebuilder:validation:Type=string
	// +kubebuilder:validation:MaxLength=128
	TargetParticipantUID types.UID `json:"targetParticipantUID"`
	// TargetPodUID pins its original actor incarnation.
	// +kubebuilder:validation:Type=string
	// +kubebuilder:validation:MaxLength=128
	TargetPodUID types.UID `json:"targetPodUID"`
	// TargetContainerID pins the original actor process.
	// +kubebuilder:validation:MaxLength=256
	TargetContainerID string `json:"targetContainerID"`
	// TargetConfigurationDigest pins the original effective actor configuration.
	// +kubebuilder:validation:MaxLength=128
	TargetConfigurationDigest string `json:"targetConfigurationDigest"`
	// GenesisUID pins the frozen network chain identity.
	// +kubebuilder:validation:Type=string
	// +kubebuilder:validation:MaxLength=128
	GenesisUID types.UID `json:"genesisUID"`
	// EffectiveIntervalSeconds is the policy actually adopted by the surviving role.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=3600
	EffectiveIntervalSeconds uint64 `json:"effectiveIntervalSeconds"`
	// ProgressWindowSeconds is max(progress grace, three applied intervals) plus RPC allowance.
	// +kubebuilder:validation:Maximum=10810
	ProgressWindowSeconds uint64 `json:"progressWindowSeconds"`
}
