package v1alpha2

// NetworkOperation names supported StacksNetworkSpec.Operation values. Unknown input still requires validation.
type NetworkOperation string

const (
	// NetworkOperationRunning is the Running value of NetworkOperation.
	NetworkOperationRunning NetworkOperation = "Running"
	// NetworkOperationPaused is the Paused value of NetworkOperation.
	NetworkOperationPaused NetworkOperation = "Paused"
	// NetworkOperationStopped is the Stopped value of NetworkOperation.
	NetworkOperationStopped NetworkOperation = "Stopped"
)

// NetworkPhase names supported StacksNetworkStatus.Phase values. Unknown input still requires validation.
type NetworkPhase string

const (
	// NetworkPhaseUninitialized is the Uninitialized value of NetworkPhase.
	NetworkPhaseUninitialized NetworkPhase = "Uninitialized"
	// NetworkPhaseResolving is the Resolving value of NetworkPhase.
	NetworkPhaseResolving NetworkPhase = "Resolving"
	// NetworkPhaseResolutionError is the ResolutionError value of NetworkPhase.
	NetworkPhaseResolutionError NetworkPhase = "ResolutionError"
	// NetworkPhaseInitializing is the Initializing value of NetworkPhase.
	NetworkPhaseInitializing NetworkPhase = "Initializing"
	// NetworkPhaseRunning is the Running value of NetworkPhase.
	NetworkPhaseRunning NetworkPhase = "Running"
	// NetworkPhasePausing is the Pausing value of NetworkPhase.
	NetworkPhasePausing NetworkPhase = "Pausing"
	// NetworkPhasePaused is the Paused value of NetworkPhase.
	NetworkPhasePaused NetworkPhase = "Paused"
	// NetworkPhaseStopping is the Stopping value of NetworkPhase.
	NetworkPhaseStopping NetworkPhase = "Stopping"
	// NetworkPhaseStopped is the Stopped value of NetworkPhase.
	NetworkPhaseStopped NetworkPhase = "Stopped"
	// NetworkPhaseDestroying is the Destroying value of NetworkPhase.
	NetworkPhaseDestroying NetworkPhase = "Destroying"
	// NetworkPhaseFailed is the Failed value of NetworkPhase.
	NetworkPhaseFailed NetworkPhase = "Failed"
)

// GateName names supported Gate.Name values. Unknown input still requires validation.
type GateName string

const (
	// GatePrepareBitcoin is the PrepareBitcoin value of GateName.
	GatePrepareBitcoin GateName = "PrepareBitcoin"
	// GateEnrollPoX4 is the EnrollPoX4 value of GateName.
	GateEnrollPoX4 GateName = "EnrollPoX4"
	// GatePrepareNakamoto is the PrepareNakamoto value of GateName.
	GatePrepareNakamoto GateName = "PrepareNakamoto"
	// GatePreparePoX5 is the PreparePoX5 value of GateName.
	GatePreparePoX5 GateName = "PreparePoX5"
	// GateEnrollPoX5 is the EnrollPoX5 value of GateName.
	GateEnrollPoX5 GateName = "EnrollPoX5"
	// GatePrepareWaterfall is the PrepareWaterfall value of GateName.
	GatePrepareWaterfall GateName = "PrepareWaterfall"
)

// WorkerPhase names supported WorkerExecutionStatus.Phase values. Unknown input still requires validation.
type WorkerPhase string

const (
	// WorkerPhaseInactive is the Inactive value of WorkerPhase.
	WorkerPhaseInactive WorkerPhase = "Inactive"
	// WorkerPhaseActive is the Active value of WorkerPhase.
	WorkerPhaseActive WorkerPhase = "Active"
	// WorkerPhasePaused is the Paused value of WorkerPhase.
	WorkerPhasePaused WorkerPhase = "Paused"
	// WorkerPhaseBlocked is the Blocked value of WorkerPhase.
	WorkerPhaseBlocked WorkerPhase = "Blocked"
	// WorkerPhaseDraining is the Draining value of WorkerPhase.
	WorkerPhaseDraining WorkerPhase = "Draining"
	// WorkerPhaseSettled is the Settled value of WorkerPhase.
	WorkerPhaseSettled WorkerPhase = "Settled"
	// WorkerPhaseUnsettled is the Unsettled value of WorkerPhase.
	WorkerPhaseUnsettled WorkerPhase = "Unsettled"
	// WorkerPhaseUnknown is the Unknown value of WorkerPhase.
	WorkerPhaseUnknown WorkerPhase = "Unknown"
	// WorkerPhaseFailed is the Failed value of WorkerPhase.
	WorkerPhaseFailed WorkerPhase = "Failed"
)

// WorkerShutdownReason names supported WorkerShutdown.Reason values. Unknown input still requires validation.
type WorkerShutdownReason string

const (
	// WorkerShutdownNetworkStopped is the NetworkStopped value of WorkerShutdownReason.
	WorkerShutdownNetworkStopped WorkerShutdownReason = "NetworkStopped"
	// WorkerShutdownNetworkDeleting is the NetworkDeleting value of WorkerShutdownReason.
	WorkerShutdownNetworkDeleting WorkerShutdownReason = "NetworkDeleting"
	// WorkerShutdownParticipantRemoved is the ParticipantRemoved value of WorkerShutdownReason.
	WorkerShutdownParticipantRemoved WorkerShutdownReason = "ParticipantRemoved"
)

// WorkerDisposalOutcome names supported WorkerDisposal.Outcome values. Unknown input still requires validation.
type WorkerDisposalOutcome string

const (
	// WorkerDisposalSettled is the Settled value of WorkerDisposalOutcome.
	WorkerDisposalSettled WorkerDisposalOutcome = "Settled"
	// WorkerDisposalUnsettled is the Unsettled value of WorkerDisposalOutcome.
	WorkerDisposalUnsettled WorkerDisposalOutcome = "Unsettled"
)

// PostconditionKind names supported TransactionPostcondition.Kind values. Unknown input still requires validation.
type PostconditionKind string

const (
	// PostconditionPoX4Enrollment is the PoX4Enrollment value of PostconditionKind.
	PostconditionPoX4Enrollment PostconditionKind = "PoX4Enrollment"
	// PostconditionPoX4Extension is the PoX4Extension value of PostconditionKind.
	PostconditionPoX4Extension PostconditionKind = "PoX4Extension"
	// PostconditionContractDeployment is the ContractDeployment value of PostconditionKind.
	PostconditionContractDeployment PostconditionKind = "ContractDeployment"
	// PostconditionRegistryInitialization is the RegistryInitialization value of PostconditionKind.
	PostconditionRegistryInitialization PostconditionKind = "RegistryInitialization"
	// PostconditionManagerDeployment is the ManagerDeployment value of PostconditionKind.
	PostconditionManagerDeployment PostconditionKind = "ManagerDeployment"
	// PostconditionSignerRegistration is the SignerRegistration value of PostconditionKind.
	PostconditionSignerRegistration PostconditionKind = "SignerRegistration"
	// PostconditionPoX5Enrollment is the PoX5Enrollment value of PostconditionKind.
	PostconditionPoX5Enrollment PostconditionKind = "PoX5Enrollment"
	// PostconditionPoX5Extension is the PoX5Extension value of PostconditionKind.
	PostconditionPoX5Extension PostconditionKind = "PoX5Extension"
)
