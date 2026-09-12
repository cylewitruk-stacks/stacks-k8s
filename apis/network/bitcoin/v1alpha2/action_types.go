package v1alpha2

import (
	action "github.com/cylewitruk-stacks/stacks-k8s/apis/network/actions/v1alpha2"
	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// BitcoinActionReservation retains one finite action through receipt and lifecycle acknowledgement.
// +kubebuilder:validation:XValidation:rule="has(self.generation) != has(self.reorganization)",message="exactly one finite action spec is required"
type BitcoinActionReservation struct {
	// Request pins the action kind, name and UID.
	Request common.Binding `json:"request"`
	// Generation snapshots one immutable finite generation request.
	Generation *action.BitcoinBlockGenerationSpec `json:"generation,omitempty"`
	// Reorganization snapshots one immutable local suffix replacement request.
	Reorganization *action.BitcoinReorganizationSpec `json:"reorganization,omitempty"`
	// Network captures the admitted environment revision.
	Network action.NetworkIdentity `json:"network"`
	// Target exposes exact actor/workload admission identity to the action lifecycle.
	Target action.TargetIdentity `json:"target"`
	// Runtime retains exact transport and process identity for every arm and cleanup.
	Runtime BitcoinTargetIdentity `json:"runtime"`
	// AdmittedAt is the original successful reservation time.
	AdmittedAt metav1.Time `json:"admittedAt"`
	// ExpiresAt bounds new generation and invalidation from API creation.
	ExpiresAt metav1.Time `json:"expiresAt"`
	// StartedAt records the first potentially executable action arm.
	StartedAt *metav1.Time `json:"startedAt,omitempty"`
	// NextDispatchAt retains receipt-relative timing without redraw or catch-up.
	NextDispatchAt *metav1.Time `json:"nextDispatchAt,omitempty"`
	// LastCompletedAt retains the latest attributable receipt time.
	LastCompletedAt *metav1.Time `json:"lastCompletedAt,omitempty"`
	// CorrelationID snapshots the bounded public correlation label.
	// +kubebuilder:validation:MaxLength=63
	CorrelationID string `json:"correlationID,omitempty"`
	// BlocksGenerated counts exactly attributed single-block responses.
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=100
	BlocksGenerated int32 `json:"blocksGenerated,omitempty"`
	// LastBlockHash identifies the latest acknowledged block, without asserting canonicality.
	LastBlockHash string `json:"lastBlockHash,omitempty"`
	// LastDispatchID binds the receipt that the lifecycle controller must acknowledge.
	LastDispatchID string `json:"lastDispatchID,omitempty"`
	// StopReason withdraws new generation/invalidation with the same execution CAS.
	// +kubebuilder:validation:MaxLength=128
	StopReason string `json:"stopReason,omitempty"`
	// EffectUncertain retains lost transport authority even if later cleanup facts improve.
	EffectUncertain bool `json:"effectUncertain,omitempty"`
	// CleanupUnsafe prevents compensation on changed identity or outside its fixed horizon.
	CleanupUnsafe bool `json:"cleanupUnsafe,omitempty"`
	// OriginalChain captures the immutable original tip for a reorganization.
	OriginalChain *action.BitcoinChainPoint `json:"originalChain,omitempty"`
	// ForkParent captures the retained ancestor.
	ForkParent *action.BitcoinChainPoint `json:"forkParent,omitempty"`
	// AcceptedChain is the last fully validated replacement header.
	AcceptedChain *action.BitcoinChainPoint `json:"acceptedChain,omitempty"`
	// InvalidatedHash is the original first removed block and sole allowed marker target.
	InvalidatedHash string `json:"invalidatedHash,omitempty"`
	// InvalidationAcknowledged records the matching null receipt.
	InvalidationAcknowledged bool `json:"invalidationAcknowledged,omitempty"`
	// CleanupAcknowledged records matching reconsideration, independently of action outcome.
	CleanupAcknowledged bool `json:"cleanupAcknowledged,omitempty"`
	// ReplacementBlockHashes retains the bounded ordered generation receipts.
	// +kubebuilder:validation:MaxItems=7
	// +listType=atomic
	ReplacementBlockHashes []string `json:"replacementBlockHashes,omitempty"`
	// FinalChain records verified higher-work canonical replacement after cleanup.
	FinalChain *action.BitcoinChainPoint `json:"finalChain,omitempty"`
}
