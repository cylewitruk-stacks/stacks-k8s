package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// BitcoinReorganization replaces one bounded local regtest suffix with acknowledged cleanup.
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Blocks",type=integer,JSONPath=`.status.blocksGenerated`
type BitcoinReorganization struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	// Spec is immutable from creation.
	Spec BitcoinReorganizationSpec `json:"spec"`
	// Status records local mechanism and compensation evidence.
	Status BitcoinReorganizationStatus `json:"status,omitempty"`
}

// BitcoinReorganizationList contains local suffix replacement requests.
// +kubebuilder:object:root=true
type BitcoinReorganizationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	// Items are the requests in this list.
	Items []BitcoinReorganization `json:"items"`
}

// BitcoinReorganizationSpec defines one finite irreversible operation with compensation.
// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="action spec is immutable"
type BitcoinReorganizationSpec struct {
	// NetworkRef names the owning aggregate.
	NetworkRef LocalReference `json:"networkRef"`
	// BitcoinNodeRef names its exact compiled baseline target.
	BitcoinNodeRef LocalReference `json:"bitcoinNodeRef"`
	// Depth is the suffix length; exactly depth+1 replacement blocks are requested.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=6
	Depth int32 `json:"depth"`
	// Address receives replacement coinbase outputs.
	// +kubebuilder:validation:MinLength=14
	// +kubebuilder:validation:MaxLength=128
	Address string `json:"address"`
	// BoundaryPolicy explicitly accepts unknown protocol schedule boundaries.
	BoundaryPolicy ReorganizationBoundaryPolicy `json:"boundaryPolicy"`
	// Timeout bounds new invalidation and generation from object creation.
	// +kubebuilder:validation:Type=string
	// +kubebuilder:validation:MaxLength=32
	// +kubebuilder:validation:XValidation:rule="duration(self) > duration('0s')",message="timeout must be positive"
	// +kubebuilder:validation:XValidation:rule="duration(self) <= duration('10m')",message="timeout cannot exceed 10m"
	Timeout metav1.Duration `json:"timeout"`
}

// ReorganizationBoundaryPolicy requires explicit opt-ins without claiming schedule knowledge.
type ReorganizationBoundaryPolicy struct {
	// AllowEpochBoundaryCrossing permits an epoch boundary in the affected range.
	// +kubebuilder:validation:XValidation:rule="self == true",message="unknown schedule requires explicit epoch opt-in"
	AllowEpochBoundaryCrossing bool `json:"allowEpochBoundaryCrossing"`
	// AllowRewardCycleBoundaryCrossing permits a reward-cycle boundary.
	// +kubebuilder:validation:XValidation:rule="self == true",message="unknown schedule requires explicit reward-cycle opt-in"
	AllowRewardCycleBoundaryCrossing bool `json:"allowRewardCycleBoundaryCrossing"`
	// AllowPreparePhaseBoundaryCrossing permits a PoX prepare-phase boundary.
	// +kubebuilder:validation:XValidation:rule="self == true",message="unknown schedule requires explicit prepare-phase opt-in"
	AllowPreparePhaseBoundaryCrossing bool `json:"allowPreparePhaseBoundaryCrossing"`
}

// BitcoinChainPoint records a bounded verified block header identity.
type BitcoinChainPoint struct {
	// Hash identifies the block.
	Hash string `json:"hash"`
	// Height records its chain position.
	Height int64 `json:"height"`
	// PreviousBlockHash identifies its parent; genesis has none.
	PreviousBlockHash string `json:"previousBlockHash,omitempty"`
	// Chainwork is the fixed-width hexadecimal cumulative work.
	Chainwork string `json:"chainwork"`
}

// BitcoinReorganizationStatus adds bounded branch and cleanup facts to the common action lifecycle.
type BitcoinReorganizationStatus struct {
	BitcoinBlockGenerationStatus `json:",inline"`
	// OriginalChain records the admitted original tip.
	OriginalChain *BitcoinChainPoint `json:"originalChain,omitempty"`
	// ForkParent records the retained ancestor.
	ForkParent *BitcoinChainPoint `json:"forkParent,omitempty"`
	// InvalidatedHash records the first removed block.
	InvalidatedHash string `json:"invalidatedHash,omitempty"`
	// ReplacementBlockHashes are ordered acknowledged generation receipts.
	// +kubebuilder:validation:MaxItems=7
	ReplacementBlockHashes []string `json:"replacementBlockHashes,omitempty"`
	// FinalChain records the final local canonical observation, when verified.
	FinalChain *BitcoinChainPoint `json:"finalChain,omitempty"`
	// InvalidationAcknowledged records a matching invalidateblock receipt.
	InvalidationAcknowledged bool `json:"invalidationAcknowledged,omitempty"`
	// CleanupAcknowledged records a matching reconsiderblock receipt.
	CleanupAcknowledged bool `json:"cleanupAcknowledged,omitempty"`
}
