package v1alpha2

// CleanupFinalizer retains cleanup and unresolved execution obligations.
const CleanupFinalizer = "actions.stacks.org/action-cleanup"

// IsTerminalPhase reports a frozen action outcome in the common lifecycle.
func IsTerminalPhase(phase Phase) bool {
	return phase == PhaseCompleted || phase == PhaseRecovered || phase == PhaseFailed || phase == PhaseInconclusive
}

// CorrelationIDLabel associates an action with external experiment evidence.
const CorrelationIDLabel = "actions.stacks.org/correlation-id"
