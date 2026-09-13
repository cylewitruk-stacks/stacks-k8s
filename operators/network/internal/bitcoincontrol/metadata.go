package bitcoincontrol

const (
	// workerRoleLabel identifies runtime-owned metadata.
	workerRoleLabel = "network.stacks.org/worker-role"
)

const (
	// workerRoleControl identifies Bitcoin control Deployments and their Pods.
	workerRoleControl = "bitcoin-control"
	// controlInputAnnotation binds a control Pod template to its public input digest.
	controlInputAnnotation = "network.stacks.org/control-input"
	// rpcVolumeName pairs the credential volume with its read-only mount.
	rpcVolumeName = "rpc"
)
