package stacksoperation

// Execution reasons shared by nonce observation and role readiness decisions.
const (
	// reasonPaused identifies the Paused execution outcome.
	reasonPaused = "Paused"
	// reasonStateObserved identifies the StateObserved execution outcome.
	reasonStateObserved = "StateObserved"
	// reasonIncluded identifies the Included execution outcome.
	reasonIncluded = "Included"
	// reasonIdle identifies the Idle execution outcome.
	reasonIdle = "Idle"
	// reasonPoX4EnrollmentObserved identifies the PoX4EnrollmentObserved execution outcome.
	reasonPoX4EnrollmentObserved = "PoX4EnrollmentObserved"
	// reasonPoX5EnrollmentObserved identifies the PoX5EnrollmentObserved execution outcome.
	reasonPoX5EnrollmentObserved = "PoX5EnrollmentObserved"
	// reasonPoX4InclusionObservedAtTransition identifies the PoX4InclusionObservedAtTransition execution outcome.
	reasonPoX4InclusionObservedAtTransition = "PoX4InclusionObservedAtTransition"
	// reasonPoX5PostconditionObserved identifies the PoX5PostconditionObserved execution outcome.
	reasonPoX5PostconditionObserved = "PoX5PostconditionObserved"
	// reasonAwaitingMaintenanceWindow identifies the AwaitingMaintenanceWindow execution outcome.
	reasonAwaitingMaintenanceWindow = "AwaitingMaintenanceWindow"
	// reasonWaitingCadence identifies the WaitingCadence execution outcome.
	reasonWaitingCadence = "WaitingCadence"
	// reasonAccepted identifies the Accepted execution outcome.
	reasonAccepted = "Accepted"
	// reasonAwaitingInclusion identifies the AwaitingInclusion execution outcome.
	reasonAwaitingInclusion = "AwaitingInclusion"
)
