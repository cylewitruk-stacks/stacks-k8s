package v1alpha1

import (
	bitcoinv1alpha1 "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha1"
	stacksv1alpha1 "github.com/cylewitruk-stacks/stacks-k8s/apis/network/stacks/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// StacksNetwork declares the desired topology of one Stacks regtest network.
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:validation:XValidation:rule="self.metadata.name.size() <= 63 && self.metadata.name.matches('^[a-z0-9]([-a-z0-9]*[a-z0-9])?$')",message="metadata.name must be a DNS label no longer than 63 characters"
// +kubebuilder:printcolumn:name="Ready",type=integer,JSONPath=`.status.readyActors`,description="Ready actors"
// +kubebuilder:printcolumn:name="Desired",type=integer,JSONPath=`.status.desiredActors`,description="Desired actors"
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
type StacksNetwork struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              StacksNetworkSpec   `json:"spec"`
	Status            StacksNetworkStatus `json:"status,omitempty"`
}

// StacksNetworkList contains StacksNetwork resources.
// +kubebuilder:object:root=true
type StacksNetworkList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []StacksNetwork `json:"items"`
}

// StacksNetworkSpec defines an aggregate regtest topology.
// +kubebuilder:validation:XValidation:rule="!has(self.stacksTransactionProduction) || (has(self.stacksNodes) && self.stacksNodes.exists(node, node.name == self.stacksTransactionProduction.target))",message="transaction production must reference a declared Stacks node"
// +kubebuilder:validation:XValidation:rule="!has(oldSelf.stacksTransactionProduction) || !has(self.stacksTransactionProduction) || (self.stacksTransactionProduction.target == oldSelf.stacksTransactionProduction.target && self.stacksTransactionProduction.sender == oldSelf.stacksTransactionProduction.sender)",message="changing transaction ingress or sender requires a fresh environment"
// +kubebuilder:validation:XValidation:rule="!has(self.bitcoinBlockProduction) || self.bitcoinBlockProduction.targets.all(target, self.bitcoinNodes.exists(node, node.name == target.name))",message="production must reference a declared Bitcoin node"
// +kubebuilder:validation:XValidation:rule="!has(self.stacksNodes) || self.stacksNodes.all(node, self.bitcoinNodes.exists(bitcoin, bitcoin.name == node.bitcoinNodeRef))",message="every Stacks node must reference a declared Bitcoin node"
// +kubebuilder:validation:XValidation:rule="!has(self.signers) || (has(self.stacksNodes) && self.signers.all(signer, self.stacksNodes.exists(node, node.name == signer.nodeRef && node.role == 'signer-node')))",message="every signer must reference a declared signer-node"
// +kubebuilder:validation:XValidation:rule="!has(self.signers) || self.signers.all(signer, self.signers.exists_one(other, other.nodeRef == signer.nodeRef))",message="a signer-node may be referenced by only one signer"
// +kubebuilder:validation:XValidation:rule="!has(self.signers) || self.signers.all(signer, self.signers.exists_one(other, other.index == signer.index))",message="signer indices must be unique"
// +kubebuilder:validation:XValidation:rule="!has(self.stacksNodes) || self.stacksNodes.all(node, !has(node.serviceRefs) || node.serviceRefs.all(ref, self.bitcoinNodes.exists(actor, actor.name == ref) || (has(self.stacksNodes) && self.stacksNodes.exists(actor, actor.name == ref)) || (has(self.signers) && self.signers.exists(actor, actor.name == ref))))",message="Stacks node serviceRefs must name declared actors"
// +kubebuilder:validation:XValidation:rule="!has(self.signers) || self.signers.all(signer, !has(signer.serviceRefs) || signer.serviceRefs.all(ref, self.bitcoinNodes.exists(actor, actor.name == ref) || (has(self.stacksNodes) && self.stacksNodes.exists(actor, actor.name == ref)) || (has(self.signers) && self.signers.exists(actor, actor.name == ref))))",message="signer serviceRefs must name declared actors"
type StacksNetworkSpec struct {
	// StacksTransactionProduction optionally maintains fixed-interval STX transfers.
	StacksTransactionProduction *stacksv1alpha1.TransferPolicy `json:"stacksTransactionProduction,omitempty"`
	// BitcoinBlockProduction optionally offers fixed or jittered regtest block production.
	BitcoinBlockProduction *bitcoinv1alpha1.ProductionPolicy `json:"bitcoinBlockProduction,omitempty"`
	Suspended              bool                              `json:"suspended,omitempty"`
	Defaults               NetworkDefaults                   `json:"defaults"`
	Genesis                *GenesisSpec                      `json:"genesis,omitempty"`
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=32
	// +listType=map
	// +listMapKey=name
	BitcoinNodes []BitcoinNodeTemplate `json:"bitcoinNodes"`
	// +kubebuilder:validation:MaxItems=100
	// +listType=map
	// +listMapKey=name
	StacksNodes []StacksNodeTemplate `json:"stacksNodes,omitempty"`
	// +kubebuilder:validation:MaxItems=100
	// +listType=map
	// +listMapKey=name
	Signers []StacksSignerTemplate `json:"signers,omitempty"`
}

