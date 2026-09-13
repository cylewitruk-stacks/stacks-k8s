package participantworkload

const (
	// miningEnabledAnnotation identifies runtime-owned metadata.
	miningEnabledAnnotation = "network.stacks.org/mining-enabled"
	// candidateConfigurationAnnotation identifies runtime-owned metadata.
	candidateConfigurationAnnotation = "network.stacks.org/candidate-config"
)

// actorPurpose binds the StatefulSet and ordinal Pod names to one participant incarnation.
const actorPurpose = "actor"
