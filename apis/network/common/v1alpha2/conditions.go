package v1alpha2

const (
	// ConditionResolved identifies the Resolved status condition.
	ConditionResolved = "Resolved"
)

// Native termination reasons have no exported constants in k8s.io/api.
const (
	// ReasonContainerStatusUnknown prevents treating lost container status as termination evidence.
	ReasonContainerStatusUnknown = "ContainerStatusUnknown"
	// ReasonNodeLost identifies loss of kubelet contact, not confirmed process termination.
	ReasonNodeLost = "NodeLost"
)
