package v1alpha2

// Phase names supported BitcoinBlockGenerationStatus.Phase values. Unknown input still requires validation.
type Phase string

const (
	// PhasePending is the Pending value of Phase.
	PhasePending Phase = "Pending"
	// PhaseAdmitted is the Admitted value of Phase.
	PhaseAdmitted Phase = "Admitted"
	// PhaseActive is the Active value of Phase.
	PhaseActive Phase = "Active"
	// PhaseRecovering is the Recovering value of Phase.
	PhaseRecovering Phase = "Recovering"
	// PhaseCompleted is the Completed value of Phase.
	PhaseCompleted Phase = "Completed"
	// PhaseRecovered is the Recovered value of Phase.
	PhaseRecovered Phase = "Recovered"
	// PhaseFailed is the Failed value of Phase.
	PhaseFailed Phase = "Failed"
	// PhaseInconclusive is the Inconclusive value of Phase.
	PhaseInconclusive Phase = "Inconclusive"
)

// CadenceMode names supported GenerationCadence.Mode values. Unknown input still requires validation.
type CadenceMode string

const (
	// CadenceImmediate is the Immediate value of CadenceMode.
	CadenceImmediate CadenceMode = "Immediate"
	// CadenceFixed is the Fixed value of CadenceMode.
	CadenceFixed CadenceMode = "Fixed"
	// CadenceUniform is the Uniform value of CadenceMode.
	CadenceUniform CadenceMode = "Uniform"
	// CadenceExplicit is the Explicit value of CadenceMode.
	CadenceExplicit CadenceMode = "Explicit"
)
