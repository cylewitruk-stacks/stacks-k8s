package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// LocalObjectReference identifies a same-namespace topology object.
type LocalObjectReference struct {
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern=`^[a-z0-9](?:[-a-z0-9]{0,61}[a-z0-9])?$`
	Name string `json:"name"`
}

// ConfigSource selects generated, inline, ConfigMap, or Secret configuration.
// +kubebuilder:validation:XValidation:rule="(has(self.generated) ? 1 : 0) + (has(self.inline) ? 1 : 0) + (has(self.configMapRef) ? 1 : 0) + (has(self.secretRef) ? 1 : 0) == 1",message="exactly one configuration source is required"
type ConfigSource struct {
	Generated    *GeneratedConfig `json:"generated,omitempty"`
	Inline       *InlineConfig    `json:"inline,omitempty"`
	ConfigMapRef *ConfigObjectRef `json:"configMapRef,omitempty"`
	SecretRef    *ConfigObjectRef `json:"secretRef,omitempty"`
}

// GeneratedConfig selects one bounded built-in configuration profile.
type GeneratedConfig struct {
	// +kubebuilder:validation:Enum=bitcoin-regtest/v1;nakamoto-regtest-node/v1
	Profile string `json:"profile"`
	// Seed pins a generated Stacks node identity. Empty derives from network and actor names.
	Seed string `json:"seed,omitempty"`
	// +kubebuilder:validation:MaxItems=64
	// +listType=set
	BootstrapPeers []string `json:"bootstrapPeers,omitempty"`
	// +kubebuilder:validation:Enum=queued;blocking
	EventDispatcher string `json:"eventDispatcher,omitempty"`
}

// InlineConfig contains public, non-secret actor configuration.
type InlineConfig struct {
	// +kubebuilder:validation:MinLength=1
	Data      string `json:"data"`
	Key       string `json:"key,omitempty"`
	MountPath string `json:"mountPath,omitempty"`
}

// ConfigObjectRef identifies one complete configuration file.
type ConfigObjectRef struct {
	// +kubebuilder:validation:MinLength=1
	Name      string `json:"name"`
	Key       string `json:"key,omitempty"`
	MountPath string `json:"mountPath,omitempty"`
	// ExpectedDigest triggers a restart and verifies mounted bytes before actor startup.
	// +kubebuilder:validation:Pattern=`^sha256:[0-9a-f]{64}$`
	ExpectedDigest string `json:"expectedDigest,omitempty"`
}

// NetworkDefaults supplies values inherited by compiled leaf resources.
type NetworkDefaults struct {
	BitcoinImage      string                        `json:"bitcoinImage,omitempty"`
	StacksNodeImage   string                        `json:"stacksNodeImage,omitempty"`
	StacksSignerImage string                        `json:"stacksSignerImage,omitempty"`
	DependencyImage   string                        `json:"dependencyImage,omitempty"`
	ImagePullPolicy   corev1.PullPolicy             `json:"imagePullPolicy,omitempty"`
	ImagePullSecrets  []corev1.LocalObjectReference `json:"imagePullSecrets,omitempty"`
	Workload          WorkloadSpec                  `json:"workload,omitempty"`
}

