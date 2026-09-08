package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// ArtifactReference pins one same-namespace ConfigMap or Secret entry by content.
type ArtifactReference struct {
	// Name identifies the object containing the entry.
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern=`^[a-z0-9](?:[-a-z0-9]{0,61}[a-z0-9])?$`
	Name string `json:"name"`
	// Key identifies the entry within the object.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:Pattern=`^[a-zA-Z0-9._-]+$`
	Key string `json:"key"`
	// Digest binds the exact bytes read before use.
	// +kubebuilder:validation:MaxLength=71
	// +kubebuilder:validation:Pattern=`^sha256:[0-9a-f]{64}$`
	Digest string `json:"digest"`
}

// ManagedAccountPolicy assigns an exclusive account to one declared capability.
type ManagedAccountPolicy struct {
	// Name is the account's stable name within its network.
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern=`^[a-z0-9](?:[-a-z0-9]{0,61}[a-z0-9])?$`
	Name string `json:"name"`
	// Address is the funded testnet principal; declaring funding alone does not manage it.
	// +kubebuilder:validation:MaxLength=41
	// +kubebuilder:validation:Pattern=`^ST[0-9A-HJKMNP-TV-Z]{26,39}$`
	Address string `json:"address"`
	// Target names the declared ingress actor.
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern=`^[a-z0-9](?:[-a-z0-9]{0,61}[a-z0-9])?$`
	Target string `json:"target"`
	// ConfigDigest approves the ingress configuration independently of its image.
	// +kubebuilder:validation:MaxLength=71
	// +kubebuilder:validation:Pattern=`^sha256:[0-9a-f]{64}$`
	ConfigDigest string `json:"configDigest"`
	// KeySecretRef contains the offline signing key; it is never mounted into an actor.
	KeySecretRef ArtifactReference `json:"keySecretRef"`
	// ConsumerKind identifies the only capability allowed to reserve this account.
	// +kubebuilder:validation:Enum=StacksContractSet;StacksStackingParticipant
	ConsumerKind string `json:"consumerKind"`
	// Consumer names that capability within this network.
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern=`^[a-z0-9](?:[-a-z0-9]{0,61}[a-z0-9])?$`
	Consumer string `json:"consumer"`
}

// StacksAccount retains the exclusive transaction authority of one managed account.
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
type StacksAccount struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	// Spec binds the account to its environment and consumer.
	Spec StacksAccountSpec `json:"spec"`
	// Status is a bounded account ledger, not a chain indexer.
	Status StacksAccountStatus `json:"status,omitempty"`
}

// StacksAccountList contains managed account ledgers.
// +kubebuilder:object:root=true
type StacksAccountList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	// Items contains the listed ledgers.
	Items []StacksAccount `json:"items"`
}

// StacksAccountSpec is immutable for the lifetime of account authority.
// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="account authority is immutable"
type StacksAccountSpec struct {
	// NetworkName identifies the owning network.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	NetworkName string `json:"networkName"`
	// NetworkUID prevents admission against another environment incarnation.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=64
	NetworkUID string `json:"networkUID"`
	// Policy declares the sole account consumer and signing identity.
	Policy ManagedAccountPolicy `json:"policy"`
}

// AccountTransaction records authorization and exact execution of one operation.
type AccountTransaction struct {
	// Ordinal is monotonic within this consumer's account; old operations cannot replay.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=9007199254740991
	Ordinal int64 `json:"ordinal"`
	// OperationDigest binds the consumer's explicit public intent.
	// +kubebuilder:validation:MaxLength=71
	// +kubebuilder:validation:Pattern=`^sha256:[0-9a-f]{64}$`
	OperationDigest string `json:"operationDigest"`
	// ConsumerUID binds the request to the observed capability incarnation.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=64
	ConsumerUID string `json:"consumerUID"`
	// TxID hashes the signed bytes before their sole submission attempt.
	// +kubebuilder:validation:Pattern=`^[0-9a-f]{64}$`
	TxID string `json:"txID"`
	// Nonce is the exclusively reserved canonical account nonce.
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=9007199254740990
	Nonce int64 `json:"nonce"`
	// TargetUID identifies the admitted StacksNode.
	TargetUID string `json:"targetUID"`
	// PodUID identifies the admitted Pod.
	PodUID string `json:"podUID"`
	// ContainerID identifies the admitted process container.
	ContainerID string `json:"containerID"`
	// AuthorizedAt is the authorization time, including possibly-unsent requests.
	AuthorizedAt metav1.Time `json:"authorizedAt"`
	// Accepted records an exact submission acknowledgement, not execution.
	Accepted bool `json:"accepted,omitempty"`
	// RejectionReason is a bounded matched native rejection; it does not free the nonce.
	// +kubebuilder:validation:Enum=FeeTooLow;BadNonce;Other
	RejectionReason string `json:"rejectionReason,omitempty"`
	// Receipt records observed canonical execution, including unsuccessful execution.
	Receipt *AccountReceipt `json:"receipt,omitempty"`
	// Acknowledged permits a newer ordinal after the consumer retains the receipt.
	Acknowledged bool `json:"acknowledged,omitempty"`
}

// AccountReceipt is execution evidence at an observation time, not permanent finality.
type AccountReceipt struct {
	// Source identifies the qualified native evidence path.
	// +kubebuilder:validation:Enum=NativeIndex;LegacyEvent
	Source string `json:"source,omitempty"`
	// BlockID is the observed canonical index block hash.
	// +kubebuilder:validation:Pattern=`^[0-9a-f]{64}$`
	BlockID string `json:"blockID"`
	// Success reports the transaction's response result.
	Success bool `json:"success"`
	// ObservedAt records receipt collection time.
	ObservedAt metav1.Time `json:"observedAt"`
}

// StacksAccountStatus preserves the latest operation and the next expected nonce.
type StacksAccountStatus struct {
	// Phase describes account execution without leaking signing inputs.
	// +kubebuilder:validation:Enum=Idle;Pending;Ambiguous;Blocked;Executed;Abandoned
	Phase string `json:"phase,omitempty"`
	// Transaction retains the current authorization until a newer acknowledged operation.
	Transaction *AccountTransaction `json:"transaction,omitempty"`
	// NextNonce advances only when an exact canonical execution has been accounted.
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=9007199254740991
	NextNonce *int64 `json:"nextNonce,omitempty"`
}
