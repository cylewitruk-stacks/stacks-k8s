package v1alpha2

// Shared action reasons coordinate execution and lifecycle accounting.
const (
	// ReasonIdentityDiverged identifies the IdentityDiverged outcome.
	ReasonIdentityDiverged = "IdentityDiverged"
	// ReasonEffectUncertain identifies the EffectUncertain outcome.
	ReasonEffectUncertain = "EffectUncertain"
	// ReasonCleanupDeadlineExceeded identifies the CleanupDeadlineExceeded outcome.
	ReasonCleanupDeadlineExceeded = "CleanupDeadlineExceeded"
	// ReasonActionCancelled identifies the ActionCancelled outcome.
	ReasonActionCancelled = "ActionCancelled"
	// ReasonActionDeadlineExceeded identifies the ActionDeadlineExceeded outcome.
	ReasonActionDeadlineExceeded = "ActionDeadlineExceeded"
	// ReasonMechanismFailed identifies the MechanismFailed outcome.
	ReasonMechanismFailed = "MechanismFailed"
)