// WorkloadSpec contains shared resource, storage, and scheduling controls.
type WorkloadSpec struct {
	Storage      *StorageSpec      `json:"storage,omitempty"`
	Resources    *ResourceSpec     `json:"resources,omitempty"`
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`
	Tolerations  []TolerationSpec  `json:"tolerations,omitempty"`
	// +kubebuilder:validation:Minimum=0
	TerminationGracePeriodSeconds *int64 `json:"terminationGracePeriodSeconds,omitempty"`
}

// ResourceSpec contains CPU and memory requests and limits for one actor.
type ResourceSpec struct {
	Requests ResourceValues `json:"requests,omitempty"`
	Limits   ResourceValues `json:"limits,omitempty"`
}

// ResourceValues contains Kubernetes quantity strings for CPU and memory.
type ResourceValues struct {
	CPU    string `json:"cpu,omitempty"`
	Memory string `json:"memory,omitempty"`
}

// TolerationSpec is the bounded subset of Pod toleration fields used by actors.
type TolerationSpec struct {
	Key string `json:"key,omitempty"`
	// +kubebuilder:validation:Enum=Exists;Equal
	Operator corev1.TolerationOperator `json:"operator,omitempty"`
	Value    string                    `json:"value,omitempty"`
	// +kubebuilder:validation:Enum=NoSchedule;PreferNoSchedule;NoExecute
	Effect            corev1.TaintEffect `json:"effect,omitempty"`
	TolerationSeconds *int64             `json:"tolerationSeconds,omitempty"`
}

// StorageSpec configures an actor data volume.
type StorageSpec struct {
	Enabled *bool `json:"enabled,omitempty"`
	// +kubebuilder:validation:Pattern=`^[0-9]+(?:Ei|Pi|Ti|Gi|Mi|Ki|E|P|T|G|M|K)?$`
	Size             string  `json:"size,omitempty"`
	MountPath        string  `json:"mountPath,omitempty"`
	StorageClassName *string `json:"storageClassName,omitempty"`
	// +listType=set
	AccessModes    []corev1.PersistentVolumeAccessMode `json:"accessModes,omitempty"`
	RetainOnDelete bool                                `json:"retainOnDelete,omitempty"`
}

// ContainerOverride exposes explicit process controls for version-specific images.
// Operator-owned environment names are reserved and cannot be overridden.
// +kubebuilder:validation:XValidation:rule="!has(self.env) || !self.env.exists(key, key in ['POD_IP', 'STACKS_ACTOR', 'STACKS_ACTOR_ROLE', 'STACKS_CONFIG_RENDERED', 'STACKS_CONFIG_TEMPLATE', 'STACKS_NETWORK', 'STACKS_SERVICE_MAP'])",message="container environment must not override operator-owned variables"
type ContainerOverride struct {
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=32
	Command []string `json:"command,omitempty"`
	// +kubebuilder:validation:MaxItems=128
	Args []string `json:"args,omitempty"`
	// +kubebuilder:validation:MaxProperties=128
	Env        map[string]string `json:"env,omitempty"`
	WorkingDir string            `json:"workingDir,omitempty"`
}

// PortSpec exposes one named actor port.
type PortSpec struct {
	// +kubebuilder:validation:Pattern=`^[a-z0-9](?:[-a-z0-9]{0,13}[a-z0-9])?$`
	Name string `json:"name"`
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	Port     int32           `json:"port"`
	Protocol corev1.Protocol `json:"protocol,omitempty"`
}

// ActorIdentity is the immutable admitted identity of one running actor.
type ActorIdentity struct {
	Kind               string `json:"kind"`
	Name               string `json:"name"`
	Role               string `json:"role"`
	ResourceName       string `json:"resourceName"`
	ServiceName        string `json:"serviceName"`
	StatefulSetName    string `json:"statefulSetName"`
	StatefulSetUID     string `json:"statefulSetUID"`
	ControllerRevision string `json:"controllerRevision"`
	PodName            string `json:"podName"`
	PodUID             string `json:"podUID"`
	RequestedImage     string `json:"requestedImage"`
	RuntimeImageID     string `json:"runtimeImageID"`
	ConfigDigest       string `json:"configDigest"`
	SpecDigest         string `json:"specDigest"`
}

// ActorStatus reports one leaf controller's rollout and admitted identity.
type ActorStatus struct {
	ObservedGeneration int64              `json:"observedGeneration,omitempty"`
	Phase              string             `json:"phase,omitempty"`
	Ready              bool               `json:"ready,omitempty"`
	Identity           *ActorIdentity     `json:"identity,omitempty"`
	Conditions         []metav1.Condition `json:"conditions,omitempty"`
}

// ChildStatus summarizes one compiled topology resource.
type ChildStatus struct {
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	ActorName string `json:"actorName"`
	Ready     bool   `json:"ready"`
	Phase     string `json:"phase,omitempty"`
}
