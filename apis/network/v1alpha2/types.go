package v1alpha2

import (
	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	stacks "github.com/cylewitruk-stacks/stacks-k8s/apis/network/stacks/v1alpha2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
)

// GroupVersion identifies the composable network API.
var GroupVersion = schema.GroupVersion{Group: "network.stacks.org", Version: "v1alpha2"}

// AddToScheme registers root, participant and genesis resource kinds.
func AddToScheme(s *runtime.Scheme) error {
	s.AddKnownTypes(GroupVersion, &StacksNetwork{}, &StacksNetworkList{}, &StacksNetworkParticipant{}, &StacksNetworkParticipantList{}, &StacksGenesis{}, &StacksGenesisList{}, &StacksEpochSchedule{}, &StacksEpochScheduleList{})
	metav1.AddToGroupVersion(s, GroupVersion)
	return nil
}

// StacksNetwork resolves explicit participants and freezes one genesis.
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced,shortName=stacksnet
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:validation:XValidation:rule="!has(oldSelf.status) || !has(oldSelf.status.genesisRef) || (has(self.spec.expectedInputDigest) == has(oldSelf.spec.expectedInputDigest) && (!has(self.spec.expectedInputDigest) || self.spec.expectedInputDigest == oldSelf.spec.expectedInputDigest))",message="expectedInputDigest is fixed after genesis capture"
// +kubebuilder:validation:XValidation:rule="self.metadata.name == 'network'",message="the canonical network name is network"
type StacksNetwork struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	// Spec describes desired composition.
	Spec StacksNetworkSpec `json:"spec"`
	// Status reports resolved, captured and supported lifecycle state.
	Status StacksNetworkStatus `json:"status,omitempty"`
}

// InstanceIdentity retains minimal allocation state after destructive removal.
// +kubebuilder:validation:XValidation:rule="!has(oldSelf.worker) || has(self.worker)",message="worker binding cannot be removed"
type InstanceIdentity struct {
	// Name is single-use within this network UID.
	Name string `json:"name"`
	// UID is the exact generated participant identity.
	UID types.UID `json:"uid"`
	// Removing makes omission irreversible even if intent is quickly re-added.
	Removing bool `json:"removing,omitempty"`
	// Worker retains the exact standalone management Pod and terminal disposal evidence.
	Worker *WorkerSession `json:"worker,omitempty"`
}