// GenesisSpec contains network-wide values included by generated node profiles.
type GenesisSpec struct {
	// +kubebuilder:validation:MaxItems=1000
	// +listType=map
	// +listMapKey=address
	Balances []GenesisBalance `json:"balances,omitempty"`
}

// GenesisBalance grants one address an initial micro-STX balance.
type GenesisBalance struct {
	Address string `json:"address"`
	// +kubebuilder:validation:Minimum=1
	Amount int64 `json:"amount"`
}

// BitcoinNodeTemplate is the aggregate declaration for one Bitcoin node.
type BitcoinNodeTemplate struct {
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern=`^[a-z0-9](?:[-a-z0-9]{0,61}[a-z0-9])?$`
	Name   string       `json:"name"`
	Image  string       `json:"image,omitempty"`
	Config ConfigSource `json:"config"`
	// +kubebuilder:validation:MaxItems=31
	// +listType=set
	PeerRefs  []string           `json:"peerRefs,omitempty"`
	Workload  *WorkloadSpec      `json:"workload,omitempty"`
	Container *ContainerOverride `json:"container,omitempty"`
	Suspended bool               `json:"suspended,omitempty"`
}

// StacksNodeTemplate is the aggregate declaration for one Stacks node.
// +kubebuilder:validation:XValidation:rule="self.role != 'miner' || has(self.config.secretRef)",message="miner configuration must use a Secret reference"
type StacksNodeTemplate struct {
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern=`^[a-z0-9](?:[-a-z0-9]{0,61}[a-z0-9])?$`
	Name string `json:"name"`
	// +kubebuilder:validation:Enum=miner;follower;signer-node
	Role  StacksNodeRole `json:"role"`
	Image string         `json:"image,omitempty"`
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern=`^[a-z0-9](?:[-a-z0-9]{0,61}[a-z0-9])?$`
	BitcoinNodeRef string `json:"bitcoinNodeRef"`
	// ServiceRefs expose additional declared actor Services to raw configuration templates.
	// +kubebuilder:validation:MaxItems=8
	// +listType=set
	ServiceRefs []string           `json:"serviceRefs,omitempty"`
	Config      ConfigSource       `json:"config"`
	Workload    *WorkloadSpec      `json:"workload,omitempty"`
	Container   *ContainerOverride `json:"container,omitempty"`
	Suspended   bool               `json:"suspended,omitempty"`
}

// StacksSignerTemplate is the aggregate declaration for one Stacks signer.
// +kubebuilder:validation:XValidation:rule="has(self.config.secretRef)",message="signer configuration must use a Secret reference"
type StacksSignerTemplate struct {
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern=`^[a-z0-9](?:[-a-z0-9]{0,61}[a-z0-9])?$`
	Name  string `json:"name"`
	Image string `json:"image,omitempty"`
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern=`^[a-z0-9](?:[-a-z0-9]{0,61}[a-z0-9])?$`
	NodeRef string `json:"nodeRef"`
	// ServiceRefs expose additional declared actor Services to raw configuration templates.
	// +kubebuilder:validation:MaxItems=8
	// +listType=set
	ServiceRefs []string `json:"serviceRefs,omitempty"`
	// +kubebuilder:validation:Minimum=0
	Index int32 `json:"index"`
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=9007199254740991
	Weight    int64              `json:"weight"`
	PublicKey string             `json:"publicKey,omitempty"`
	Config    ConfigSource       `json:"config"`
	Workload  *WorkloadSpec      `json:"workload,omitempty"`
	Container *ContainerOverride `json:"container,omitempty"`
	Suspended bool               `json:"suspended,omitempty"`
}

// StacksNetworkStatus aggregates leaf readiness and admitted identity.
type StacksNetworkStatus struct {
	// TransactionProductionUID permanently pins the exclusive account ledger.
	TransactionProductionUID string `json:"transactionProductionUID,omitempty"`
	// BitcoinProductionUID permanently pins the production ledger for this network incarnation.
	BitcoinProductionUID string `json:"bitcoinProductionUID,omitempty"`
	// TargetDeclarations reports compiled intent without requiring global readiness.
	TargetDeclarations  *TargetDeclarations `json:"targetDeclarations,omitempty"`
	ObservedGeneration  int64               `json:"observedGeneration,omitempty"`
	Phase               string              `json:"phase,omitempty"`
	DesiredActors       int32               `json:"desiredActors,omitempty"`
	ReadyActors         int32               `json:"readyActors,omitempty"`
	InventoryReady      bool                `json:"inventoryReady,omitempty"`
	InventoryDigest     string              `json:"inventoryDigest,omitempty"`
	InventoryObservedAt *metav1.Time        `json:"inventoryObservedAt,omitempty"`
	// +kubebuilder:validation:MaxItems=232
	Actors []ActorIdentity `json:"actors,omitempty"`
	// +kubebuilder:validation:MaxItems=232
	Children   []ChildStatus      `json:"children,omitempty"`
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}
