package v1alpha1

// TargetDeclarationsVersion identifies the compiled-declaration status contract.
const TargetDeclarationsVersion = "network.stacks.org/target-declarations/v1"

// TargetDeclarations describes current parent intent independently of actor readiness.
type TargetDeclarations struct {
	// SchemaVersion identifies the entry and digest interpretation.
	// +kubebuilder:validation:Enum=network.stacks.org/target-declarations/v1
	SchemaVersion string `json:"schemaVersion"`
	// NetworkUID prevents admission across parent recreation.
	// +kubebuilder:validation:MinLength=1
	NetworkUID string `json:"networkUID"`
	// ObservedGeneration binds compilation to a particular admitted parent spec.
	// +kubebuilder:validation:Minimum=1
	ObservedGeneration int64 `json:"observedGeneration"`
	// Actors lists compiled leaf declarations in stable kind/name order.
	// +kubebuilder:validation:MaxItems=232
	// +listType=map
	// +listMapKey=kind
	// +listMapKey=name
	Actors []TargetDeclaration `json:"actors"`
}

// TargetDeclaration binds a named actor to its complete compiled leaf specification.
type TargetDeclaration struct {
	// Kind is the leaf resource kind.
	// +kubebuilder:validation:Enum=BitcoinNode;StacksNode;StacksSigner
	Kind string `json:"kind"`
	// Name is the compiled same-namespace resource name.
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern=`^[a-z0-9](?:[-a-z0-9]{0,61}[a-z0-9])?$`
	Name string `json:"name"`
	// ActorName is the logical actor name in the parent topology.
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern=`^[a-z0-9](?:[-a-z0-9]{0,61}[a-z0-9])?$`
	ActorName string `json:"actorName"`
	// Suspended is the effective parent-or-actor suspension state.
	Suspended bool `json:"suspended"`
	// SpecDigest uses the existing canonical leaf-specification digest contract.
	// +kubebuilder:validation:Pattern=`^sha256:[0-9a-f]{64}$`
	SpecDigest string `json:"specDigest"`
}
