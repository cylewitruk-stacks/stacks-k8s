package v1alpha2

import (
	stacks "github.com/cylewitruk-stacks/stacks-k8s/apis/network/stacks/v1alpha2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// WorkerPodBinding pins one standalone Pod with bounds for retained-ledger validation.
type WorkerPodBinding struct {
	// Kind is always the native Pod resource.
	// +kubebuilder:validation:Enum=Pod
	// +kubebuilder:validation:MaxLength=3
	Kind string `json:"kind"`
	// Name is the deterministic candidate resource name.
	// +kubebuilder:validation:MaxLength=253
	Name string `json:"name"`
	// UID identifies the exact Pod incarnation.
	// +kubebuilder:validation:Type=string
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=128
	UID types.UID `json:"uid"`
}

// WorkerCandidate reports one inactive standalone Pod for aggregate binding.
type WorkerCandidate struct {
	// Pod identifies the exact deterministic candidate incarnation.
	Pod WorkerPodBinding `json:"pod"`
	// ProfileDigest identifies fixed image and mounted key/config identities.
	// +kubebuilder:validation:Pattern=`^sha256:[0-9a-f]{64}$`
	// +kubebuilder:validation:MaxLength=71
	ProfileDigest string `json:"profileDigest"`
}

// WorkerSession retains the one authorized worker identity for an allocated participant.
// +kubebuilder:validation:XValidation:rule="self.pod == oldSelf.pod && self.profileDigest == oldSelf.profileDigest",message="bound worker identity is immutable"
type WorkerSession struct {
	// Pod is immutable once the aggregate binds its exact UID.
	Pod WorkerPodBinding `json:"pod"`
	// ProfileDigest pins immutable execution inputs independently of live policy.
	// +kubebuilder:validation:Pattern=`^sha256:[0-9a-f]{64}$`
	// +kubebuilder:validation:MaxLength=71
	ProfileDigest string `json:"profileDigest"`
	// Shutdown records an intentional disposal request before acknowledging worker exit.
	Shutdown *WorkerShutdown `json:"shutdown,omitempty"`
	// Disposal retains the final bounded outcome after worker acknowledgement or timeout.
	Disposal *WorkerDisposal `json:"disposal,omitempty"`
}

// WorkerShutdown is aggregate-owned terminal control for one retained worker session.
type WorkerShutdown struct {
	// NetworkGeneration identifies the root control observed before shutdown.
	NetworkGeneration int64 `json:"networkGeneration"`
	// Reason explains the intentional terminal control.
	// +kubebuilder:validation:Enum=NetworkStopped;NetworkDeleting;ParticipantRemoved
	Reason string `json:"reason"`
	// RequestedAt bounds settlement without claiming chain inclusion or server quiescence.
	RequestedAt metav1.Time `json:"requestedAt"`
}

// WorkerDisposal is retained aggregate evidence, not permission to replace a worker.
type WorkerDisposal struct {
	// Outcome distinguishes settled submission accounting from retained uncertainty.
	// +kubebuilder:validation:Enum=Settled;Unsettled
	Outcome string `json:"outcome"`
	// ProcessNonce identifies the acknowledging process when an acknowledgement exists.
	// +kubebuilder:validation:MaxLength=64
	ProcessNonce string `json:"processNonce,omitempty"`
	// ObservedAt records acknowledgement or bounded-settlement expiry.
	ObservedAt metav1.Time `json:"observedAt"`
	// Terminated records observed exact-process termination, never mere Pod absence.
	Terminated bool `json:"terminated"`
}

// WorkerExecutionStatus is written only by the surviving bound worker process.
type WorkerExecutionStatus struct {
	// Traffic retains current canonical availability independently of original inclusion progress.
	Traffic *TrafficObservation `json:"traffic,omitempty"`
	// Faucet retains bounded request accounting for this process.
	Faucet *stacks.FaucetWorkerSummary `json:"faucet,omitempty"`
	// PodUID identifies the worker receiving downward-API identity.
	PodUID types.UID `json:"podUID"`
	// ProcessNonce prevents an accidental same-Pod process restart from resuming execution.
	// +kubebuilder:validation:MaxLength=64
	ProcessNonce string `json:"processNonce"`
	// ProfileDigest identifies immutable execution inputs.
	// +kubebuilder:validation:Pattern=`^sha256:[0-9a-f]{64}$`
	// +kubebuilder:validation:MaxLength=71
	ProfileDigest string `json:"profileDigest"`
	// AppliedPolicyDigest identifies the complete policy adopted at a safe role boundary.
	// +kubebuilder:validation:MaxLength=128
	AppliedPolicyDigest string `json:"appliedPolicyDigest,omitempty"`
	// ObservedGeneration identifies the participant controls evaluated.
	ObservedGeneration int64 `json:"observedGeneration"`
	// NetworkGeneration identifies the root control acknowledged.
	NetworkGeneration int64 `json:"networkGeneration"`
	// Phase reports process-local execution without claiming protocol completion.
	// +kubebuilder:validation:Enum=Inactive;Active;Paused;Blocked;Draining;Settled;Unsettled;Unknown;Failed
	Phase string `json:"phase"`
	// Reason is a bounded public classification without private error contents.
	// +kubebuilder:validation:MaxLength=128
	Reason string `json:"reason,omitempty"`
	// ObservedAt records the original successful state observation.
	ObservedAt metav1.Time `json:"observedAt"`
	// Pending counts unresolved local submissions without exposing nonce history.
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=1000
	Pending int32 `json:"pending"`
	// Transactions contains cumulative submission facts for this worker process.
	Transactions *TransactionExecutionStatus `json:"transactions,omitempty"`
	// PoX4 retains the latest public native enrollment observation.
	PoX4 *PoX4EnrollmentObservation `json:"pox4,omitempty"`
	// PoX5 retains exact native direct-manager enrollment evidence.
	PoX5 *PoX5EnrollmentObservation `json:"pox5,omitempty"`
	// AdministratorTransactions contains the distinct administrator account stream, when needed.
	AdministratorTransactions *TransactionExecutionStatus `json:"administratorTransactions,omitempty"`
	// Contracts retains exact pinned source and native registry observations.
	Contracts *ContractSetObservation `json:"contracts,omitempty"`
}
