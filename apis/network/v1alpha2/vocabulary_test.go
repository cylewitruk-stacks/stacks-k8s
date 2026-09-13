package v1alpha2

import (
	"encoding/json"
	"testing"
)

// TestVocabularyWireValues pins serialized contracts independently of controller call sites.
func TestVocabularyWireValues(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value any
		wire  string
	}{
		{"NetworkOperationRunning", NetworkOperationRunning, `"Running"`},
		{"NetworkOperationPaused", NetworkOperationPaused, `"Paused"`},
		{"NetworkOperationStopped", NetworkOperationStopped, `"Stopped"`},
		{"NetworkPhaseUninitialized", NetworkPhaseUninitialized, `"Uninitialized"`},
		{"NetworkPhaseResolving", NetworkPhaseResolving, `"Resolving"`},
		{"NetworkPhaseResolutionError", NetworkPhaseResolutionError, `"ResolutionError"`},
		{"NetworkPhaseInitializing", NetworkPhaseInitializing, `"Initializing"`},
		{"NetworkPhaseRunning", NetworkPhaseRunning, `"Running"`},
		{"NetworkPhasePausing", NetworkPhasePausing, `"Pausing"`},
		{"NetworkPhasePaused", NetworkPhasePaused, `"Paused"`},
		{"NetworkPhaseStopping", NetworkPhaseStopping, `"Stopping"`},
		{"NetworkPhaseStopped", NetworkPhaseStopped, `"Stopped"`},
		{"NetworkPhaseDestroying", NetworkPhaseDestroying, `"Destroying"`},
		{"NetworkPhaseFailed", NetworkPhaseFailed, `"Failed"`},
		{"GatePrepareBitcoin", GatePrepareBitcoin, `"PrepareBitcoin"`},
		{"GateEnrollPoX4", GateEnrollPoX4, `"EnrollPoX4"`},
		{"GatePrepareNakamoto", GatePrepareNakamoto, `"PrepareNakamoto"`},
		{"GatePreparePoX5", GatePreparePoX5, `"PreparePoX5"`},
		{"GateEnrollPoX5", GateEnrollPoX5, `"EnrollPoX5"`},
		{"GatePrepareWaterfall", GatePrepareWaterfall, `"PrepareWaterfall"`},
		{"WorkerPhaseInactive", WorkerPhaseInactive, `"Inactive"`},
		{"WorkerPhaseActive", WorkerPhaseActive, `"Active"`},
		{"WorkerPhasePaused", WorkerPhasePaused, `"Paused"`},
		{"WorkerPhaseBlocked", WorkerPhaseBlocked, `"Blocked"`},
		{"WorkerPhaseDraining", WorkerPhaseDraining, `"Draining"`},
		{"WorkerPhaseSettled", WorkerPhaseSettled, `"Settled"`},
		{"WorkerPhaseUnsettled", WorkerPhaseUnsettled, `"Unsettled"`},
		{"WorkerPhaseUnknown", WorkerPhaseUnknown, `"Unknown"`},
		{"WorkerPhaseFailed", WorkerPhaseFailed, `"Failed"`},
		{"WorkerShutdownNetworkStopped", WorkerShutdownNetworkStopped, `"NetworkStopped"`},
		{"WorkerShutdownNetworkDeleting", WorkerShutdownNetworkDeleting, `"NetworkDeleting"`},
		{"WorkerShutdownParticipantRemoved", WorkerShutdownParticipantRemoved, `"ParticipantRemoved"`},
		{"WorkerDisposalSettled", WorkerDisposalSettled, `"Settled"`},
		{"WorkerDisposalUnsettled", WorkerDisposalUnsettled, `"Unsettled"`},
		{"PostconditionPoX4Enrollment", PostconditionPoX4Enrollment, `"PoX4Enrollment"`},
		{"PostconditionPoX4Extension", PostconditionPoX4Extension, `"PoX4Extension"`},
		{"PostconditionContractDeployment", PostconditionContractDeployment, `"ContractDeployment"`},
		{"PostconditionRegistryInitialization", PostconditionRegistryInitialization, `"RegistryInitialization"`},
		{"PostconditionManagerDeployment", PostconditionManagerDeployment, `"ManagerDeployment"`},
		{"PostconditionSignerRegistration", PostconditionSignerRegistration, `"SignerRegistration"`},
		{"PostconditionPoX5Enrollment", PostconditionPoX5Enrollment, `"PoX5Enrollment"`},
		{"PostconditionPoX5Extension", PostconditionPoX5Extension, `"PoX5Extension"`},
		{"LabelNetwork", LabelNetwork, `"network.stacks.org/network"`},
		{"LabelNetworkUID", LabelNetworkUID, `"network.stacks.org/network-uid"`},
		{"LabelParticipant", LabelParticipant, `"network.stacks.org/participant"`},
		{"LabelParticipantUID", LabelParticipantUID, `"network.stacks.org/participant-uid"`},
		{"LabelParticipantKind", LabelParticipantKind, `"network.stacks.org/participant-kind"`},
		{"LabelRole", LabelRole, `"network.stacks.org/role"`},
		{"LabelActor", LabelActor, `"network.stacks.org/actor"`},
		{"LabelSourceUID", LabelSourceUID, `"network.stacks.org/source-uid"`},
		{"LabelSourceKind", LabelSourceKind, `"network.stacks.org/source-kind"`},
		{"LabelManagedBy", LabelManagedBy, `"app.kubernetes.io/managed-by"`},
		{"AnnotationSourceName", AnnotationSourceName, `"network.stacks.org/source-name"`},
		{"AnnotationPolicyDigest", AnnotationPolicyDigest, `"network.stacks.org/policy-digest"`},
		{
			"AnnotationConfigurationDigest",
			AnnotationConfigurationDigest,
			`"network.stacks.org/configuration-digest"`,
		},
		{"ParticipantBitcoinNode", ParticipantBitcoinNode, `"BitcoinNode"`},
		{"ParticipantStacksNode", ParticipantStacksNode, `"StacksNode"`},
		{"ParticipantStacksSigner", ParticipantStacksSigner, `"StacksSigner"`},
		{"ParticipantStacksStacker", ParticipantStacksStacker, `"StacksStacker"`},
		{"ParticipantStacksFaucet", ParticipantStacksFaucet, `"StacksFaucet"`},
		{"ParticipantStacksContractSet", ParticipantStacksContractSet, `"StacksContractSet"`},
		{
			"ParticipantStacksTransactionProduction",
			ParticipantStacksTransactionProduction,
			`"StacksTransactionProduction"`,
		},
		{"ParticipantBitcoinBlockProduction", ParticipantBitcoinBlockProduction, `"BitcoinBlockProduction"`},
		{"ConditionAdmissionReady", ConditionAdmissionReady, `"AdmissionReady"`},
		{"ConditionBitcoinPrepared", ConditionBitcoinPrepared, `"BitcoinPrepared"`},
		{"ConditionConfigVerified", ConditionConfigVerified, `"ConfigVerified"`},
		{"ConditionFailed", ConditionFailed, `"Failed"`},
		{"ConditionInitialized", ConditionInitialized, `"Initialized"`},
		{"ConditionOperational", ConditionOperational, `"Operational"`},
		{"ConditionPlacementReady", ConditionPlacementReady, `"PlacementReady"`},
		{"ConditionPolicyDeferred", ConditionPolicyDeferred, `"PolicyDeferred"`},
		{"ConditionRunning", ConditionRunning, `"Running"`},
		{"ConditionWorkloadReady", ConditionWorkloadReady, `"WorkloadReady"`},
		{"ReasonAdmissionUnavailable", ReasonAdmissionUnavailable, `"AdmissionUnavailable"`},
		{"ReasonAwaitingFirstAnchor", ReasonAwaitingFirstAnchor, `"AwaitingFirstAnchor"`},
		{"ReasonBootstrapPending", ReasonBootstrapPending, `"BootstrapPending"`},
		{
			"ReasonContractObservationUnavailable",
			ReasonContractObservationUnavailable,
			`"ContractObservationUnavailable"`,
		},
		{"ReasonDeadlineOutcomeUnknown", ReasonDeadlineOutcomeUnknown, `"DeadlineOutcomeUnknown"`},
		{"ReasonDesiredPause", ReasonDesiredPause, `"DesiredPause"`},
		{"ReasonFrozenCeilingExceeded", ReasonFrozenCeilingExceeded, `"FrozenCeilingExceeded"`},
		{"ReasonIdentityUnavailable", ReasonIdentityUnavailable, `"IdentityUnavailable"`},
		{"ReasonInvalidAmount", ReasonInvalidAmount, `"InvalidAmount"`},
		{"ReasonInvalidConfiguration", ReasonInvalidConfiguration, `"InvalidConfiguration"`},
		{"ReasonNetworkDeleting", ReasonNetworkDeleting, `"NetworkDeleting"`},
		{"ReasonNetworkFailed", ReasonNetworkFailed, `"NetworkFailed"`},
		{"ReasonNetworkStopped", ReasonNetworkStopped, `"NetworkStopped"`},
		{"ReasonObservationUnavailable", ReasonObservationUnavailable, `"ObservationUnavailable"`},
		{"ReasonOwnershipConflict", ReasonOwnershipConflict, `"OwnershipConflict"`},
		{"ReasonParticipantRemoved", ReasonParticipantRemoved, `"ParticipantRemoved"`},
		{"ReasonPending", ReasonPending, `"Pending"`},
		{"ReasonPlacementError", ReasonPlacementError, `"PlacementError"`},
		{
			"ReasonPrepareBitcoinObservationDeadline",
			ReasonPrepareBitcoinObservationDeadline,
			`"PrepareBitcoinObservationDeadline"`,
		},
		{"ReasonPreparedChainRegressed", ReasonPreparedChainRegressed, `"PreparedChainRegressed"`},
		{"ReasonRequiresReplacement", ReasonRequiresReplacement, `"RequiresReplacement"`},
		{"ReasonRunning", ReasonRunning, `"Running"`},
		{"ReasonScheduled", ReasonScheduled, `"Scheduled"`},
		{"ReasonStopped", ReasonStopped, `"Stopped"`},
		{"ReasonStopping", ReasonStopping, `"Stopping"`},
		{"ReasonTerminated", ReasonTerminated, `"Terminated"`},
		{"ReasonTerminationUnknown", ReasonTerminationUnknown, `"TerminationUnknown"`},
		{"ReasonWorkerBound", ReasonWorkerBound, `"WorkerBound"`},
		{"ReasonWorkerIdentityLost", ReasonWorkerIdentityLost, `"WorkerIdentityLost"`},
		{"RoleActor", RoleActor, `"actor"`},
		{"RoleSupport", RoleSupport, `"support"`},
		{"ContainerBitcoin", ContainerBitcoin, `"bitcoin"`},
		{"ContainerStacksNode", ContainerStacksNode, `"stacks-node"`},
		{"ContainerStacksSigner", ContainerStacksSigner, `"stacks-signer"`},

		{"ManagedByNetworkOperator", ManagedByNetworkOperator, `"stacks-network-operator"`},
		{"NetworkLabelValue", NetworkLabelValue, `"network"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := json.Marshal(tc.value)
			if err != nil {
				t.Fatal(err)
			}
			if string(raw) != tc.wire {
				t.Fatalf("wire value %s, want %s", raw, tc.wire)
			}
		})
	}
}
