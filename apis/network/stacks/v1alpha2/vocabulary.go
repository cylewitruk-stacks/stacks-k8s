package v1alpha2

// FaucetDecision names supported FaucetAdmission.Decision values. Unknown input still requires validation.
type FaucetDecision string

const (
	// FaucetDecisionPending is the Pending value of FaucetDecision.
	FaucetDecisionPending FaucetDecision = "Pending"
	// FaucetDecisionAdmitted is the Admitted value of FaucetDecision.
	FaucetDecisionAdmitted FaucetDecision = "Admitted"
	// FaucetDecisionRejected is the Rejected value of FaucetDecision.
	FaucetDecisionRejected FaucetDecision = "Rejected"
	// FaucetDecisionExpired is the Expired value of FaucetDecision.
	FaucetDecisionExpired FaucetDecision = "Expired"
)

// FaucetExecutionPhase names supported FaucetExecution.Phase values. Unknown input still requires validation.
type FaucetExecutionPhase string

const (
	// FaucetExecutionSubmitted is the Submitted value of FaucetExecutionPhase.
	FaucetExecutionSubmitted FaucetExecutionPhase = "Submitted"
	// FaucetExecutionCompleted is the Completed value of FaucetExecutionPhase.
	FaucetExecutionCompleted FaucetExecutionPhase = "Completed"
	// FaucetExecutionRejected is the Rejected value of FaucetExecutionPhase.
	FaucetExecutionRejected FaucetExecutionPhase = "Rejected"
	// FaucetExecutionExpired is the Expired value of FaucetExecutionPhase.
	FaucetExecutionExpired FaucetExecutionPhase = "Expired"
	// FaucetExecutionInconclusive is the Inconclusive value of FaucetExecutionPhase.
	FaucetExecutionInconclusive FaucetExecutionPhase = "Inconclusive"
)

// FaucetPhase names supported FaucetRequestStatus.Phase values. Unknown input still requires validation.
type FaucetPhase string

const (
	// FaucetPending is the Pending value of FaucetPhase.
	FaucetPending FaucetPhase = "Pending"
	// FaucetSubmitted is the Submitted value of FaucetPhase.
	FaucetSubmitted FaucetPhase = "Submitted"
	// FaucetCompleted is the Completed value of FaucetPhase.
	FaucetCompleted FaucetPhase = "Completed"
	// FaucetRejected is the Rejected value of FaucetPhase.
	FaucetRejected FaucetPhase = "Rejected"
	// FaucetInconclusive is the Inconclusive value of FaucetPhase.
	FaucetInconclusive FaucetPhase = "Inconclusive"
	// FaucetExpired is the Expired value of FaucetPhase.
	FaucetExpired FaucetPhase = "Expired"
)
