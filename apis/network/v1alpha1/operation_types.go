package v1alpha1

import stacks "github.com/cylewitruk-stacks/stacks-k8s/apis/network/stacks/v1alpha1"

// NetworkOperation declares managed protocol responsibilities independently of genesis funding.
// +kubebuilder:validation:XValidation:rule="self.accounts.all(a, self.accounts.exists_one(b, b.address == a.address))",message="managed account addresses must be unique"
// +kubebuilder:validation:XValidation:rule="!has(self.contractSets) || self.contractSets.all(c, self.accounts.exists(a, a.name == c.account && a.consumerKind == 'StacksContractSet' && a.consumer == c.name))",message="contract sets require their exclusive managed deployer account"
// +kubebuilder:validation:XValidation:rule="!has(self.participants) || self.participants.all(p, p.holderAccount != p.administratorAccount && self.accounts.exists(a, a.name == p.holderAccount && a.consumerKind == 'StacksStackingParticipant' && a.consumer == p.name) && self.accounts.exists(a, a.name == p.administratorAccount && a.consumerKind == 'StacksStackingParticipant' && a.consumer == p.name))",message="participants require separate exclusive holder and administrator accounts"
type NetworkOperation struct {
	// Accounts explicitly opts funded accounts into exclusive controller management.
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=32
	// +listType=map
	// +listMapKey=name
	Accounts []stacks.ManagedAccountPolicy `json:"accounts"`
	// ContractSets declares contract prerequisites independently of epoch activation.
	// +kubebuilder:validation:MaxItems=8
	// +listType=map
	// +listMapKey=name
	ContractSets []stacks.ContractSetPolicy `json:"contractSets,omitempty"`
	// Participants declares maintained stacking independently of actor existence.
	// +kubebuilder:validation:MaxItems=16
	// +listType=map
	// +listMapKey=name
	Participants []stacks.StackingPolicy `json:"participants,omitempty"`
	// Paused stops new protocol transactions while preserving desired policies and receipt observation.
	Paused bool `json:"paused,omitempty"`
}

// CapabilityIdentity pins a managed child incarnation across removal and re-addition.
type CapabilityIdentity struct {
	// Kind identifies the managed capability API kind.
	// +kubebuilder:validation:Enum=StacksAccount;StacksContractSet;StacksStackingParticipant
	Kind string `json:"kind"`
	// Name is the compiled same-namespace resource name.
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
	// UID permanently identifies this ledger or consumer for the network incarnation.
	// +kubebuilder:validation:MaxLength=64
	// +kubebuilder:validation:MinLength=1
	UID string `json:"uid"`
}