// StacksNetworkStatus is written only by the aggregate controller.
type StacksNetworkStatus struct {
	// ObservationPolicy identifies release freshness and progress parameters.
	ObservationPolicy *ObservationPolicy `json:"observationPolicy,omitempty"`
	// Phase reports the observed lifecycle, not protocol health.
	Phase string `json:"phase,omitempty"`
	// InputDigest identifies semantic candidate inputs.
	InputDigest string `json:"inputDigest,omitempty"`
	// GenesisRef binds the immutable artifact.
	GenesisRef *common.Binding `json:"genesisRef,omitempty"`
	// GenesisDigest identifies public chain configuration only.
	GenesisDigest string `json:"genesisDigest,omitempty"`
	// Identities records bounded allocation history.
	// +kubebuilder:validation:MaxItems=1000
	// +listType=map
	// +listMapKey=name
	Identities []InstanceIdentity `json:"identities,omitempty"`
	// Bitcoin pins network-owned execution and initialization records before workers activate.
	Bitcoin *BitcoinRuntimeStatus `json:"bitcoin,omitempty"`
	// Initialization preserves frozen gate progress and the current generation ceiling.
	Initialization *InitializationStatus `json:"initialization,omitempty"`
	// Conditions distinguish resolution, initialization and operation.
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// Source captures a reusable definition or inline provenance.
type Source struct {
	// Name identifies a reusable definition; empty for inline.
	Name string `json:"name,omitempty"`
	// UID pins a reusable definition; empty for inline.
	UID types.UID `json:"uid,omitempty"`
	// Generation identifies the resolved input revision.
	Generation int64 `json:"generation"`
	// Digest identifies inline or reusable public spec content.
	Digest string `json:"digest"`
}

// StacksNetworkParticipantSpec is aggregate-owned compiled intent.
type StacksNetworkParticipantSpec struct {
	// NetworkUID binds the canonical root.
	NetworkUID types.UID `json:"networkUID"`
	// ParticipantName identifies the root map-list entry.
	ParticipantName string `json:"participantName"`
	// Kind selects a domain controller.
	Kind ParticipantKind `json:"kind"`
	// Source records pinned input provenance.
	Source Source `json:"source"`
	// Configuration contains complete compiled values.
	Configuration Configuration `json:"configuration"`
	// Control projects the user's workload-specific operation.
	Control *Control `json:"control,omitempty"`
}

// Admission retains a complete validated public policy independently of candidate edits.
type Admission struct {
	// Source records the exact provenance of this retained complete policy.
	Source Source `json:"source"`
	// PolicyDigest identifies the whole admitted configuration.
	PolicyDigest string `json:"policyDigest"`
	// Configuration retains public requirements before runtime exists.
	Configuration Configuration `json:"configuration"`
	// Dependencies pins exact mandatory resource identities.
	// +kubebuilder:validation:MaxItems=1000
	Dependencies []common.Binding `json:"dependencies,omitempty"`
}

// ParticipantStatus separates candidate resolution from admitted policy.
type ParticipantStatus struct {
	// Admission contains the complete accepted public policy.
	Admission *Admission `json:"admission,omitempty"`
	// Runtime contains domain-owned workload identity and readiness observations.
	Runtime *ParticipantRuntimeStatus `json:"runtime,omitempty"`
	// BitcoinControl contains domain-owned control process termination evidence.
	BitcoinControl *BitcoinControlRuntimeStatus `json:"bitcoinControl,omitempty"`
	// Scheduling projects the production domain's effective retained baseline state.
	Scheduling *bitcoin.BitcoinSchedulingStatus `json:"scheduling,omitempty"`
	// Execution is owned only by the exact bound Stacks worker process.
	Execution *WorkerExecutionStatus `json:"execution,omitempty"`
	// Conditions report resolution and replacement requirements.
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// StacksNetworkParticipant is generated, never a reusable declaration.
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:subresource:status
// +kubebuilder:validation:XValidation:rule="self.spec.networkUID == oldSelf.spec.networkUID && self.spec.participantName == oldSelf.spec.participantName && self.spec.kind == oldSelf.spec.kind",message="participant identity is immutable"
type StacksNetworkParticipant struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	// Spec contains aggregate-owned compiled policy.
	Spec StacksNetworkParticipantSpec `json:"spec"`
	// Status separates aggregate admission, domain runtime and worker execution.
	Status ParticipantStatus `json:"status,omitempty"`
}

// Allocation freezes a concrete public principal and balance.
type Allocation struct {
	// Address is a resolved account principal.
	Address string `json:"address"`
	// AmountMicroSTX is a normalized decimal quantity.
	AmountMicroSTX common.Amount `json:"amountMicroSTX"`
}

// ContractBindings identifies exact public sBTC configuration.
type ContractBindings struct {
	// Deployer is the resolved deployment principal.
	Deployer string `json:"deployer"`
	// Bundle names the release contract manifest.
	Bundle string `json:"bundle"`
	// SourceHashes identifies contract sources.
	// +kubebuilder:validation:MaxProperties=32
	SourceHashes map[string]string `json:"sourceHashes"`
}

// Chain contains canonical public genesis inputs, excluding provenance.
type Chain struct {
	// Profile identifies the pinned profile revision.
	Profile string `json:"profile"`
	// Epochs contains ordered activation boundaries.
	// +kubebuilder:validation:MaxItems=14
	Epochs []Epoch `json:"epochs"`
	// PoX contains resolved cycle lengths.
	PoX PoX `json:"pox"`
	// Allocations contains deduplicated initial balances.
	// +kubebuilder:validation:MaxItems=1000
	Allocations []Allocation `json:"allocations"`
	// Contracts binds required public contract principals.
	Contracts ContractBindings `json:"contracts"`
}

// BootstrapRequirement preserves values needed to evaluate the initial cohort.
type BootstrapRequirement struct {
	// Kind freezes the domain role independently of whether its instance still exists.
	Kind ParticipantKind `json:"kind"`
	// Participant is the exact initial instance identity.
	Participant common.Binding `json:"participant"`
	// PolicyDigest identifies its initially admitted configuration.
	PolicyDigest string `json:"policyDigest"`
	// Dependencies pins public credential identities.
	// +kubebuilder:validation:MaxItems=1000
	Dependencies []common.Binding `json:"dependencies,omitempty"`
	// MiningEnabled preserves the initial Stacks node's mining role through bootstrap.
	MiningEnabled *bool `json:"miningEnabled,omitempty"`
	// AmountMicroSTX preserves required stake.
	AmountMicroSTX *common.Amount `json:"amountMicroSTX,omitempty"`
	// LockCycles preserves required coverage.
	LockCycles *int32 `json:"lockCycles,omitempty"`
	// RenewWhenRemainingCycles preserves maintenance needed before initial gates finish.
	RenewWhenRemainingCycles *int32 `json:"renewWhenRemainingCycles,omitempty"`
	// BitcoinPayoutWallet pins the public payout identity across production replacement.
	BitcoinPayoutWallet *common.Binding `json:"bitcoinPayoutWallet,omitempty"`
	// BitcoinInitialization preserves initial wallet/maturity requirements.
	BitcoinInitialization *bitcoin.Initialization `json:"bitcoinInitialization,omitempty"`
	// RegistryInitialization preserves exact initial bridge state.
	RegistryInitialization *stacks.RegistryInitialization `json:"registryInitialization,omitempty"`
	// Accounts preserves public account values used by this participant.
	// +kubebuilder:validation:MaxItems=1000
	Accounts []PublicAccount `json:"accounts,omitempty"`
}

// PublicAccount retains public account evidence without private material.
type PublicAccount struct {
	// Binding pins the source identity.
	Binding common.Binding `json:"binding"`
	// Identity retains the exact public key and address.
	Identity common.PublicIdentity `json:"identity"`
}

// Gate describes the fixed profile's initial observation boundary.
type Gate struct {
	// Name identifies one convergence gate.
	Name string `json:"name"`
	// BitcoinCeiling bounds new generation.
	BitcoinCeiling int64 `json:"bitcoinCeiling"`
	// TargetCycle identifies required enrollment coverage when applicable.
	TargetCycle *int64 `json:"targetCycle,omitempty"`
}

// Bootstrap freezes ordered gates and public cohort requirements.
type Bootstrap struct {
	// Gates defines the profile's fixed initialization boundaries.
	// +kubebuilder:validation:MaxItems=16
	Gates []Gate `json:"gates"`
	// Requirements preserves independently evaluable public cohort values.
	// +kubebuilder:validation:MaxItems=1000
	Requirements []BootstrapRequirement `json:"requirements"`
}

// GenesisSource provides non-chain provenance.
type GenesisSource struct {
	// NetworkUID identifies this independent environment.
	NetworkUID types.UID `json:"networkUID"`
	// InputDigest identifies captured semantic inputs.
	InputDigest string `json:"inputDigest"`
	// Dependencies pins genesis-source identities.
	// +kubebuilder:validation:MaxItems=1000
	Dependencies []common.Binding `json:"dependencies,omitempty"`
}

// StacksGenesisSpec is immutable after a single successful creation.
type StacksGenesisSpec struct {
	// Chain is the digest-bearing public chain configuration.
	Chain Chain `json:"chain"`
	// Source records captured network provenance.
	Source GenesisSource `json:"source"`
	// Bootstrap retains requirements outside the chain digest.
	Bootstrap Bootstrap `json:"bootstrap"`
}

// StacksGenesis is the network-owned freeze artifact.
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:subresource:status
// +kubebuilder:validation:XValidation:rule="self.spec == oldSelf.spec",message="genesis is immutable"
type StacksGenesis struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	// Spec stores captured immutable inputs.
	Spec StacksGenesisSpec `json:"spec"`
	// Status reports publication digest.
	Status common.ResolutionStatus `json:"status,omitempty"`
}

