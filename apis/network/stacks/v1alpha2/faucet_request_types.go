package v1alpha2

import (
	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// StacksFaucetRequest is one user-owned transfer request bound to one network attempt.
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=".status.phase"
type StacksFaucetRequest struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	// Spec fixes the requested transfer and its original lifetime.
	Spec StacksFaucetRequestSpec `json:"spec"`
	// Status separates admission/projection from worker-owned execution evidence.
	Status FaucetRequestStatus `json:"status,omitempty"`
}

// StacksFaucetRequestList contains independently owned requests.
// +kubebuilder:object:root=true
type StacksFaucetRequestList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	// Items contains request objects.
	Items []StacksFaucetRequest `json:"items"`
}

// StacksFaucetRequestSpec is immutable, including its deadline basis.
// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="request spec is immutable"
type StacksFaucetRequestSpec struct {
	// NetworkUID prevents a request from following a replacement environment.
	// +kubebuilder:validation:Type=string
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=128
	NetworkUID types.UID `json:"networkUID"`
	// FaucetRef selects one logical faucet participant in this namespace.
	FaucetRef common.NameRef `json:"faucetRef"`
	// Destination is resolved once to a concrete public address.
	Destination Recipient `json:"destination"`
	// AmountMicroSTX is a positive uint64 transfer amount.
	// +kubebuilder:validation:XValidation:rule="self != '0'",message="amount must be positive"
	AmountMicroSTX common.Amount `json:"amountMicroSTX"`
	// Timeout starts at creationTimestamp; the controller never extends it.
	// +kubebuilder:validation:XValidation:rule="duration(self) > duration('0s') && duration(self) <= duration('1h')",message="timeout must be positive and at most one hour"
	Timeout common.Duration `json:"timeout"`
}

// FaucetBinding is a bounded exact public identity used in immutable execution admission.
type FaucetBinding struct {
	// Kind identifies the bound public resource.
	// +kubebuilder:validation:MaxLength=64
	Kind string `json:"kind"`
	// Name identifies the same-namespace object.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	Name string `json:"name"`
	// UID prevents identity replacement.
	// +kubebuilder:validation:Type=string
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=128
	UID types.UID `json:"uid"`
	// Fingerprint pins resolved public content where applicable.
	// +kubebuilder:validation:MaxLength=71
	Fingerprint string `json:"fingerprint,omitempty"`
}

// FaucetAdmission is owned solely by the request controller.
// +kubebuilder:validation:XValidation:rule="oldSelf.decision != 'Admitted' || self == oldSelf",message="execution admission is immutable once granted"
// +kubebuilder:validation:XValidation:rule="oldSelf.decision != 'Rejected' && oldSelf.decision != 'Expired' || self == oldSelf",message="negative terminal admission is immutable"
// +kubebuilder:validation:XValidation:rule="self.decision != 'Admitted' || has(self.faucet) && has(self.worker) && has(self.sourceAccount) && has(self.target) && has(self.destination) && has(self.amountMicroSTX) && has(self.feeMicroSTX) && has(self.profileDigest)",message="admission requires complete execution bindings"
type FaucetAdmission struct {
	// Decision separates waiting from granted or permanently refused execution authority.
	// +kubebuilder:validation:Enum=Pending;Admitted;Rejected;Expired
	Decision FaucetDecision `json:"decision"`
	// Reason is a bounded controller classification.
	// +kubebuilder:validation:MaxLength=64
	Reason string `json:"reason"`
	// ExpiresAt is creationTimestamp + timeout, with subsecond precision preserved.
	// +kubebuilder:validation:Format=date-time
	// +kubebuilder:validation:MaxLength=64
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="absolute request deadline is immutable"
	ExpiresAt string `json:"expiresAt"`
	// NetworkUID is the exact environment admitted for execution.
	// +kubebuilder:validation:Type=string
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=128
	NetworkUID types.UID `json:"networkUID"`
	// Faucet is the exact generated participant, not the reusable definition.
	Faucet *FaucetBinding `json:"faucet,omitempty"`
	// Worker is the exact non-restarting Pod bound in the root ledger.
	Worker *FaucetBinding `json:"worker,omitempty"`
	// ProfileDigest pins the worker's mounted identities and immutable ingress profile.
	// +kubebuilder:validation:Pattern=`^sha256:[0-9a-f]{64}$`
	// +kubebuilder:validation:MaxLength=71
	ProfileDigest string `json:"profileDigest,omitempty"`
	// SourceAccount is the immutable funding account identity.
	SourceAccount *FaucetBinding `json:"sourceAccount,omitempty"`
	// Target is the exact admitted Stacks mutation ingress participant.
	Target *FaucetBinding `json:"target,omitempty"`
	// DestinationAccount records the optional account resolved to Destination.
	DestinationAccount *FaucetBinding `json:"destinationAccount,omitempty"`
	// Destination is the resolved immutable testnet principal.
	// +kubebuilder:validation:MaxLength=64
	// +kubebuilder:validation:Pattern=`^ST[0-9A-HJKMNP-TV-Z]{26,39}$`
	Destination string `json:"destination,omitempty"`
	// AmountMicroSTX is the immutable admitted transfer amount.
	AmountMicroSTX common.Amount `json:"amountMicroSTX,omitempty"`
	// FeeMicroSTX is the explicit release-selected transaction fee.
	FeeMicroSTX common.Amount `json:"feeMicroSTX,omitempty"`
}

