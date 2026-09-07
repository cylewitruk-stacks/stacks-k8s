package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// StacksGenesisProfile is an immutable public recipe copied into new networks by provisioning tools.
// It has no controller and is not a live dependency of an existing network.
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:validation:XValidation:rule="self.spec == oldSelf.spec",message="genesis profiles are immutable; create a new profile revision"
type StacksGenesisProfile struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	// Spec contains public chain parameters and optional predeclared accounts.
	Spec GenesisSpec `json:"spec"`
}

// StacksGenesisProfileList contains reusable public genesis recipes.
// +kubebuilder:object:root=true
type StacksGenesisProfileList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []StacksGenesisProfile `json:"items"`
}

// GenesisSpec defines shared chain initialization and protocol activation parameters.
type GenesisSpec struct {
	// UseTestGenesisChainstate includes the selected binary's test genesis allocations in addition to Balances.
	// Omission selects true in the built-in regtest renderer.
	UseTestGenesisChainstate *bool `json:"useTestGenesisChainstate,omitempty"`
	// Balances grants initial funds. Named entries may be selected by provisioning and bootstrap.
	// +kubebuilder:validation:MaxItems=1000
	// A name-keyed map rejects duplicates within the CEL cost budget (Kubernetes 1.32+).
	// +kubebuilder:validation:XValidation:rule="[self.filter(b, has(b.name) && b.name != '')].all(named, size(named.transformMapEntry(i, b, {b.name: i})) == size(named))",message="nonempty balance names must be unique"
	// +listType=map
	// +listMapKey=address
	Balances []GenesisBalance `json:"balances,omitempty"`
	// Epochs is the shared activation schedule; omission selects the built-in regtest schedule.
	// Empty or omitted epochs select the built-in schedule; an explicit schedule must be complete.
	// +kubebuilder:validation:MaxItems=14
	// +kubebuilder:validation:XValidation:rule="size(self) == 0 || self.map(e, e.name) == ['1.0','2.0','2.05','2.1','2.2','2.3','2.4','2.5','3.0','3.1','3.2','3.3','3.4','4.0']",message="epochs must contain the complete supported schedule in order"
	// +kubebuilder:validation:XValidation:rule="size(self) != 14 || [0,1,2,3,4,5,6,7,8,9,10,11,12].all(i, self[i].startHeight <= self[i+1].startHeight)",message="epoch heights must be nondecreasing"
	// +kubebuilder:validation:XValidation:rule="size(self) != 14 || (self[0].startHeight == 0 && self[1].startHeight == 0)",message="epochs 1.0 and 2.0 must start at zero"
	// +listType=atomic
	Epochs []GenesisEpoch `json:"epochs,omitempty"`
	// PoX defines the shared regtest cycle lengths.
	PoX *GenesisPoX `json:"pox,omitempty"`
}

// GenesisBalance grants one public address an initial micro-STX balance.
type GenesisBalance struct {
	// Name optionally identifies an account for external tooling.
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern=`^$|^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
	Name string `json:"name,omitempty"`
	// Address is the public Stacks principal; private keys belong in Secrets.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=64
	Address string `json:"address"`
	// Amount is the initial balance in micro-STX, not an ongoing funding instruction.
	// +kubebuilder:validation:Minimum=1
	Amount int64 `json:"amount"`
}

// GenesisEpoch declares one epoch's Bitcoin activation height.
type GenesisEpoch struct {
	// Name is the protocol epoch understood by the selected Stacks binary.
	// +kubebuilder:validation:Enum="1.0";"2.0";"2.05";"2.1";"2.2";"2.3";"2.4";"2.5";"3.0";"3.1";"3.2";"3.3";"3.4";"4.0"
	Name string `json:"name"`
	// StartHeight is the first burn height assigned to this epoch.
	// +kubebuilder:validation:Minimum=0
	StartHeight int64 `json:"startHeight"`
}

// GenesisPoX defines the regtest reward cycle shared by every node.
// +kubebuilder:validation:XValidation:rule="self.prepareLength < self.rewardCycleLength",message="prepare phase must be shorter than the complete reward cycle"
type GenesisPoX struct {
	// PrepareLength is the prepare phase duration in Bitcoin blocks.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=100000
	PrepareLength int32 `json:"prepareLength"`
	// RewardCycleLength is the complete cycle length, including its prepare phase.
	// +kubebuilder:validation:Minimum=2
	// +kubebuilder:validation:Maximum=100000
	RewardCycleLength int32 `json:"rewardCycleLength"`
}
