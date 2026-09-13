package v1alpha2

// Public workload metadata binds observable resources to their network and participant.
const (
	// LabelNetwork identifies the network.stacks.org/network metadata key.
	LabelNetwork = "network.stacks.org/network"
	// LabelNetworkUID identifies the network.stacks.org/network-uid metadata key.
	LabelNetworkUID = "network.stacks.org/network-uid"
	// LabelParticipant identifies the network.stacks.org/participant metadata key.
	LabelParticipant = "network.stacks.org/participant"
	// LabelParticipantUID identifies the network.stacks.org/participant-uid metadata key.
	LabelParticipantUID = "network.stacks.org/participant-uid"
	// LabelParticipantKind identifies the network.stacks.org/participant-kind metadata key.
	LabelParticipantKind = "network.stacks.org/participant-kind"
	// LabelRole identifies the network.stacks.org/role metadata key.
	LabelRole = "network.stacks.org/role"
	// LabelActor identifies the network.stacks.org/actor metadata key.
	LabelActor = "network.stacks.org/actor"
	// LabelSourceUID identifies the network.stacks.org/source-uid metadata key.
	LabelSourceUID = "network.stacks.org/source-uid"
	// LabelSourceKind identifies the network.stacks.org/source-kind metadata key.
	LabelSourceKind = "network.stacks.org/source-kind"
	// LabelManagedBy identifies the app.kubernetes.io/managed-by metadata key.
	LabelManagedBy = "app.kubernetes.io/managed-by"
	// AnnotationSourceName identifies the network.stacks.org/source-name metadata key.
	AnnotationSourceName = "network.stacks.org/source-name"
	// AnnotationPolicyDigest identifies the network.stacks.org/policy-digest metadata key.
	AnnotationPolicyDigest = "network.stacks.org/policy-digest"
	// AnnotationConfigurationDigest identifies the network.stacks.org/configuration-digest metadata key.
	AnnotationConfigurationDigest = "network.stacks.org/configuration-digest"
)
