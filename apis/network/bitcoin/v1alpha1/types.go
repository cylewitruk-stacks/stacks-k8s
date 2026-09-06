package v1alpha1

import (
	actionv1alpha1 "github.com/cylewitruk-stacks/stacks-k8s/apis/network/actions/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// GroupVersion identifies the Bitcoin capability API.
var GroupVersion = schema.GroupVersion{Group: "bitcoin.stacks.org", Version: "v1alpha1"}

// AddToScheme registers Bitcoin capability resources.
func AddToScheme(scheme *runtime.Scheme) error {
	scheme.AddKnownTypes(GroupVersion, &BitcoinBlockProduction{}, &BitcoinBlockProductionList{}, &BitcoinProductionTarget{}, &BitcoinProductionTargetList{})
	metav1.AddToGroupVersion(scheme, GroupVersion)
	return nil
}

// TargetPolicy identifies one immutable actor and its current destination.
type TargetPolicy struct {
	// Target names a Bitcoin actor in the owning network.
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern=`^[a-z0-9](?:[-a-z0-9]{0,61}[a-z0-9])?$`
	Target string `json:"target"`
	// IntervalSeconds mirrors the root policy cadence; this target has no independent timer.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=86400
	IntervalSeconds int32 `json:"intervalSeconds"`
	// Address receives generated regtest coinbase outputs.
	// +kubebuilder:validation:MinLength=14
	// +kubebuilder:validation:MaxLength=128
	Address string `json:"address"`
	// Paused stops new dispatches without cancelling one already armed.
	Paused bool `json:"paused,omitempty"`
}

// BitcoinProductionTarget retains one policy-owned execution and exclusion ledger.
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Blocks",type=integer,JSONPath=`.status.blocksProduced`
type BitcoinProductionTarget struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	// Spec is compiled by the owning production policy.
	Spec BitcoinProductionTargetSpec `json:"spec"`
	// Status retains dispatch evidence for this target's production lifetime.
	Status BitcoinProductionTargetStatus `json:"status,omitempty"`
}

// BitcoinProductionTargetList contains production resources.
// +kubebuilder:object:root=true
type BitcoinProductionTargetList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []BitcoinProductionTarget `json:"items"`
}

// BitcoinProductionTargetSpec binds one policy to an immutable network and target.
// +kubebuilder:validation:XValidation:rule="self.networkName == oldSelf.networkName && self.networkUID == oldSelf.networkUID && self.productionUID == oldSelf.productionUID && self.policy.target == oldSelf.policy.target",message="network, production owner and target are immutable"
type BitcoinProductionTargetSpec struct {
	// ProductionUID pins the owning aggregate production policy.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=64
	ProductionUID string `json:"productionUID"`
	// NetworkName identifies the owning StacksNetwork.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	NetworkName string `json:"networkName"`
	// NetworkUID prevents admission after parent replacement.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=64
	NetworkUID string `json:"networkUID"`
	// Policy supplies the current target, cadence, destination and pause state.
	Policy TargetPolicy `json:"policy"`
}

// BitcoinProductionTargetStatus is both the bounded dispatch ledger and user-facing state.
// +kubebuilder:validation:XValidation:rule="!(has(self.action) && has(self.reorganization))",message="only one action may reserve the executor"
type BitcoinProductionTargetStatus struct {
	// OpportunitiesConsumed is the latest observed target opportunity number, including skips.
	// +kubebuilder:validation:Minimum=0
	OpportunitiesConsumed int64 `json:"opportunitiesConsumed,omitempty"`
	// OpportunitiesSkipped counts consumed opportunities that authorized no baseline RPC.
	// +kubebuilder:validation:Minimum=0
	OpportunitiesSkipped int64 `json:"opportunitiesSkipped,omitempty"`
	// LastSkipReason reports the most recent bounded skip classification.
	// +kubebuilder:validation:Enum=Unavailable;Reserved;Outstanding;Expired;PolicyChanged;Capacity;Unobserved
	LastSkipReason string `json:"lastSkipReason,omitempty"`
	// Reorganization retains the finite replacement and its cleanup obligation.
	Reorganization *ReorganizationReservation `json:"reorganization,omitempty"`
	// Action reserves this ledger for one bounded action through durable accounting.
	Action *GenerationReservation `json:"action,omitempty"`
	// ObservedGeneration identifies the policy reflected by Phase.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// Phase is Waiting, Reserved, Collecting, Accounting, Running, Paused, Blocked, or Abandoned.
	Phase string `json:"phase,omitempty"`
	// Message explains the current operational state without credential material.
	Message string `json:"message,omitempty"`
	// DispatchState is empty before first use, Idle after a receipt, or Armed until resolved.
	// +kubebuilder:validation:Enum=Idle;Armed
	DispatchState string `json:"dispatchState,omitempty"`
	// DispatchID uniquely identifies the last authorization, including its process-start nonce.
	DispatchID string `json:"dispatchID,omitempty"`
	// TargetUID records the admitted leaf incarnation.
	TargetUID string `json:"targetUID,omitempty"`
	// PodUID records the admitted Pod incarnation.
	PodUID string `json:"podUID,omitempty"`
	// ContainerID records the admitted Bitcoin process container.
	ContainerID string `json:"containerID,omitempty"`
	// BlocksProduced counts blocks acknowledged and atomically accounted by this ledger.
	BlocksProduced int64 `json:"blocksProduced,omitempty"`
	// LastBlockHash is the latest acknowledged block; it is not a canonical-chain assertion.
	LastBlockHash string `json:"lastBlockHash,omitempty"`
	// LastCompletedAt anchors cadence across ordinary controller restarts.
	LastCompletedAt *metav1.Time `json:"lastCompletedAt,omitempty"`
}

