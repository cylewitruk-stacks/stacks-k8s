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

const (
	// reasonAccountUnavailable reports AccountUnavailable.
	reasonAccountUnavailable = "AccountUnavailable"
	// reasonAuthorizationUnavailable reports AuthorizationUnavailable.
	reasonAuthorizationUnavailable = "AuthorizationUnavailable"
	// reasonAwaitingClarity3 reports AwaitingClarity3.
	reasonAwaitingClarity3 = "AwaitingClarity3"
	// reasonAwaitingContractPostcondition reports AwaitingContractPostcondition.
	reasonAwaitingContractPostcondition = "AwaitingContractPostcondition"
	// reasonAwaitingEpoch3 reports AwaitingEpoch3.
	reasonAwaitingEpoch3 = "AwaitingEpoch3"
	// reasonAwaitingNakamoto reports AwaitingNakamoto.
	reasonAwaitingNakamoto = "AwaitingNakamoto"
	// reasonAwaitingPoX4Unlock reports AwaitingPoX4Unlock.
	reasonAwaitingPoX4Unlock = "AwaitingPoX4Unlock"
	// reasonAwaitingPoX5 reports AwaitingPoX5.
	reasonAwaitingPoX5 = "AwaitingPoX5"
	// reasonAwaitingPoXPostcondition reports AwaitingPoXPostcondition.
	reasonAwaitingPoXPostcondition = "AwaitingPoXPostcondition"
	// reasonAwaitingRequiredCycle reports AwaitingRequiredCycle.
	reasonAwaitingRequiredCycle = "AwaitingRequiredCycle"
	// reasonAwaitingUnlock reports AwaitingUnlock.
	reasonAwaitingUnlock = "AwaitingUnlock"
	// reasonAwaitingUnlockForAmountChange reports AwaitingUnlockForAmountChange.
	reasonAwaitingUnlockForAmountChange = "AwaitingUnlockForAmountChange"
	// reasonBootstrapEnrollmentHeld reports BootstrapEnrollmentHeld.
	reasonBootstrapEnrollmentHeld = "BootstrapEnrollmentHeld"
	// reasonBootstrapWindowMissed reports BootstrapWindowMissed.
	reasonBootstrapWindowMissed = "BootstrapWindowMissed"
	// reasonChainIdentityMismatch reports ChainIdentityMismatch.
	reasonChainIdentityMismatch = "ChainIdentityMismatch"
	// reasonChainUnavailable reports ChainUnavailable.
	reasonChainUnavailable = "ChainUnavailable"
	// reasonConflict reports Conflict.
	reasonConflict = "Conflict"
	// reasonConstructionFailed reports ConstructionFailed.
	reasonConstructionFailed = "ConstructionFailed"
	// reasonContractOperationFailed reports ContractOperationFailed.
	reasonContractOperationFailed = "ContractOperationFailed"
	// reasonContractPostconditionObserved reports ContractPostconditionObserved.
	reasonContractPostconditionObserved = "ContractPostconditionObserved"
	// reasonContractSetObserved reports ContractSetObserved.
	reasonContractSetObserved = "ContractSetObserved"
	// reasonCounterExhausted reports CounterExhausted.
	reasonCounterExhausted = "CounterExhausted"
	// reasonDeadlineBeforeSend reports DeadlineBeforeSend.
	reasonDeadlineBeforeSend = "DeadlineBeforeSend"
	// reasonDeletedBeforeSend reports DeletedBeforeSend.
	reasonDeletedBeforeSend = "DeletedBeforeSend"
	// reasonDependenciesUnavailable reports DependenciesUnavailable.
	reasonDependenciesUnavailable = "DependenciesUnavailable"
	// reasonDraining reports Draining.
	reasonDraining = "Draining"
	// reasonEnrollmentStateMismatch reports EnrollmentStateMismatch.
	reasonEnrollmentStateMismatch = "EnrollmentStateMismatch"
	// reasonExecutionRejected reports ExecutionRejected.
	reasonExecutionRejected = "ExecutionRejected"
	// reasonExistingAccountLock reports ExistingAccountLock.
	reasonExistingAccountLock = "ExistingAccountLock"
	// reasonInclusionUnavailable reports InclusionUnavailable.
	reasonInclusionUnavailable = "InclusionUnavailable"
	// reasonInsufficientFunds reports InsufficientFunds.
	reasonInsufficientFunds = "InsufficientFunds"
	// reasonInvalidBootstrapWindow reports InvalidBootstrapWindow.
	reasonInvalidBootstrapWindow = "InvalidBootstrapWindow"
	// reasonInvalidContractPolicy reports InvalidContractPolicy.
	reasonInvalidContractPolicy = "InvalidContractPolicy"
	// reasonInvalidHolder reports InvalidHolder.
	reasonInvalidHolder = "InvalidHolder"
	// reasonInvalidInputs reports InvalidInputs.
	reasonInvalidInputs = "InvalidInputs"
	// reasonInvalidPoXCycle reports InvalidPoXCycle.
	reasonInvalidPoXCycle = "InvalidPoXCycle"
	// reasonInvalidPolicy reports InvalidPolicy.
	reasonInvalidPolicy = "InvalidPolicy"
	// reasonInvalidRequestAmount reports InvalidRequestAmount.
	reasonInvalidRequestAmount = "InvalidRequestAmount"
	// reasonInvalidRequestDeadline reports InvalidRequestDeadline.
	reasonInvalidRequestDeadline = "InvalidRequestDeadline"
	// reasonInvalidRequestFee reports InvalidRequestFee.
	reasonInvalidRequestFee = "InvalidRequestFee"
	// reasonNonceMismatch reports NonceMismatch.
	reasonNonceMismatch = "NonceMismatch"
	// reasonObservationMismatch reports ObservationMismatch.
	reasonObservationMismatch = "ObservationMismatch"
	// reasonPoX4LockExpired reports PoX4LockExpired.
	reasonPoX4LockExpired = "PoX4LockExpired"
	// reasonPoX4OperationFailed reports PoX4OperationFailed.
	reasonPoX4OperationFailed = "PoX4OperationFailed"
	// reasonPoX5OperationFailed reports PoX5OperationFailed.
	reasonPoX5OperationFailed = "PoX5OperationFailed"
	// reasonPoXObservationUnavailable reports PoXObservationUnavailable.
	reasonPoXObservationUnavailable = "PoXObservationUnavailable"
	// reasonPolicyUnavailable reports PolicyUnavailable.
	reasonPolicyUnavailable = "PolicyUnavailable"
	// reasonRegistryObservationUnavailable reports RegistryObservationUnavailable.
	reasonRegistryObservationUnavailable = "RegistryObservationUnavailable"
	// reasonRejectionBackoff reports RejectionBackoff.
	reasonRejectionBackoff = "RejectionBackoff"
	// reasonRequestAbsent reports RequestAbsent.
	reasonRequestAbsent = "RequestAbsent"
	// reasonRequestAlreadySettled reports RequestAlreadySettled.
	reasonRequestAlreadySettled = "RequestAlreadySettled"
	// reasonRequestBindingUnavailable reports RequestBindingUnavailable.
	reasonRequestBindingUnavailable = "RequestBindingUnavailable"
	// reasonRequestExpired reports RequestExpired.
	reasonRequestExpired = "RequestExpired"
	// reasonRequestListUnavailable reports RequestListUnavailable.
	reasonRequestListUnavailable = "RequestListUnavailable"
	// reasonRequestPublicationUnavailable reports RequestPublicationUnavailable.
	reasonRequestPublicationUnavailable = "RequestPublicationUnavailable"
	// reasonRequestReadUnavailable reports RequestReadUnavailable.
	reasonRequestReadUnavailable = "RequestReadUnavailable"
	// reasonRequestStateLost reports RequestStateLost.
	reasonRequestStateLost = "RequestStateLost"
	// reasonStakeBelowThreshold reports StakeBelowThreshold.
	reasonStakeBelowThreshold = "StakeBelowThreshold"
	// reasonSubmissionUncertain reports SubmissionUncertain.
	reasonSubmissionUncertain = "SubmissionUncertain"
	// reasonWaitingRequests reports WaitingRequests.
	reasonWaitingRequests = "WaitingRequests"
	// reasonWorkerIdentityUnavailable reports WorkerIdentityUnavailable.
	reasonWorkerIdentityUnavailable = "WorkerIdentityUnavailable"
	// reasonWorkerProcessChanged reports WorkerProcessChanged.
	reasonWorkerProcessChanged = "WorkerProcessChanged"
	// reasonWorkerStoppedBeforeSend reports WorkerStoppedBeforeSend.
	reasonWorkerStoppedBeforeSend = "WorkerStoppedBeforeSend"
)
