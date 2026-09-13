// Package v1alpha2 defines the composable network and generated instance APIs.
// +kubebuilder:object:generate=true
// +groupName=network.stacks.org
package v1alpha2

import (
	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	stacks "github.com/cylewitruk-stacks/stacks-k8s/apis/network/stacks/v1alpha2"
	corev1 "k8s.io/api/core/v1"
)

// ParticipantKind is a closed union of supported network participants.
// +kubebuilder:validation:Enum=BitcoinNode;StacksNode;StacksSigner;StacksStacker;StacksFaucet;StacksContractSet;StacksTransactionProduction;BitcoinBlockProduction
type ParticipantKind string

const (
	// ParticipantBitcoinNode selects the BitcoinNode participant domain.
	ParticipantBitcoinNode ParticipantKind = bitcoin.KindBitcoinNode
	// ParticipantStacksNode selects the StacksNode participant domain.
	ParticipantStacksNode ParticipantKind = stacks.KindStacksNode
	// ParticipantStacksSigner selects the StacksSigner participant domain.
	ParticipantStacksSigner ParticipantKind = stacks.KindStacksSigner
	// ParticipantStacksStacker selects the StacksStacker participant domain.
	ParticipantStacksStacker ParticipantKind = stacks.KindStacksStacker
	// ParticipantStacksFaucet selects the StacksFaucet participant domain.
	ParticipantStacksFaucet ParticipantKind = stacks.KindStacksFaucet
	// ParticipantStacksContractSet selects the StacksContractSet participant domain.
	ParticipantStacksContractSet ParticipantKind = stacks.KindStacksContractSet
	// ParticipantStacksTransactionProduction selects the StacksTransactionProduction participant domain.
	ParticipantStacksTransactionProduction ParticipantKind = stacks.KindStacksTransactionProduction
	// ParticipantBitcoinBlockProduction selects the BitcoinBlockProduction participant domain.
	ParticipantBitcoinBlockProduction ParticipantKind = bitcoin.KindBitcoinBlockProduction
)

// Configuration contains exactly the branch selected by the outer kind.
type Configuration struct {
	// BitcoinNode configures Core.
	BitcoinNode *bitcoin.BitcoinNodeSpec `json:"bitcoinNode,omitempty"`
	// StacksNode configures a Stacks peer.
	StacksNode *stacks.StacksNodeSpec `json:"stacksNode,omitempty"`
	// StacksSigner configures consensus signing.
	StacksSigner *stacks.StacksSignerSpec `json:"stacksSigner,omitempty"`
	// StacksStacker configures protocol maintenance.
	StacksStacker *stacks.StacksStackerSpec `json:"stacksStacker,omitempty"`
	// StacksFaucet configures explicit funding.
	StacksFaucet *stacks.StacksFaucetSpec `json:"stacksFaucet,omitempty"`
	// StacksContractSet configures required contracts.
	StacksContractSet *stacks.StacksContractSetSpec `json:"stacksContractSet,omitempty"`
	// StacksTransactionProduction configures baseline traffic.
	StacksTransactionProduction *stacks.StacksTransactionProductionSpec `json:"stacksTransactionProduction,omitempty"`
	// BitcoinBlockProduction configures baseline Bitcoin opportunities.
	BitcoinBlockProduction *bitcoin.BitcoinBlockProductionSpec `json:"bitcoinBlockProduction,omitempty"`
}

// Definition supplies exactly one reusable reference or inline declaration.
// +kubebuilder:validation:XValidation:rule="has(self.ref) != has(self.inline)",message="select exactly one definition source"
type Definition struct {
	// Ref selects the reusable CR of the outer kind.
	Ref *common.NameRef `json:"ref,omitempty"`
	// Inline supplies the corresponding spec directly.
	Inline *Configuration `json:"inline,omitempty"`
}

// Control contains workload-specific desired operation, separate from policy.
type Control struct {
	// Suspended scales actors down with retained storage.
	Suspended *bool `json:"suspended,omitempty"`
	// Paused cooperatively holds supported baseline workers.
	Paused *bool `json:"paused,omitempty"`
}

