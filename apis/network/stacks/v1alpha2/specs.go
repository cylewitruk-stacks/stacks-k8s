// Package v1alpha2 defines reusable Stacks participants and account identities.
// +kubebuilder:object:generate=true
// +groupName=stacks.stacks.org
package v1alpha2

import common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"

// StacksAccountSpec selects one reusable identity.
type StacksAccountSpec struct {
	// Key defines imported/public/generated identity.
	Key *common.KeySource `json:"key,omitempty"`
}

// Mining configures the Stacks node's optional miner role.
type Mining struct {
	// Enabled controls mining; omission is false.
	Enabled *bool `json:"enabled,omitempty"`
	// BitcoinWalletRef supplies miner funding identity, including while disabled.
	BitcoinWalletRef *common.NameRef `json:"bitcoinWalletRef,omitempty"`
	// FeeRateSatsPerVByte controls Bitcoin commit fees.
	// +kubebuilder:validation:Minimum=1
	FeeRateSatsPerVByte *int32 `json:"feeRateSatsPerVByte,omitempty"`
}

// StacksNodeSpec describes a Stacks peer and its protected bindings.
type StacksNodeSpec struct {
	common.ActorFields `json:",inline"`
	// BitcoinNodeRef is a mandatory participant binding after composition.
	BitcoinNodeRef *common.NameRef `json:"bitcoinNodeRef,omitempty"`
	// IdentityAccountRef supplies P2P/miner identity.
	IdentityAccountRef *common.NameRef `json:"identityAccountRef,omitempty"`
	// Mining selects the optional miner role.
	Mining *Mining `json:"mining,omitempty"`
	// Peers supplies startup hints.
	Peers *common.Peers `json:"peers,omitempty"`
}

// StacksSignerSpec describes consensus signing independently of administration.
type StacksSignerSpec struct {
	common.ActorFields `json:",inline"`
	// NodeRef selects the associated StacksNode participant.
	NodeRef *common.NameRef `json:"nodeRef,omitempty"`
	// AccountRef supplies consensus signing identity.
	AccountRef *common.NameRef `json:"accountRef,omitempty"`
}

// StacksStackerSpec defines direct stacking and protocol maintenance.
// +kubebuilder:validation:XValidation:rule="!has(self.lockCycles) || !has(self.renewWhenRemainingCycles) || self.renewWhenRemainingCycles < self.lockCycles",message="renewal threshold must be below lock cycles"
type StacksStackerSpec struct {
	common.WorkerFields `json:",inline"`
	// HolderAccountRef funds stake.
	HolderAccountRef *common.NameRef `json:"holderAccountRef,omitempty"`
	// AdministratorAccountRef controls the direct manager.
	AdministratorAccountRef *common.NameRef `json:"administratorAccountRef,omitempty"`
	// SignerRef identifies a consensus signer participant.
	SignerRef *common.NameRef `json:"signerRef,omitempty"`
	// TargetNodeRef selects verified mutation ingress.
	TargetNodeRef *common.NameRef `json:"targetNodeRef,omitempty"`
	// AmountMicroSTX is desired stake.
	AmountMicroSTX *common.Amount `json:"amountMicroSTX,omitempty"`
	// LockCycles controls enrollment coverage.
	// +kubebuilder:validation:Minimum=2
	// +kubebuilder:validation:Maximum=12
	LockCycles *int32 `json:"lockCycles,omitempty"`
	// RenewWhenRemainingCycles triggers maintenance.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=11
	RenewWhenRemainingCycles *int32 `json:"renewWhenRemainingCycles,omitempty"`
}

// RegistryInitialization configures explicit test bridge state.
// +kubebuilder:validation:XValidation:rule="self.threshold <= size(self.signerAccountRefs)",message="threshold must not exceed signer count"
type RegistryInitialization struct {
	// Mode is the supported initialization authority.
	// +kubebuilder:validation:Enum=ExplicitTestRegistry
	Mode string `json:"mode"`
	// SignerAccountRefs supplies public bridge identities.
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=100
	// +listType=map
	// +listMapKey=name
	SignerAccountRefs []common.NameRef `json:"signerAccountRefs"`
	// AggregateKeyAccountRef supplies the explicit aggregate key.
	AggregateKeyAccountRef common.NameRef `json:"aggregateKeyAccountRef"`
	// Threshold bounds registry authorization.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=100
	Threshold int32 `json:"threshold"`
}

// StacksContractSetSpec selects the pinned real contract bundle.
type StacksContractSetSpec struct {
	common.WorkerFields `json:",inline"`
	// DeployerAccountRef supplies deployment identity.
	DeployerAccountRef *common.NameRef `json:"deployerAccountRef,omitempty"`
	// TargetNodeRef selects mutation ingress.
	TargetNodeRef *common.NameRef `json:"targetNodeRef,omitempty"`
	// Bundle selects the immutable source manifest.
	// +kubebuilder:validation:Enum=sbtc-regtest-v1
	Bundle *string `json:"bundle,omitempty"`
	// Initialization specifies registry postconditions.
	Initialization *RegistryInitialization `json:"initialization,omitempty"`
}

// StacksFaucetSpec offers explicit transfers independently of stacking.
type StacksFaucetSpec struct {
	common.WorkerFields `json:",inline"`
	// AccountRef selects funding identity; omission generates one.
	AccountRef *common.NameRef `json:"accountRef,omitempty"`
	// GenesisBalanceMicroSTX contributes one immutable allocation.
	GenesisBalanceMicroSTX *common.Amount `json:"genesisBalanceMicroSTX,omitempty"`
	// TargetNodeRef selects native ingress.
	TargetNodeRef *common.NameRef `json:"targetNodeRef,omitempty"`
	// MaxRequestMicroSTX bounds each transfer.
	MaxRequestMicroSTX *common.Amount `json:"maxRequestMicroSTX,omitempty"`
}

// Recipient identifies an account or explicit testnet destination.
// +kubebuilder:validation:XValidation:rule="has(self.accountRef) != has(self.address)",message="select an account or address"
type Recipient struct {
	// AccountRef selects a resolved public identity.
	AccountRef *common.NameRef `json:"accountRef,omitempty"`
	// Address is an explicit destination.
	// +kubebuilder:validation:Pattern=`^ST[0-9A-HJKMNP-TV-Z]{26,39}$`
	// +kubebuilder:validation:MaxLength=64
	Address *string `json:"address,omitempty"`
}

// StacksTransactionProductionSpec defines offered transfer cadence.
type StacksTransactionProductionSpec struct {
	common.WorkerFields `json:",inline"`
	// AccountRef selects signing identity.
	AccountRef *common.NameRef `json:"accountRef,omitempty"`
	// TargetNodeRef selects native ingress.
	TargetNodeRef *common.NameRef `json:"targetNodeRef,omitempty"`
	// Recipient receives baseline transfers.
	Recipient *Recipient `json:"recipient,omitempty"`
	// AmountMicroSTX is transfer amount.
	AmountMicroSTX *common.Amount `json:"amountMicroSTX,omitempty"`
	// FeeMicroSTX is transfer fee.
	FeeMicroSTX *common.Amount `json:"feeMicroSTX,omitempty"`
	// Interval is the offered cadence.
	Interval *common.Duration `json:"interval,omitempty"`
}
