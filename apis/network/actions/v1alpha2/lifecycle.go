package v1alpha2

// CleanupFinalizer retains cleanup and unresolved execution obligations.
const CleanupFinalizer = "actions.stacks.org/action-cleanup"

// IsTerminalPhase reports a frozen action outcome in the common lifecycle.
func IsTerminalPhase(phase string) bool {
	return phase == "Completed" || phase == "Recovered" || phase == "Failed" || phase == "Inconclusive"
}
