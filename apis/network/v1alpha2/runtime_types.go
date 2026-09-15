package v1alpha2

import common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"

// RuntimeEndpoint exposes one identity-bound participant Service.
type RuntimeEndpoint struct {
	// Name identifies the protocol endpoint.
	Name string `json:"name"`
	// Host is the actual same-namespace Service DNS name.
	Host string `json:"host"`
	// Port is the protocol port.
	Port int32 `json:"port"`
}

// ParticipantRuntimeStatus contains domain-owned workload observations, never credentials.
type ParticipantRuntimeStatus struct {
	// Protocol contains read-only native Stacks progress and prepared-set observations.
	Protocol *StacksProtocolObservation `json:"protocol,omitempty"`
	// WorkerCandidate reports an inactive standalone management Pod for root binding.
	WorkerCandidate *WorkerCandidate `json:"workerCandidate,omitempty"`
	// ObservedGeneration identifies the participant controls/configuration evaluated.
	ObservedGeneration int64 `json:"observedGeneration"`
	// PolicyDigest identifies the complete admitted policy used by this runtime.
	PolicyDigest string `json:"policyDigest,omitempty"`
	// WorkloadRefs identifies the actual owned workload roots.
	// +kubebuilder:validation:MaxItems=8
	// +listType=map
	// +listMapKey=name
	WorkloadRefs []common.Binding `json:"workloadRefs,omitempty"`
	// ConfigRef identifies immutable rendered server configuration.
	ConfigRef *common.Binding `json:"configRef,omitempty"`
	// ConfigurationDigest identifies public rendering inputs, including signer attachments.
	ConfigurationDigest string `json:"configurationDigest,omitempty"`
	// EventAuthSecretRef binds the node-owned authentication shared only with its signer.
	EventAuthSecretRef *common.Binding `json:"eventAuthSecretRef,omitempty"`
	// RPCSecretRef identifies this node's mutation-worker credentials.
	RPCSecretRef *common.Binding `json:"rpcSecretRef,omitempty"`
	// ObserverRPCSecretRef pins the read-only Bitcoin observer credential.
	ObserverRPCSecretRef *common.Binding `json:"observerRPCSecretRef,omitempty"`
	// ActorRPCSecretRef identifies restricted protocol-client credentials.
	ActorRPCSecretRef *common.Binding `json:"actorRPCSecretRef,omitempty"`
	// Endpoints contains actual Service addresses.
	// +kubebuilder:validation:MaxItems=8
	// +listType=map
	// +listMapKey=name
	Endpoints []RuntimeEndpoint `json:"endpoints,omitempty"`
	// PodIP is the native address observed with PodRef and ContainerID.
	// +kubebuilder:validation:MaxLength=45
	PodIP string `json:"podIP,omitempty"`
	// PodRef pins the most recently observed actor Pod.
	PodRef *common.Binding `json:"podRef,omitempty"`
	// ContainerID identifies the current or last observed actor process.
	ContainerID string `json:"containerID,omitempty"`
	// ImageID records the kubelet-resolved actor image digest.
	ImageID string `json:"imageID,omitempty"`
	// Terminated requires observed process termination or proof no process was created.
	Terminated bool `json:"terminated"`
}
