package v1alpha2

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// StacksProtocolObservation contains coherent public native reads from one exact node process.
type StacksProtocolObservation struct {
	// Available requires a complete successful canonical observation; failed reads withdraw it.
	Available bool `json:"available"`
	// Reason is a bounded public observation classification.
	// +kubebuilder:validation:MaxLength=64
	Reason string `json:"reason"`
	// PodUID pins the exact process workload.
	// +kubebuilder:validation:Type=string
	// +kubebuilder:validation:MaxLength=128
	PodUID types.UID `json:"podUID"`
	// ContainerID pins the observed native node process.
	// +kubebuilder:validation:MaxLength=256
	ContainerID string `json:"containerID"`
	// ConfigurationDigest binds the complete public actor configuration inputs.
	// +kubebuilder:validation:MaxLength=71
	ConfigurationDigest string `json:"configurationDigest"`
	// GenesisUID pins the immutable network bootstrap requirements.
	// +kubebuilder:validation:Type=string
	// +kubebuilder:validation:MaxLength=128
	GenesisUID types.UID `json:"genesisUID"`
	// ObservedAt is the successful observation time, never refreshed by failed RPCs.
	ObservedAt metav1.Time `json:"observedAt"`
	// HighestStacksHeight retains the greatest successfully observed height for this participant.
	HighestStacksHeight uint64 `json:"highestStacksHeight"`
	// LastHeightAdvancedAt changes only when a strictly greater Stacks height is observed.
	LastHeightAdvancedAt metav1.Time `json:"lastHeightAdvancedAt"`
	// NetworkID preserves the full native uint32 chain identifier.
	// +kubebuilder:validation:Maximum=4294967295
	NetworkID uint64 `json:"networkID"`
	// BurnHeight is the native node's processed Bitcoin height.
	BurnHeight uint64 `json:"burnHeight"`
	// StacksHeight is the current canonical Stacks height.
	StacksHeight uint64 `json:"stacksHeight"`
	// StacksTip identifies the canonical Stacks header.
	// +kubebuilder:validation:MaxLength=64
	StacksTip string `json:"stacksTip"`
	// IndexBlockID pins all prepared-set reads to the native canonical tip.
	// +kubebuilder:validation:MaxLength=64
	IndexBlockID string `json:"indexBlockID"`
	// BurnConsensusHash identifies the native processed Bitcoin fork.
	// +kubebuilder:validation:MaxLength=40
	BurnConsensusHash string `json:"burnConsensusHash"`
	// FullySynced is the node's own synchronization report.
	FullySynced bool `json:"fullySynced"`
	// PoXContract is the active boot contract principal.
	// +kubebuilder:validation:MaxLength=128
	PoXContract string `json:"poxContract"`
	// PoXBurnHeight is the native PoX timing view's processed height.
	PoXBurnHeight uint64 `json:"poxBurnHeight"`
	// RewardCycle is the current native reward cycle.
	RewardCycle uint64 `json:"rewardCycle"`
	// CycleLength is the native Bitcoin-block reward period.
	CycleLength uint64 `json:"cycleLength"`
	// PreparedSet reports the frozen active prepared-set gate, when one is required.
	PreparedSet *PreparedSignerSetObservation `json:"preparedSet,omitempty"`
}

// PreparedSignerSetObservation retains exact native protocol weights for a frozen cycle.
type PreparedSignerSetObservation struct {
	// Cycle identifies the explicit frozen target cycle.
	Cycle uint64 `json:"cycle"`
	// Available is false for Core's explicit not-available-yet response.
	Available bool `json:"available"`
	// Version is the native reward-set encoding.
	// +kubebuilder:validation:Maximum=1
	Version uint32 `json:"version"`
	// Threshold is the protocol's uint128 micro-STX threshold, encoded losslessly.
	// +kubebuilder:validation:MaxLength=39
	Threshold string `json:"threshold,omitempty"`
	// Signers contains exact native membership and weights.
	// +kubebuilder:validation:MaxItems=1000
	// +listType=atomic
	Signers []PreparedSignerObservation `json:"signers,omitempty"`
	// ObservedAt is the original successful prepared-set observation time.
	ObservedAt metav1.Time `json:"observedAt"`
}

// PreparedSignerObservation identifies one native consensus signer entry.
type PreparedSignerObservation struct {
	// PublicKey identifies the compressed consensus key.
	// +kubebuilder:validation:MaxLength=66
	// +kubebuilder:validation:Pattern=`^(02|03)[0-9a-f]{64}$`
	PublicKey string `json:"publicKey"`
	// Weight is reported by Core and must not be derived from specification amounts.
	// +kubebuilder:validation:Maximum=4294967295
	// +kubebuilder:validation:Minimum=1
	Weight uint64 `json:"weight"`
	// StackedAmount is the native uint128 micro-STX amount.
	// +kubebuilder:validation:MaxLength=39
	StackedAmount string `json:"stackedAmount"`
}
