package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// ContractArtifact declares one immutable contract and its dependency names.
type ContractArtifact struct {
	// Name is the contract name under the set's deployer principal.
	// +kubebuilder:validation:MaxLength=40
	// +kubebuilder:validation:Pattern=`^[a-zA-Z][a-zA-Z0-9_-]{0,39}$`
	Name string `json:"name"`
	// SourceRef selects exact source bytes from a same-namespace ConfigMap.
	SourceRef ArtifactReference `json:"sourceRef"`
	// ClarityVersion determines the earliest supported language runtime.
	// +kubebuilder:validation:Enum=3;6
	ClarityVersion int32 `json:"clarityVersion"`
	// DependsOn names contracts in this set that must be observed first.
	// +kubebuilder:validation:MaxItems=16
	// +kubebuilder:validation:items:MaxLength=40
	// +listType=set
	DependsOn []string `json:"dependsOn,omitempty"`
}

// BridgeInitialization declares the explicit initial test registry state.
type BridgeInitialization struct {
	// SignerPublicKeys are the retained bridge keys, independent of consensus signers.
	// +kubebuilder:validation:MinItems=2
	// +kubebuilder:validation:MaxItems=2
	// +kubebuilder:validation:items:Pattern=`^(02|03)[0-9a-f]{64}$`
	// +kubebuilder:validation:items:MaxLength=66
	// +listType=set
	SignerPublicKeys []string `json:"signerPublicKeys"`
	// AggregatePublicKey is an explicit test key, not a DKG result.
	// +kubebuilder:validation:MaxLength=66
	// +kubebuilder:validation:Pattern=`^(02|03)[0-9a-f]{64}$`
	AggregatePublicKey string `json:"aggregatePublicKey"`
}

// ContractSetPolicy declares desired contract sources and optional bridge initialization.
// +kubebuilder:validation:XValidation:rule="self.contracts.all(c, !has(c.dependsOn) || c.dependsOn.all(d, d != c.name && self.contracts.exists(other, other.name == d)))",message="contract dependencies must name another contract in the set"
// +kubebuilder:validation:XValidation:rule="!has(self.bridge) || ['sbtc-registry', 'sbtc-token', 'sbtc-bootstrap-signers', 'sbtc-deposit', 'sbtc-withdrawal'].all(n, self.contracts.exists(c, c.name == n))",message="bridge initialization requires the complete standard contract set"
type ContractSetPolicy struct {
	// Name is the capability name within the network.
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern=`^[a-z0-9](?:[-a-z0-9]{0,61}[a-z0-9])?$`
	Name string `json:"name"`
	// Account names the exclusively assigned managed deployer account.
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern=`^[a-z0-9](?:[-a-z0-9]{0,61}[a-z0-9])?$`
	Account string `json:"account"`
	// Contracts are immutable named artifacts, not an imperative transaction plan.
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=16
	// +listType=map
	// +listMapKey=name
	Contracts []ContractArtifact `json:"contracts"`
	// Bridge initializes the standard sBTC registry after its declared contracts exist.
	Bridge *BridgeInitialization `json:"bridge,omitempty"`
	// Paused prevents new authorizations; existing receipts remain observable.
	Paused bool `json:"paused,omitempty"`
}

// StacksContractSet reconciles declared contract sources and initial registry state.
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
type StacksContractSet struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	// Spec binds contract intent to an owning environment.
	Spec StacksContractSetSpec `json:"spec"`
	// Status records observed contracts and exact transaction receipts.
	Status ManagedOperationStatus `json:"status,omitempty"`
}

// StacksContractSetList contains contract capabilities.
// +kubebuilder:object:root=true
type StacksContractSetList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	// Items contains the listed capabilities.
	Items []StacksContractSet `json:"items"`
}

// StacksContractSetSpec pins irreversible contract identity while allowing pause changes.
// +kubebuilder:validation:XValidation:rule="self.networkName == oldSelf.networkName && self.networkUID == oldSelf.networkUID && self.policy.name == oldSelf.policy.name && self.policy.account == oldSelf.policy.account && self.policy.contracts == oldSelf.policy.contracts",message="contract set identity and artifacts are immutable"
// +kubebuilder:validation:XValidation:rule="has(self.policy.bridge) == has(oldSelf.policy.bridge) && (!has(self.policy.bridge) || self.policy.bridge == oldSelf.policy.bridge)",message="initial bridge identity is immutable"
type StacksContractSetSpec struct {
	// NetworkName identifies the owning network.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	NetworkName string `json:"networkName"`
	// NetworkUID prevents admission after environment replacement.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=64
	NetworkUID string `json:"networkUID"`
	// Policy declares the desired contract set.
	Policy ContractSetPolicy `json:"policy"`
}

