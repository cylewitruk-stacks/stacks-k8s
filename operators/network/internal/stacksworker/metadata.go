package stacksworker

const (
	// workloadLabel identifies runtime-owned metadata.
	workloadLabel = "network.stacks.org/workload"
	// profileJSONAnnotation identifies runtime-owned metadata.
	profileJSONAnnotation = "network.stacks.org/worker-profile-json"
)

// workloadStacksWorker selects the scoped Stacks management process Pods.
const workloadStacksWorker = "stacks-worker"
