package bitcoincontrol

const (
	// reasonActivated reports Activated.
	reasonActivated = "Activated"
	// reasonActivationWithdrawn reports ActivationWithdrawn.
	reasonActivationWithdrawn = "ActivationWithdrawn"
	// reasonAwaitingEnrollmentDemand reports AwaitingEnrollmentDemand.
	reasonAwaitingEnrollmentDemand = "AwaitingEnrollmentDemand"
	// reasonAwaitingPostReceiptObservation reports a retained pre-generation chain sample.
	reasonAwaitingPostReceiptObservation = "AwaitingPostReceiptObservation"
	// reasonBaselineCadenceArmed reports BaselineCadenceArmed.
	reasonBaselineCadenceArmed = "BaselineCadenceArmed"
	// reasonBaselineCadenceWaiting reports BaselineCadenceWaiting.
	reasonBaselineCadenceWaiting = "BaselineCadenceWaiting"
	// reasonBaselineInputsUnavailable reports BaselineInputsUnavailable.
	reasonBaselineInputsUnavailable = "BaselineInputsUnavailable"
	// reasonBaselineOfferAssigned reports BaselineOfferAssigned.
	reasonBaselineOfferAssigned = "BaselineOfferAssigned"
	// reasonBaselineOfferCommitted reports BaselineOfferCommitted.
	reasonBaselineOfferCommitted = "BaselineOfferCommitted"
	// reasonBaselinePolicyAdopted reports BaselinePolicyAdopted.
	reasonBaselinePolicyAdopted = "BaselinePolicyAdopted"
	// reasonBaselineTargetSelected reports BaselineTargetSelected.
	reasonBaselineTargetSelected = "BaselineTargetSelected"
	// reasonBootstrapGateAuthorityUnavailable reports BootstrapGateAuthorityUnavailable.
	reasonBootstrapGateAuthorityUnavailable = "BootstrapGateAuthorityUnavailable"
	// reasonCadenceArmed reports CadenceArmed.
	reasonCadenceArmed = "CadenceArmed"
	// reasonCadenceUnavailable reports CadenceUnavailable.
	reasonCadenceUnavailable = "CadenceUnavailable"
	// reasonCadenceWaiting reports CadenceWaiting.
	reasonCadenceWaiting = "CadenceWaiting"
	// reasonCancellationRequested reports CancellationRequested.
	reasonCancellationRequested = "CancellationRequested"
	// reasonCancelledBeforeActivation reports CancelledBeforeActivation.
	reasonCancelledBeforeActivation = "CancelledBeforeActivation"
	// reasonCapabilityDisabled reports CapabilityDisabled.
	reasonCapabilityDisabled = "CapabilityDisabled"
	// reasonCommittedOfferUnavailable reports CommittedOfferUnavailable.
	reasonCommittedOfferUnavailable = "CommittedOfferUnavailable"
	// reasonCoreViewsDiverged reports CoreViewsDiverged.
	reasonCoreViewsDiverged = "CoreViewsDiverged"
	// reasonDeadlineExceeded reports DeadlineExceeded.
	reasonDeadlineExceeded = "DeadlineExceeded"
	// reasonDesiredStop reports DesiredStop.
	reasonDesiredStop = "DesiredStop"
	// reasonDurationElapsed reports DurationElapsed.
	reasonDurationElapsed = "DurationElapsed"
	// reasonExecutionBindingUnavailable reports ExecutionBindingUnavailable.
	reasonExecutionBindingUnavailable = "ExecutionBindingUnavailable"
	// reasonExecutionRecordReplaced reports ExecutionRecordReplaced.
	reasonExecutionRecordReplaced = "ExecutionRecordReplaced"
	// reasonExecutionRecordUnavailable reports ExecutionRecordUnavailable.
	reasonExecutionRecordUnavailable = "ExecutionRecordUnavailable"
	// reasonExternalChainMovement reports ExternalChainMovement.
	reasonExternalChainMovement = "ExternalChainMovement"
	// reasonFrozenGateReached reports FrozenGateReached.
	reasonFrozenGateReached = "FrozenGateReached"
	// reasonInitializationCompletionUnavailable reports InitializationCompletionUnavailable.
	reasonInitializationCompletionUnavailable = "InitializationCompletionUnavailable"
	// reasonInitializationTargetUnavailable reports InitializationTargetUnavailable.
	reasonInitializationTargetUnavailable = "InitializationTargetUnavailable"
	// reasonMinerMaturityUnconfirmed reports MinerMaturityUnconfirmed.
	reasonMinerMaturityUnconfirmed = "MinerMaturityUnconfirmed"
	// reasonNetworkPaused reports NetworkPaused.
	reasonNetworkPaused = "NetworkPaused"
	// reasonNextGateNotImplemented reports NextGateNotImplemented.
	reasonNextGateNotImplemented = "NextGateNotImplemented"
	// reasonNodeObservationIdentityChanged reports NodeObservationIdentityChanged.
	reasonNodeObservationIdentityChanged = "NodeObservationIdentityChanged"
	// reasonNodeObservationUnavailable reports NodeObservationUnavailable.
	reasonNodeObservationUnavailable = "NodeObservationUnavailable"
	// reasonOpportunityCounterExhausted reports OpportunityCounterExhausted.
	reasonOpportunityCounterExhausted = "OpportunityCounterExhausted"
	// reasonOpportunityExpiredBeforeAssignment reports OpportunityExpiredBeforeAssignment.
	reasonOpportunityExpiredBeforeAssignment = "OpportunityExpiredBeforeAssignment"
	// reasonOpportunityOutstanding reports OpportunityOutstanding.
	reasonOpportunityOutstanding = "OpportunityOutstanding"
	// reasonOpportunitySelected reports OpportunitySelected.
	reasonOpportunitySelected = "OpportunitySelected"
	// reasonPendingDeadline reports PendingDeadline.
	reasonPendingDeadline = "PendingDeadline"
	// reasonProductionIdentityUnavailable reports ProductionIdentityUnavailable.
	reasonProductionIdentityUnavailable = "ProductionIdentityUnavailable"
	// reasonProductionSourceUnavailable reports ProductionSourceUnavailable.
	reasonProductionSourceUnavailable = "ProductionSourceUnavailable"
	// reasonRPCOutstanding reports RPCOutstanding.
	reasonRPCOutstanding = "RPCOutstanding"
	// reasonReplacementNotCanonical reports ReplacementNotCanonical.
	reasonReplacementNotCanonical = "ReplacementNotCanonical"
	// reasonReplacementVerificationFailed reports ReplacementVerificationFailed.
	reasonReplacementVerificationFailed = "ReplacementVerificationFailed"
	// reasonReplacementWorkInsufficient reports ReplacementWorkInsufficient.
	reasonReplacementWorkInsufficient = "ReplacementWorkInsufficient"
	// reasonSchedulerNotEnrolled reports SchedulerNotEnrolled.
	reasonSchedulerNotEnrolled = "SchedulerNotEnrolled"
	// reasonSchedulerWaiting reports SchedulerWaiting.
	reasonSchedulerWaiting = "SchedulerWaiting"
	// reasonSelectedAdmissionUnavailable reports SelectedAdmissionUnavailable.
	reasonSelectedAdmissionUnavailable = "SelectedAdmissionUnavailable"
	// reasonSelectedDependencyUnavailable reports SelectedDependencyUnavailable.
	reasonSelectedDependencyUnavailable = "SelectedDependencyUnavailable"
	// reasonSelectedExecutionUnavailable reports SelectedExecutionUnavailable.
	reasonSelectedExecutionUnavailable = "SelectedExecutionUnavailable"
	// reasonSelectedObservationUnavailable reports SelectedObservationUnavailable.
	reasonSelectedObservationUnavailable = "SelectedObservationUnavailable"
	// reasonSelectedTargetBusy reports SelectedTargetBusy.
	reasonSelectedTargetBusy = "SelectedTargetBusy"
	// reasonSelectedTargetNotReady reports SelectedTargetNotReady.
	reasonSelectedTargetNotReady = "SelectedTargetNotReady"
	// reasonSelectedTargetUnavailable reports SelectedTargetUnavailable.
	reasonSelectedTargetUnavailable = "SelectedTargetUnavailable"
	// reasonSelectedTargetUnverified reports SelectedTargetUnverified.
	reasonSelectedTargetUnverified = "SelectedTargetUnverified"
	// reasonSelectedWalletsPreparing reports SelectedWalletsPreparing.
	reasonSelectedWalletsPreparing = "SelectedWalletsPreparing"
	// reasonStacksReadinessUnavailable reports StacksReadinessUnavailable.
	reasonStacksReadinessUnavailable = "StacksReadinessUnavailable"
	// reasonTargetSelectionUnavailable reports TargetSelectionUnavailable.
	reasonTargetSelectionUnavailable = "TargetSelectionUnavailable"
	// reasonWaitingForActivation reports WaitingForActivation.
	reasonWaitingForActivation = "WaitingForActivation"
	// reasonWalletIdentityUnavailable reports WalletIdentityUnavailable.
	reasonWalletIdentityUnavailable = "WalletIdentityUnavailable"
	// reasonWalletInventoryIncomplete reports WalletInventoryIncomplete.
	reasonWalletInventoryIncomplete = "WalletInventoryIncomplete"
	// reasonWalletsPreparing reports WalletsPreparing.
	reasonWalletsPreparing = "WalletsPreparing"
)
