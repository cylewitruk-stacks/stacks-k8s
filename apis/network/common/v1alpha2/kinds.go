package v1alpha2

// Kubernetes kind names used by public identity bindings. The upstream API
// packages do not expose kind-name constants; these do not restrict Binding.Kind.
const (
	// KindSecret identifies the Kubernetes Secret resource.
	KindSecret = "Secret"
	// KindConfigMap identifies the Kubernetes ConfigMap resource.
	KindConfigMap = "ConfigMap"
	// KindPod identifies the Kubernetes Pod resource.
	KindPod = "Pod"
	// KindService identifies the Kubernetes Service resource.
	KindService = "Service"
	// KindStatefulSet identifies the Kubernetes StatefulSet resource.
	KindStatefulSet = "StatefulSet"
	// KindDeployment identifies the Kubernetes Deployment resource.
	KindDeployment = "Deployment"
	// KindReplicaSet identifies the Kubernetes ReplicaSet resource.
	KindReplicaSet = "ReplicaSet"
	// KindRole identifies the Kubernetes Role resource.
	KindRole = "Role"
	// KindServiceAccount identifies the Kubernetes ServiceAccount resource.
	KindServiceAccount = "ServiceAccount"
)