// FaucetExecution is written only by the bound surviving worker process.
// +kubebuilder:validation:XValidation:rule="!self.noSend || !has(self.txID) && (self.phase == 'Rejected' || self.phase == 'Expired')",message="no-send evidence cannot identify a submitted transaction"
// +kubebuilder:validation:XValidation:rule="self.phase != 'Expired' || self.noSend",message="expiry requires affirmative no-send evidence"
// +kubebuilder:validation:XValidation:rule="self.phase != 'Completed' || !self.noSend && has(self.txID) && has(self.inclusionBlockID)",message="completion requires exact native inclusion"
// +kubebuilder:validation:XValidation:rule="self.phase != 'Rejected' || self.noSend || has(self.inclusionBlockID) || has(self.txID) && self.reason in ['RejectedFeeTooLow', 'RejectedBadNonce', 'RejectedConflictingNonceInMempool', 'RejectedNotEnoughFunds']",message="rejection requires no-send evidence, a classified native refusal with txID, or exact failed inclusion"
type FaucetExecution struct {
	// Phase reports dispatch and exact native outcomes independently of projection.
	// +kubebuilder:validation:Enum=Submitted;Completed;Rejected;Expired;Inconclusive
	Phase FaucetExecutionPhase `json:"phase"`
	// Reason classifies the observed outcome without raw RPC bodies.
	// +kubebuilder:validation:MaxLength=64
	Reason string `json:"reason"`
	// NetworkUID identifies the environment actually observed by this worker.
	// +kubebuilder:validation:Type=string
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=128
	NetworkUID types.UID `json:"networkUID"`
	// FaucetUID pins the admitted participant instance.
	// +kubebuilder:validation:Type=string
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=128
	FaucetUID types.UID `json:"faucetUID"`
	// WorkerUID pins the admitted Pod.
	// +kubebuilder:validation:Type=string
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=128
	WorkerUID types.UID `json:"workerUID"`
	// ProcessNonce identifies the surviving process that owns pending coordination.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=128
	ProcessNonce string `json:"processNonce"`
	// NoSend is affirmative worker evidence that dispatch never began.
	NoSend bool `json:"noSend"`
	// TxID is the exact signed transaction identity when dispatch began.
	// +kubebuilder:validation:Pattern=`^[0-9a-f]{64}$`
	TxID string `json:"txID,omitempty"`
	// Destination and AmountMicroSTX retain the actual immutable transfer inputs.
	// +kubebuilder:validation:MaxLength=64
	Destination string `json:"destination"`
	// AmountMicroSTX is the exact micro-STX amount.
	AmountMicroSTX common.Amount `json:"amountMicroSTX"`
	// InclusionBlockID identifies the native canonical inclusion, when observed.
	// +kubebuilder:validation:Pattern=`^[0-9a-f]{64}$`
	InclusionBlockID string `json:"inclusionBlockID,omitempty"`
	// ObservedAt is the original successful observation time retained through API retries.
	ObservedAt metav1.Time `json:"observedAt"`
}

// FaucetRequestStatus contains the request controller's projection and disjoint worker evidence.
type FaucetRequestStatus struct {
	// Admission is written by the request controller.
	Admission *FaucetAdmission `json:"admission,omitempty"`
	// Phase is a projection, never independent transaction evidence.
	// +kubebuilder:validation:Enum=Pending;Submitted;Completed;Rejected;Inconclusive;Expired
	Phase FaucetPhase `json:"phase,omitempty"`
	// Conditions belong solely to the request controller.
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
	// Execution belongs solely to the admitted worker.
	Execution *FaucetExecution `json:"execution,omitempty"`
}

// FaucetRetainedOutcome preserves the latest outcome after a request object is deleted.
type FaucetRetainedOutcome struct {
	// Request is the exact deleted request identity.
	Request FaucetBinding `json:"request"`
	// Execution preserves bounded public facts; signed bytes and nonces are excluded.
	Execution FaucetExecution `json:"execution"`
}

// FaucetWorkerSummary reports capacity and deleted-request evidence without a durable nonce ledger.
type FaucetWorkerSummary struct {
	// Active counts locally unresolved requests and unpersisted outcomes.
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=1000
	Active int32 `json:"active"`
	// Orphaned counts deleted requests whose transaction outcome remains unresolved.
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=1000
	Orphaned int32 `json:"orphaned"`
	// Completed counts exact successful transfer inclusions observed by this process.
	// +kubebuilder:validation:Minimum=0
	Completed uint64 `json:"completed"`
	// Rejected counts no-send refusals, classified ingress refusals and failed inclusions.
	// +kubebuilder:validation:Minimum=0
	Rejected uint64 `json:"rejected"`
	// Expired counts affirmative no-send deadline or deletion acknowledgements.
	// +kubebuilder:validation:Minimum=0
	Expired uint64 `json:"expired"`
	// LastDeletedOutcome retains the latest deleted-request evidence until process destruction.
	LastDeletedOutcome *FaucetRetainedOutcome `json:"lastDeletedOutcome,omitempty"`
}
