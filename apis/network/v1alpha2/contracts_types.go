package v1alpha2

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// ContractSetObservation records exact pinned sources and explicit native registry state at one tip.
type ContractSetObservation struct {
	// Deployer identifies the immutable deployment principal.
	// +kubebuilder:validation:MaxLength=64
	Deployer string `json:"deployer"`
	// Bundle identifies the release-pinned source manifest.
	// +kubebuilder:validation:Enum=sbtc-regtest-v1
	Bundle string `json:"bundle"`
	// SourceDigest identifies the complete observed name-to-source-hash map.
	// +kubebuilder:validation:Pattern=`^sha256:[0-9a-f]{64}$`
	SourceDigest string `json:"sourceDigest"`
	// SignerPublicKeys preserves the exact declared registry key order.
	// +kubebuilder:validation:MinItems=2
	// +kubebuilder:validation:MaxItems=100
	// +kubebuilder:validation:items:MaxLength=66
	// +kubebuilder:validation:items:Pattern=`^(02|03)[0-9a-f]{64}$`
	// +listType=atomic
	SignerPublicKeys []string `json:"signerPublicKeys"`
	// AggregatePublicKey is the explicit registry aggregate key.
	// +kubebuilder:validation:Pattern=`^(02|03)[0-9a-f]{64}$`
	AggregatePublicKey string `json:"aggregatePublicKey"`
	// Threshold records the exact observed registry quorum.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=100
	Threshold uint64 `json:"threshold"`
	// SignerPrincipal is the principal derived by the pinned bootstrap contract from the same key tuple.
	// +kubebuilder:validation:MaxLength=64
	SignerPrincipal string `json:"signerPrincipal"`
	// Complete requires all source hashes and every registry field to match.
	Complete bool `json:"complete"`
	// StacksTip is the canonical index block ID pinning source, registry and account reads.
	// +kubebuilder:validation:Pattern=`^[0-9a-f]{64}$`
	StacksTip string `json:"stacksTip"`
	// BurnHeight is the target's processed canonical Bitcoin height.
	// +kubebuilder:validation:Minimum=0
	BurnHeight uint64 `json:"burnHeight"`
	// ObservedAt preserves the successful observation time across status publication retries.
	ObservedAt metav1.Time `json:"observedAt"`
}
