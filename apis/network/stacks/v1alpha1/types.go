package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// GroupVersion identifies the Stacks capability API.
var GroupVersion = schema.GroupVersion{Group: "stacks.stacks.org", Version: "v1alpha1"}

// AddToScheme registers transaction-production resources.
func AddToScheme(scheme *runtime.Scheme) error {
	scheme.AddKnownTypes(GroupVersion, &StacksTransactionProduction{}, &StacksTransactionProductionList{},
		&StacksAccount{}, &StacksAccountList{}, &StacksContractSet{}, &StacksContractSetList{},
		&StacksStackingParticipant{}, &StacksStackingParticipantList{})
	metav1.AddToGroupVersion(scheme, GroupVersion)
	return nil
}

// TransferPolicy offers one tiny STX transfer at a time through one declared ingress.
type TransferPolicy struct {
	// CredentialsSecret names the same-namespace immutable worker credential document.
	// Bitcoin uses credentials.json; transfers use account.json.
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$`
	CredentialsSecret string `json:"credentialsSecret,omitempty"`
	// Target names the declared Stacks ingress actor.
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern=`^[a-z0-9](?:[-a-z0-9]{0,61}[a-z0-9])?$`
	Target string `json:"target"`
	// Sender identifies the administrator-provisioned exclusive test account.
	// +kubebuilder:validation:MaxLength=42
	// +kubebuilder:validation:Pattern=`^ST[0-9A-HJKMNP-TV-Z]{20,40}$`
	Sender string `json:"sender"`
	// Recipient receives the offered micro-STX amount.
	// +kubebuilder:validation:MaxLength=42
	// +kubebuilder:validation:Pattern=`^ST[0-9A-HJKMNP-TV-Z]{20,40}$`
	Recipient string `json:"recipient"`
	// AmountMicroSTX bounds each ordinary transfer to at most one STX.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=1000000
	AmountMicroSTX int64 `json:"amountMicroSTX"`
	// FeeMicroSTX is explicit; no automatic fee escalation occurs.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=1000000
	FeeMicroSTX int64 `json:"feeMicroSTX"`
	// IntervalSeconds is offered cadence, not a guaranteed block interval.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=86400
	IntervalSeconds int32 `json:"intervalSeconds"`
	// MinimumBurnHeight holds new transfers until the ingress observes this burn height.
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=9007199254740991
	MinimumBurnHeight int64 `json:"minimumBurnHeight,omitempty"`
	// Paused stops new signing/dispatch while outstanding evidence remains observable.
	Paused bool `json:"paused,omitempty"`
}

// StacksTransactionProduction maintains aggregate-owned, fixed-interval STX demand.
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Confirmed",type=integer,JSONPath=`.status.confirmed`
type StacksTransactionProduction struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	// Spec is the current aggregate-owned declaration.
	Spec StacksTransactionProductionSpec `json:"spec"`
	// Status retains bounded submission and inclusion evidence.
	Status StacksTransactionProductionStatus `json:"status,omitempty"`
}

// StacksTransactionProductionList contains transaction-production resources.
// +kubebuilder:object:root=true
type StacksTransactionProductionList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []StacksTransactionProduction `json:"items"`
}

// StacksTransactionProductionSpec binds an exclusive account to one network and ingress.
// +kubebuilder:validation:XValidation:rule="self.networkName == oldSelf.networkName && self.networkUID == oldSelf.networkUID && self.policy.target == oldSelf.policy.target && self.policy.sender == oldSelf.policy.sender",message="network, ingress and sender are immutable"
type StacksTransactionProductionSpec struct {
	// NetworkName names the owning StacksNetwork.
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:MinLength=1
	NetworkName string `json:"networkName"`
	// NetworkUID prevents admission after parent replacement.
	// +kubebuilder:validation:MaxLength=64
	// +kubebuilder:validation:MinLength=1
	NetworkUID string `json:"networkUID"`
	// Policy is the supported steady transfer profile.
	Policy TransferPolicy `json:"policy"`
}

// StacksTransactionProductionStatus is a bounded ledger, not a transaction indexer.
type StacksTransactionProductionStatus struct {
	// Conditions includes WorkerReady, independently of protocol execution evidence.
	// +kubebuilder:validation:MaxItems=8
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
	// ObservedGeneration identifies the declaration inspected by the controller.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// Phase is Waiting, Paused, Pending, Ambiguous, Running, Blocked, or Abandoned.
	// +kubebuilder:validation:Enum=Waiting;Paused;Pending;Ambiguous;Running;Blocked;Abandoned
	Phase string `json:"phase,omitempty"`
	// Message reports state without exposing credentials or raw server errors.
	Message string `json:"message,omitempty"`
	// Outstanding reserves this account until the exact transaction is accounted.
	Outstanding bool `json:"outstanding,omitempty"`
	// TxID identifies the signed transaction before its only authorized submission.
	TxID string `json:"txID,omitempty"`
	// Nonce is the last reserved account nonce.
	Nonce int64 `json:"nonce,omitempty"`
	// NextNonce equals Nonce while outstanding and becomes Nonce+1 after exact inclusion.
	NextNonce *int64 `json:"nextNonce,omitempty"`
	// TargetUID binds dispatch to an admitted StacksNode incarnation.
	TargetUID string `json:"targetUID,omitempty"`
	// PodUID records the admitted Pod incarnation.
	PodUID string `json:"podUID,omitempty"`
	// SubmittedAt records authorization time, including possibly-unsent requests.
	SubmittedAt *metav1.Time `json:"submittedAt,omitempty"`
	// Accepted reports whether the node acknowledged this TxID; it is not confirmation.
	Accepted bool `json:"accepted,omitempty"`
	// RejectionReason retains a matched ingress rejection for TxID, not a non-execution guarantee.
	// +kubebuilder:validation:Enum=FeeTooLow;BadNonce;Other
	RejectionReason string `json:"rejectionReason,omitempty"`
	// Confirmed counts exact successful canonical inclusions observed and accounted once.
	Confirmed int64 `json:"confirmed,omitempty"`
	// LastBlockID is the index block hash at the last successful inclusion observation.
	LastBlockID string `json:"lastBlockID,omitempty"`
	// LastConfirmedAt is the time of that observation, not a finality guarantee.
	LastConfirmedAt *metav1.Time `json:"lastConfirmedAt,omitempty"`
}