// StackingPolicy maintains one explicitly declared direct stacking participant.
// +kubebuilder:validation:XValidation:rule="self.holderAccount != self.administratorAccount",message="holder and administrator accounts must be distinct"
type StackingPolicy struct {
	// Name is the participant name within the network.
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern=`^[a-z0-9](?:[-a-z0-9]{0,61}[a-z0-9])?$`
	Name string `json:"name"`
	// Signer names the consensus signer actor independently of its holder.
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern=`^[a-z0-9](?:[-a-z0-9]{0,61}[a-z0-9])?$`
	Signer string `json:"signer"`
	// HolderAccount names the managed funded holder.
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern=`^[a-z0-9](?:[-a-z0-9]{0,61}[a-z0-9])?$`
	HolderAccount string `json:"holderAccount"`
	// AdministratorAccount names the manager's exclusive deployment/registration account.
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern=`^[a-z0-9](?:[-a-z0-9]{0,61}[a-z0-9])?$`
	AdministratorAccount string `json:"administratorAccount"`
	// ConsensusKeySecretRef supplies offline PoX authorizations separately from holder spending.
	ConsensusKeySecretRef ArtifactReference `json:"consensusKeySecretRef"`
	// AmountMicroSTX applies to new enrollments; it does not resize an existing lock.
	// PoX-4 enrollment also satisfies the observed dynamic threshold.
	// +kubebuilder:validation:Minimum=50000000000
	// +kubebuilder:validation:Maximum=9007199254740991
	AmountMicroSTX int64 `json:"amountMicroSTX"`
	// LockCycles is the enrollment horizon; renewal maintains this horizon in bounded extensions.
	// +kubebuilder:validation:Minimum=2
	// +kubebuilder:validation:Maximum=12
	LockCycles int32 `json:"lockCycles"`
	// Paused stops new enrollment and renewal, without unlocking an existing stake.
	Paused bool `json:"paused,omitempty"`
}

// StacksStackingParticipant maintains direct enrollment and renewal across supported protocols.
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
type StacksStackingParticipant struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	// Spec declares the participant's maintained policy.
	Spec StacksStackingParticipantSpec `json:"spec"`
	// Status records current observations and receipt acknowledgements.
	Status ManagedOperationStatus `json:"status,omitempty"`
}

// StacksStackingParticipantList contains direct stacking capabilities.
// +kubebuilder:object:root=true
type StacksStackingParticipantList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	// Items contains the listed capabilities.
	Items []StacksStackingParticipant `json:"items"`
}

// StacksStackingParticipantSpec pins identity while allowing ongoing policy changes.
// +kubebuilder:validation:XValidation:rule="self.networkName == oldSelf.networkName && self.networkUID == oldSelf.networkUID && self.policy.name == oldSelf.policy.name && self.policy.signer == oldSelf.policy.signer && self.policy.holderAccount == oldSelf.policy.holderAccount && self.policy.administratorAccount == oldSelf.policy.administratorAccount && self.policy.consensusKeySecretRef == oldSelf.policy.consensusKeySecretRef",message="participant identities are immutable"
type StacksStackingParticipantSpec struct {
	// NetworkName identifies the owning network.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	NetworkName string `json:"networkName"`
	// NetworkUID prevents admission after environment replacement.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=64
	NetworkUID string `json:"networkUID"`
	// Policy defines the maintained direct participant.
	Policy StackingPolicy `json:"policy"`
}

// OperationAcknowledgement retains an account receipt before permitting the account's next operation.
type OperationAcknowledgement struct {
	// Account identifies the ledger that owns this receipt.
	Account string `json:"account"`
	// Transaction contains public authorization and exact execution evidence.
	Transaction AccountTransaction `json:"transaction"`
}

// ManagedOperationStatus separates observed desired state from transaction completion.
type ManagedOperationStatus struct {
	// Conditions includes WorkerReady, independently of protocol execution evidence.
	// +kubebuilder:validation:MaxItems=8
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
	// ObservedGeneration identifies the declaration last inspected.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// Phase describes current convergence without implying permanent chain finality.
	// +kubebuilder:validation:Enum=Waiting;Paused;Pending;Ready;Blocked
	Phase string `json:"phase,omitempty"`
	// Message describes unmet prerequisites without raw RPC data or credentials.
	// +kubebuilder:validation:MaxLength=1024
	Message string `json:"message,omitempty"`
	// Protocol is the observed active PoX contract, when relevant.
	Protocol string `json:"protocol,omitempty"`
	// BurnHeight is the chain observation used for this status.
	BurnHeight int64 `json:"burnHeight,omitempty"`
	// UnlockHeight is the holder's observed current lock horizon.
	UnlockHeight int64 `json:"unlockHeight,omitempty"`
	// Receipts retains the latest acknowledgement for each exclusively used account.
	// +kubebuilder:validation:MaxItems=2
	// +listType=map
	// +listMapKey=account
	Receipts []OperationAcknowledgement `json:"receipts,omitempty"`
}