// GenerationReservation keeps generation exclusion in the atomic ledger.
type GenerationReservation struct {
	ActionReservation `json:",inline"`
	// Spec snapshots the admitted typed operation.
	Spec actionv1alpha1.BitcoinBlockGenerationSpec `json:"spec"`
	// NextDispatchAt is persisted with the preceding receipt; it never authorizes replay.
	NextDispatchAt *metav1.MicroTime `json:"nextDispatchAt,omitempty"`
}

// ActionReservation retains shared identity and receipt accounting facts.
type ActionReservation struct {
	// Name identifies the selected action object.
	Name string `json:"name"`
	// UID prevents action replacement from inheriting authority.
	UID string `json:"uid"`
	// Generation records admission metadata; deletion may advance it without changing the immutable spec.
	Generation int64 `json:"generation"`
	// AdmittedAt records first reservation time.
	AdmittedAt metav1.Time `json:"admittedAt"`
	// ExpiresAt is the action's creation-relative deadline.
	ExpiresAt metav1.Time `json:"expiresAt"`
	// Network captures admission against the current parent catalog.
	Network actionv1alpha1.NetworkIdentity `json:"network"`
	// Target freezes the actor/runtime identity for all dispatches.
	Target actionv1alpha1.TargetIdentity `json:"target"`
	// Policy records the ledger and static configuration profile.
	Policy actionv1alpha1.PolicyIdentity `json:"policy"`
	// CorrelationID snapshots the external correlation label.
	CorrelationID string `json:"correlationID,omitempty"`
	// StartedAt records the first durable authorization.
	StartedAt *metav1.Time `json:"startedAt,omitempty"`
	// BlocksGenerated counts acknowledged receipts, separately from baseline blocks.
	BlocksGenerated int32 `json:"blocksGenerated,omitempty"`
	// LastBlockHash retains the latest action receipt.
	LastBlockHash string `json:"lastBlockHash,omitempty"`
	// LastDispatchID identifies the receipt to acknowledge in action status.
	LastDispatchID string `json:"lastDispatchID,omitempty"`
	// LastCompletedAt anchors the action's next dispatch.
	LastCompletedAt *metav1.Time `json:"lastCompletedAt,omitempty"`
	// StopReason freezes a definite admission/identity failure before another dispatch.
	StopReason string `json:"stopReason,omitempty"`
	// EffectUncertain marks lost receipt/process knowledge; time cannot clear it.
	EffectUncertain bool `json:"effectUncertain,omitempty"`
}

// ReorganizationReservation retains compensation obligations across dependent RPCs.
type ReorganizationReservation struct {
	ActionReservation `json:",inline"`
	// Spec snapshots the immutable replacement operation.
	Spec actionv1alpha1.BitcoinReorganizationSpec `json:"spec"`
	// OriginalChain fixes the original active tip.
	OriginalChain actionv1alpha1.BitcoinChainPoint `json:"originalChain"`
	// ForkParent fixes the retained ancestor.
	ForkParent actionv1alpha1.BitcoinChainPoint `json:"forkParent"`
	// InvalidatedHash fixes the sole compensation target.
	InvalidatedHash string `json:"invalidatedHash"`
	// Step identifies the current or most recently acknowledged typed mutation.
	// +kubebuilder:validation:Enum=Invalidate;Generate;Reconsider
	Step string `json:"step,omitempty"`
	// InvalidationAcknowledged means the first mutation returned successfully.
	InvalidationAcknowledged bool `json:"invalidationAcknowledged,omitempty"`
	// CleanupAcknowledged means compensation returned successfully.
	CleanupAcknowledged bool `json:"cleanupAcknowledged,omitempty"`
	// ReplacementBlockHashes retains at most depth+1 exact receipt hashes.
	// +kubebuilder:validation:MaxItems=7
	ReplacementBlockHashes []string `json:"replacementBlockHashes,omitempty"`
	// VerifiedBlocks counts receipts whose header ancestry has been checked.
	VerifiedBlocks int32 `json:"verifiedBlocks,omitempty"`
	// VerifiedTip anchors the next single-block request.
	VerifiedTip *actionv1alpha1.BitcoinChainPoint `json:"verifiedTip,omitempty"`
	// FinalChain records a successful local canonical/work observation.
	FinalChain *actionv1alpha1.BitcoinChainPoint `json:"finalChain,omitempty"`
	// CleanupUnsafe prevents compensation on a changed or expired execution boundary.
	CleanupUnsafe bool `json:"cleanupUnsafe,omitempty"`
}
