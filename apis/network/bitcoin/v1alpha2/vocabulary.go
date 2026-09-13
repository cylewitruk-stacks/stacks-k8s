package v1alpha2

// CadenceMode names supported Cadence.Mode values. Unknown input still requires validation.
type CadenceMode string

const (
	// CadenceFixed is the Fixed value of CadenceMode.
	CadenceFixed CadenceMode = "Fixed"
	// CadenceUniform is the Uniform value of CadenceMode.
	CadenceUniform CadenceMode = "Uniform"
)

// OfferMode names supported BitcoinBlockOffer.Mode values. Unknown input still requires validation.
type OfferMode string

const (
	// OfferBootstrap is the Bootstrap value of OfferMode.
	OfferBootstrap OfferMode = "Bootstrap"
	// OfferBaseline is the Baseline value of OfferMode.
	OfferBaseline OfferMode = "Baseline"
)

// RPCMethod names supported BitcoinArmedRPC.Method values. Unknown input still requires validation.
type RPCMethod string

const (
	// RPCCreateWallet is the CreateWallet value of RPCMethod.
	RPCCreateWallet RPCMethod = "CreateWallet"
	// RPCLoadWallet is the LoadWallet value of RPCMethod.
	RPCLoadWallet RPCMethod = "LoadWallet"
	// RPCImportDescriptor is the ImportDescriptor value of RPCMethod.
	RPCImportDescriptor RPCMethod = "ImportDescriptor"
	// RPCUnloadWallet is the UnloadWallet value of RPCMethod.
	RPCUnloadWallet RPCMethod = "UnloadWallet"
	// RPCGenerate is the Generate value of RPCMethod.
	RPCGenerate RPCMethod = "Generate"
	// RPCInvalidateBlock is the InvalidateBlock value of RPCMethod.
	RPCInvalidateBlock RPCMethod = "InvalidateBlock"
	// RPCReconsiderBlock is the ReconsiderBlock value of RPCMethod.
	RPCReconsiderBlock RPCMethod = "ReconsiderBlock"
)

// ExecutionPhase names supported BitcoinExecutionStatus.Phase values. Unknown input still requires validation.
type ExecutionPhase string

const (
	// ExecutionIdle is the Idle value of ExecutionPhase.
	ExecutionIdle ExecutionPhase = "Idle"
	// ExecutionArmed is the Armed value of ExecutionPhase.
	ExecutionArmed ExecutionPhase = "Armed"
	// ExecutionBlocked is the Blocked value of ExecutionPhase.
	ExecutionBlocked ExecutionPhase = "Blocked"
	// ExecutionAbandoned is the Abandoned value of ExecutionPhase.
	ExecutionAbandoned ExecutionPhase = "Abandoned"
)

// InitializationPhase names supported BitcoinInitializationStatus.Phase values. Unknown input still requires
// validation.
type InitializationPhase string

const (
	// InitializationWaiting is the Waiting value of InitializationPhase.
	InitializationWaiting InitializationPhase = "Waiting"
	// InitializationPreparing is the Preparing value of InitializationPhase.
	InitializationPreparing InitializationPhase = "Preparing"
	// InitializationPaused is the Paused value of InitializationPhase.
	InitializationPaused InitializationPhase = "Paused"
	// InitializationHeld is the Held value of InitializationPhase.
	InitializationHeld InitializationPhase = "Held"
	// InitializationBlocked is the Blocked value of InitializationPhase.
	InitializationBlocked InitializationPhase = "Blocked"
	// InitializationAbandoned is the Abandoned value of InitializationPhase.
	InitializationAbandoned InitializationPhase = "Abandoned"
)

// DrainOutcome names supported BitcoinDrainStatus.Outcome values. Unknown input still requires validation.
type DrainOutcome string

const (
	// DrainDrained is the Drained value of DrainOutcome.
	DrainDrained DrainOutcome = "Drained"
	// DrainUncertain is the Uncertain value of DrainOutcome.
	DrainUncertain DrainOutcome = "Uncertain"
)

// ControlOperation names supported BitcoinControlAcknowledgement.Operation values. Unknown input still
// requires validation.
type ControlOperation string

const (
	// ControlPaused is the Paused value of ControlOperation.
	ControlPaused ControlOperation = "Paused"
)

// BaselineStage names supported BitcoinBaselineStatus.Stage values. Unknown input still requires validation.
type BaselineStage string

const (
	// BaselineSelected is the Selected value of BaselineStage.
	BaselineSelected BaselineStage = "Selected"
	// BaselineOffered is the Offered value of BaselineStage.
	BaselineOffered BaselineStage = "Offered"
	// BaselineAssigned is the Assigned value of BaselineStage.
	BaselineAssigned BaselineStage = "Assigned"
	// BaselineSkipped is the Skipped value of BaselineStage.
	BaselineSkipped BaselineStage = "Skipped"
	// BaselineUnassigned is the Unassigned value of BaselineStage.
	BaselineUnassigned BaselineStage = "Unassigned"
)

// OverridePhase names supported BitcoinBlockScheduleOverrideStatus.Phase values. Unknown input still requires
// validation.
type OverridePhase string

const (
	// OverridePending is the Pending value of OverridePhase.
	OverridePending OverridePhase = "Pending"
	// OverrideActive is the Active value of OverridePhase.
	OverrideActive OverridePhase = "Active"
	// OverrideCompleted is the Completed value of OverridePhase.
	OverrideCompleted OverridePhase = "Completed"
	// OverrideExpired is the Expired value of OverridePhase.
	OverrideExpired OverridePhase = "Expired"
	// OverrideCancelled is the Cancelled value of OverridePhase.
	OverrideCancelled OverridePhase = "Cancelled"
)