// Participant is a named instance declaration. Names are single-use after allocation.
// +kubebuilder:validation:XValidation:rule="self.kind == oldSelf.kind",message="kind is immutable while a participant entry exists"
// +kubebuilder:validation:XValidation:rule="!has(self.definition.inline) || (has(self.definition.inline.bitcoinNode) == (self.kind == 'BitcoinNode'))",message="configuration branch must match participant kind"
// +kubebuilder:validation:XValidation:rule="!has(self.definition.inline) || (has(self.definition.inline.stacksNode) == (self.kind == 'StacksNode'))",message="configuration branch must match participant kind"
// +kubebuilder:validation:XValidation:rule="!has(self.definition.inline) || (has(self.definition.inline.stacksSigner) == (self.kind == 'StacksSigner'))",message="configuration branch must match participant kind"
// +kubebuilder:validation:XValidation:rule="!has(self.definition.inline) || (has(self.definition.inline.stacksStacker) == (self.kind == 'StacksStacker'))",message="configuration branch must match participant kind"
// +kubebuilder:validation:XValidation:rule="!has(self.definition.inline) || (has(self.definition.inline.stacksFaucet) == (self.kind == 'StacksFaucet'))",message="configuration branch must match participant kind"
// +kubebuilder:validation:XValidation:rule="!has(self.definition.inline) || (has(self.definition.inline.stacksContractSet) == (self.kind == 'StacksContractSet'))",message="configuration branch must match participant kind"
// +kubebuilder:validation:XValidation:rule="!has(self.definition.inline) || (has(self.definition.inline.stacksTransactionProduction) == (self.kind == 'StacksTransactionProduction'))",message="configuration branch must match participant kind"
// +kubebuilder:validation:XValidation:rule="!has(self.definition.inline) || (has(self.definition.inline.bitcoinBlockProduction) == (self.kind == 'BitcoinBlockProduction'))",message="configuration branch must match participant kind"
// +kubebuilder:validation:XValidation:rule="!has(self.overrides) || (has(self.overrides.bitcoinNode) == (self.kind == 'BitcoinNode'))",message="configuration branch must match participant kind"
// +kubebuilder:validation:XValidation:rule="!has(self.overrides) || (has(self.overrides.stacksNode) == (self.kind == 'StacksNode'))",message="configuration branch must match participant kind"
// +kubebuilder:validation:XValidation:rule="!has(self.overrides) || (has(self.overrides.stacksSigner) == (self.kind == 'StacksSigner'))",message="configuration branch must match participant kind"
// +kubebuilder:validation:XValidation:rule="!has(self.overrides) || (has(self.overrides.stacksStacker) == (self.kind == 'StacksStacker'))",message="configuration branch must match participant kind"
// +kubebuilder:validation:XValidation:rule="!has(self.overrides) || (has(self.overrides.stacksFaucet) == (self.kind == 'StacksFaucet'))",message="configuration branch must match participant kind"
// +kubebuilder:validation:XValidation:rule="!has(self.overrides) || (has(self.overrides.stacksContractSet) == (self.kind == 'StacksContractSet'))",message="configuration branch must match participant kind"
// +kubebuilder:validation:XValidation:rule="!has(self.overrides) || (has(self.overrides.stacksTransactionProduction) == (self.kind == 'StacksTransactionProduction'))",message="configuration branch must match participant kind"
// +kubebuilder:validation:XValidation:rule="!has(self.overrides) || (has(self.overrides.bitcoinBlockProduction) == (self.kind == 'BitcoinBlockProduction'))",message="configuration branch must match participant kind"
// +kubebuilder:validation:XValidation:rule="!has(self.control) || ((!has(self.control.suspended) || self.kind in ['BitcoinNode','StacksNode','StacksSigner']) && (!has(self.control.paused) || self.kind in ['StacksStacker','StacksContractSet','StacksTransactionProduction','BitcoinBlockProduction']))",message="control is unsupported for this kind"
type Participant struct {
	// Name is the network-relative instance identity.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
	Name string `json:"name"`
	// Kind selects the typed branch and referenced kind.
	Kind ParticipantKind `json:"kind"`
	// Definition supplies reusable or inline inputs.
	Definition Definition `json:"definition"`
	// Overrides applies explicit per-instance values after inherited inputs.
	Overrides *Configuration `json:"overrides,omitempty"`
	// Control applies independently of substantive updates.
	Control *Control `json:"control,omitempty"`
}