// StacksEpochSchedule is a reusable immutable activation schedule.
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:subresource:status
// +kubebuilder:validation:XValidation:rule="self.spec == oldSelf.spec",message="epoch schedules are immutable"
type StacksEpochSchedule struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	// Spec contains the complete ordered schedule.
	Spec StacksEpochScheduleSpec `json:"spec"`
	// Status reports structural resolution.
	Status common.ResolutionStatus `json:"status,omitempty"`
}

// StacksNetworkList contains API objects.
// +kubebuilder:object:root=true
type StacksNetworkList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	// Items contains objects.
	Items []StacksNetwork `json:"items"`
}

// StacksNetworkParticipantList contains API objects.
// +kubebuilder:object:root=true
type StacksNetworkParticipantList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	// Items contains objects.
	Items []StacksNetworkParticipant `json:"items"`
}

// StacksGenesisList contains API objects.
// +kubebuilder:object:root=true
type StacksGenesisList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	// Items contains objects.
	Items []StacksGenesis `json:"items"`
}

// StacksEpochScheduleList contains API objects.
// +kubebuilder:object:root=true
type StacksEpochScheduleList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	// Items contains objects.
	Items []StacksEpochSchedule `json:"items"`
}

// GetResolutionStatus exposes independent schedule validation.
func (v *StacksEpochSchedule) GetResolutionStatus() *common.ResolutionStatus { return &v.Status }
