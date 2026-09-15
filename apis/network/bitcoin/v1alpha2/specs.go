// Package v1alpha2 defines reusable Bitcoin configuration resources.
// +kubebuilder:object:generate=true
// +groupName=bitcoin.stacks.org
package v1alpha2

import common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"

// BitcoinNodeSpec describes Core without a mining role.
type BitcoinNodeSpec struct {
	common.ActorFields  `json:",inline"`
	common.WorkerFields `json:",inline"`
	// Observer enables the optional network-owned read-only RPC exporter; omission disables it.
	Observer *BitcoinObserverSpec `json:"observer,omitempty"`
	// Peers supplies startup seed hints.
	Peers *common.Peers `json:"peers,omitempty"`
	// WalletRefs selects reusable wallet identities loaded by this node.
	// +kubebuilder:validation:MaxItems=100
	// +listType=map
	// +listMapKey=name
	WalletRefs *[]common.NameRef `json:"walletRefs,omitempty"`
}

// BitcoinObserverSpec configures local RPC observation independently of production.
type BitcoinObserverSpec struct {
	// Enabled starts the metrics and structured-log sidecar; omission is false.
	Enabled *bool `json:"enabled,omitempty"`
	// IntervalSeconds separates completed polls; omission uses 10 seconds.
	// +kubebuilder:validation:Minimum=5
	// +kubebuilder:validation:Maximum=300
	IntervalSeconds *int32 `json:"intervalSeconds,omitempty"`
}

// WalletKeySource selects one reusable wallet identity.
// +kubebuilder:validation:XValidation:rule="(has(self.generate) ? 1 : 0) + (has(self.secretRef) ? 1 : 0) + (has(self.stacksMinerAccountRef) ? 1 : 0) == 1",message="select exactly one wallet key source"
type WalletKeySource struct {
	// Generate requests a new wallet key.
	// +kubebuilder:validation:Enum=true
	Generate *bool `json:"generate,omitempty"`
	// SecretRef imports a supported descriptor.
	SecretRef *common.SecretKeyRef `json:"secretRef,omitempty"`
	// StacksMinerAccountRef derives a public watch-only miner descriptor.
	StacksMinerAccountRef *common.NameRef `json:"stacksMinerAccountRef,omitempty"`
}

// BitcoinWalletSpec defines a reusable wallet, without a global balance.
// +kubebuilder:validation:XValidation:rule="!has(self.watchOnly) || self.watchOnly",message="only watch-only Core wallets are supported; omit watchOnly or set it to true"
type BitcoinWalletSpec struct {
	// WalletName is the local Core wallet name; omission uses the CR name.
	// +kubebuilder:validation:MaxLength=253
	WalletName *string `json:"walletName,omitempty"`
	// KeySource defines wallet identity; omission requests generation.
	KeySource *WalletKeySource `json:"keySource,omitempty"`
	// WatchOnly omits private material from Core; only true or omission is supported.
	WatchOnly *bool `json:"watchOnly,omitempty"`
}

// Cadence separates timing from target selection.
// +kubebuilder:validation:XValidation:rule="!has(self.interval) || (duration(self.interval) >= duration('1s') && duration(self.interval) <= duration('1h'))",message="interval must be between 1s and 1h"
// +kubebuilder:validation:XValidation:rule="!has(self.minimumInterval) || (duration(self.minimumInterval) >= duration('1s') && duration(self.minimumInterval) <= duration('1h'))",message="minimumInterval must be between 1s and 1h"
// +kubebuilder:validation:XValidation:rule="!has(self.maximumInterval) || (duration(self.maximumInterval) >= duration('1s') && duration(self.maximumInterval) <= duration('1h'))",message="maximumInterval must be between 1s and 1h"
// +kubebuilder:validation:XValidation:rule="!has(self.minimumInterval) || !has(self.maximumInterval) || duration(self.maximumInterval) >= duration(self.minimumInterval)",message="maximumInterval must not be below minimumInterval"
// +kubebuilder:validation:XValidation:rule="self.mode == 'Fixed' ? (has(self.interval) && !has(self.minimumInterval) && !has(self.maximumInterval)) : (!has(self.interval) && has(self.minimumInterval) && has(self.maximumInterval))",message="cadence fields must match mode"
type Cadence struct {
	// Mode selects fixed or uniform timing.
	// +kubebuilder:validation:Enum=Fixed;Uniform
	Mode CadenceMode `json:"mode"`
	// Interval is the fixed duration.
	Interval *common.Duration `json:"interval,omitempty"`
	// MinimumInterval bounds uniform timing.
	MinimumInterval *common.Duration `json:"minimumInterval,omitempty"`
	// MaximumInterval bounds uniform timing.
	MaximumInterval *common.Duration `json:"maximumInterval,omitempty"`
}

// BitcoinBlockScheduleSpec defines immutable reusable timing.
type BitcoinBlockScheduleSpec struct {
	// Cadence controls block opportunities.
	Cadence Cadence `json:"cadence"`
}

// ProductionTarget gives one participant a positive selection weight.
type ProductionTarget struct {
	// NodeRef selects a BitcoinNode participant.
	NodeRef common.NameRef `json:"nodeRef"`
	// Weight controls relative selection probability.
	// +kubebuilder:validation:Minimum=1
	Weight int32 `json:"weight"`
}

// Initialization defines one-time network funding requirements.
type Initialization struct {
	// TargetNodeRef selects the initial generation target.
	TargetNodeRef common.NameRef `json:"targetNodeRef"`
	// MinimumHeight is the initial Bitcoin height.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=4294967295
	MinimumHeight int64 `json:"minimumHeight"`
	// MinerWalletRefs selects wallets receiving mature coinbase outputs.
	// +kubebuilder:validation:MaxItems=100
	// +listType=map
	// +listMapKey=name
	MinerWalletRefs *[]common.NameRef `json:"minerWalletRefs,omitempty"`
	// MatureOutputsPerMiner is required for every bootstrap miner.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=100
	MatureOutputsPerMiner int32 `json:"matureOutputsPerMiner"`
}

// BitcoinBlockProductionSpec defines ongoing baseline block production.
// +kubebuilder:validation:XValidation:rule="!(has(self.scheduleRef) && has(self.schedule))",message="select one schedule source"
type BitcoinBlockProductionSpec struct {
	// ScheduleRef selects immutable reusable timing.
	ScheduleRef *common.NameRef `json:"scheduleRef,omitempty"`
	// Schedule supplies inline timing.
	Schedule *BitcoinBlockScheduleSpec `json:"schedule,omitempty"`
	// Targets selects weighted generation targets.
	// +kubebuilder:validation:MaxItems=100
	Targets *[]ProductionTarget `json:"targets,omitempty"`
	// PayoutWalletRef supplies baseline payout identity.
	PayoutWalletRef *common.NameRef `json:"payoutWalletRef,omitempty"`
	// Initialization freezes initial funding behavior.
	Initialization *Initialization `json:"initialization,omitempty"`
}