// Images sets immutable per-network actor fallbacks.
type Images struct {
	// Bitcoin selects Core.
	// +kubebuilder:validation:MaxLength=512
	Bitcoin *string `json:"bitcoin,omitempty"`
	// StacksNode selects Stacks peers.
	// +kubebuilder:validation:MaxLength=512
	StacksNode *string `json:"stacksNode,omitempty"`
	// StacksSigner selects consensus signing.
	// +kubebuilder:validation:MaxLength=512
	StacksSigner *string `json:"stacksSigner,omitempty"`
}

// ActorResources supplies per-kind resource defaults.
type ActorResources struct {
	// Bitcoin is the Core workload budget.
	Bitcoin *corev1.ResourceRequirements `json:"bitcoin,omitempty"`
	// StacksNode is the peer/miner workload budget.
	StacksNode *corev1.ResourceRequirements `json:"stacksNode,omitempty"`
	// StacksSigner is the consensus workload budget.
	StacksSigner *corev1.ResourceRequirements `json:"stacksSigner,omitempty"`
}

// Defaults supplies stable inputs before definition and entry overrides.
type Defaults struct {
	// Images selects per-kind binaries.
	Images *Images `json:"images,omitempty"`
	// Storage supplies inherited actor storage.
	Storage *common.Storage `json:"storage,omitempty"`
	// ActorResources supplies inherited resource requirements.
	ActorResources *ActorResources `json:"actorResources,omitempty"`
	// WorkerPlacement applies to support Pods only.
	WorkerPlacement *common.Placement `json:"workerPlacement,omitempty"`
}

// GenesisAllocation contributes one immutable initial balance.
type GenesisAllocation struct {
	// AccountRef selects a public principal.
	AccountRef common.NameRef `json:"accountRef"`
	// AmountMicroSTX is the initial balance.
	AmountMicroSTX common.Amount `json:"amountMicroSTX"`
}

// GenesisInput defines immutable shared chain settings.
type GenesisInput struct {
	// Allocations enumerates funded accounts explicitly.
	// +kubebuilder:validation:MaxItems=1000
	Allocations []GenesisAllocation `json:"allocations,omitempty"`
	// PoX specifies regtest cycle lengths.
	PoX *PoX `json:"pox,omitempty"`
}

// PoX defines reward and preparation cycle lengths.
// +kubebuilder:validation:XValidation:rule="self.prepareLength < self.rewardCycleLength",message="prepare must be shorter than reward cycle"
type PoX struct {
	// RewardCycleLength counts Bitcoin blocks per cycle.
	// +kubebuilder:validation:Minimum=2
	// +kubebuilder:validation:Maximum=100000
	RewardCycleLength int32 `json:"rewardCycleLength"`
	// PrepareLength counts preparation blocks.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=99999
	PrepareLength int32 `json:"prepareLength"`
}

// Epoch selects one supported protocol boundary.
type Epoch struct {
	// Name identifies the protocol epoch as a string.
	// +kubebuilder:validation:Enum="1.0";"2.0";"2.05";"2.1";"2.2";"2.3";"2.4";"2.5";"3.0";"3.1";"3.2";"3.3";"3.4";"4.0"
	Name string `json:"name"`
	// StartHeight is the activation burn height.
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=4294967295
	StartHeight int64 `json:"startHeight"`
}

// StacksEpochScheduleSpec contains the complete immutable activation schedule.
type StacksEpochScheduleSpec struct {
	// Epochs are complete, ordered and nondecreasing.
	// +kubebuilder:validation:MinItems=14
	// +kubebuilder:validation:MaxItems=14
	// +listType=atomic
	// +kubebuilder:validation:XValidation:rule="self.map(e, e.name) == ['1.0','2.0','2.05','2.1','2.2','2.3','2.4','2.5','3.0','3.1','3.2','3.3','3.4','4.0']",message="epochs must be in supported order"
	// +kubebuilder:validation:XValidation:rule="[0,1,2,3,4,5,6,7,8,9,10,11,12].all(i, self[i].startHeight <= self[i+1].startHeight) && self[0].startHeight == 0 && self[1].startHeight == 0",message="epoch heights must be ordered with first two zero"
	Epochs []Epoch `json:"epochs"`
}

// StacksNetworkSpec is the user-facing composition and operation request.
// +kubebuilder:validation:XValidation:rule="!(has(self.epochScheduleRef) && has(self.epochSchedule))",message="schedule sources are exclusive"
// +kubebuilder:validation:XValidation:rule="has(self.profile) == has(oldSelf.profile) && (!has(self.profile) || self.profile == oldSelf.profile)",message="profile is immutable"
// +kubebuilder:validation:XValidation:rule="has(self.epochScheduleRef) == has(oldSelf.epochScheduleRef) && (!has(self.epochScheduleRef) || self.epochScheduleRef == oldSelf.epochScheduleRef)",message="epochScheduleRef is immutable"
// +kubebuilder:validation:XValidation:rule="has(self.epochSchedule) == has(oldSelf.epochSchedule) && (!has(self.epochSchedule) || self.epochSchedule == oldSelf.epochSchedule)",message="epochSchedule is immutable"
// +kubebuilder:validation:XValidation:rule="has(self.genesis) == has(oldSelf.genesis) && (!has(self.genesis) || self.genesis == oldSelf.genesis)",message="genesis is immutable"
// +kubebuilder:validation:XValidation:rule="has(self.defaults) == has(oldSelf.defaults) && (!has(self.defaults) || self.defaults == oldSelf.defaults)",message="defaults is immutable"
// +kubebuilder:validation:XValidation:rule="!has(self.participants) || size(self.participants.filter(p, p.kind == 'BitcoinNode')) <= 100",message="participant kind limit exceeded"
// +kubebuilder:validation:XValidation:rule="!has(self.participants) || size(self.participants.filter(p, p.kind == 'StacksNode')) <= 100",message="participant kind limit exceeded"
// +kubebuilder:validation:XValidation:rule="!has(self.participants) || size(self.participants.filter(p, p.kind == 'StacksSigner')) <= 100",message="participant kind limit exceeded"
// +kubebuilder:validation:XValidation:rule="!has(self.participants) || size(self.participants.filter(p, p.kind == 'StacksStacker')) <= 100",message="participant kind limit exceeded"
// +kubebuilder:validation:XValidation:rule="!has(self.participants) || size(self.participants.filter(p, p.kind == 'StacksFaucet')) <= 1",message="participant kind limit exceeded"
// +kubebuilder:validation:XValidation:rule="!has(self.participants) || size(self.participants.filter(p, p.kind == 'StacksContractSet')) <= 1",message="participant kind limit exceeded"
// +kubebuilder:validation:XValidation:rule="!has(self.participants) || size(self.participants.filter(p, p.kind == 'StacksTransactionProduction')) <= 1",message="participant kind limit exceeded"
// +kubebuilder:validation:XValidation:rule="!has(self.participants) || size(self.participants.filter(p, p.kind == 'BitcoinBlockProduction')) <= 1",message="participant kind limit exceeded"
type StacksNetworkSpec struct {
	// Operation requests resolution, cooperative pause or terminal stop.
	// +kubebuilder:default=Running
	// +kubebuilder:validation:Enum=Running;Paused;Stopped
	// +kubebuilder:validation:XValidation:rule="oldSelf != 'Stopped' || self == 'Stopped'",message="Stopped is terminal"
	Operation NetworkOperation `json:"operation,omitempty"`
	// Profile selects the supported immutable release profile.
	// +kubebuilder:default=regtest-pox4-pox5-v1
	// +kubebuilder:validation:Enum=regtest-pox4-pox5-v1
	Profile string `json:"profile,omitempty"`
	// EpochScheduleRef supplies a reusable schedule.
	EpochScheduleRef *common.NameRef `json:"epochScheduleRef,omitempty"`
	// EpochSchedule supplies inline schedule inputs.
	EpochSchedule *StacksEpochScheduleSpec `json:"epochSchedule,omitempty"`
	// Genesis configures funded accounts and PoX.
	Genesis *GenesisInput `json:"genesis,omitempty"`
	// Defaults supplies immutable per-network fallbacks.
	Defaults *Defaults `json:"defaults,omitempty"`
	// Participants selects named runtime instances.
	// +kubebuilder:validation:MaxItems=1000
	// +listType=map
	// +listMapKey=name
	Participants []Participant `json:"participants,omitempty"`
	// ExpectedInputDigest optionally pins reviewed initial semantic inputs.
	// +kubebuilder:validation:Pattern=`^sha256:[0-9a-f]{64}$`
	ExpectedInputDigest *string `json:"expectedInputDigest,omitempty"`
}
