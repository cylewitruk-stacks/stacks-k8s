// Package v1alpha2 defines shared public configuration values without controller dependencies.
package v1alpha2

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
)

// NameRef selects a resource or participant in the same namespace.
type NameRef struct {
	// Name is the referenced name; the containing field determines its kind.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9.]*[a-z0-9])?$`
	Name string `json:"name"`
}

// Binding identifies an exact resolved public dependency.
type Binding struct {
	// Kind identifies the API kind or participant kind.
	Kind string `json:"kind"`
	// Name is the public resource name.
	Name string `json:"name"`
	// UID prevents same-name replacement from inheriting authority.
	UID types.UID `json:"uid"`
	// Fingerprint identifies public content when relevant.
	Fingerprint string `json:"fingerprint,omitempty"`
}

// SecretKeyRef selects immutable secret material without embedding it in an API.
type SecretKeyRef struct {
	// Name selects a same-namespace Secret.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	Name string `json:"name"`
	// Key selects one entry.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	Key string `json:"key"`
}

// PublicIdentity carries validated public account inputs.
type PublicIdentity struct {
	// Address is a testnet Stacks address.
	// +kubebuilder:validation:MaxLength=64
	// +kubebuilder:validation:Pattern=`^ST[0-9A-HJKMNP-TV-Z]{26,39}$`
	Address string `json:"address"`
	// PublicKey is a compressed secp256k1 public key.
	// +kubebuilder:validation:Pattern=`^(02|03)[0-9a-fA-F]{64}$`
	PublicKey string `json:"publicKey"`
}

// KeySource selects one identity source. Omission on an account means generation.
// +kubebuilder:validation:XValidation:rule="(has(self.generate) ? 1 : 0) + (has(self.secretRef) ? 1 : 0) + (has(self.publicIdentity) ? 1 : 0) == 1",message="select exactly one key source"
type KeySource struct {
	// Generate requests a new reusable key.
	// +kubebuilder:validation:Enum=true
	Generate *bool `json:"generate,omitempty"`
	// SecretRef imports private material into a scoped resolver.
	SecretRef *SecretKeyRef `json:"secretRef,omitempty"`
	// PublicIdentity provides no signing authority.
	PublicIdentity *PublicIdentity `json:"publicIdentity,omitempty"`
}

// ResolutionStatus describes reusable inputs, never per-network execution.
type ResolutionStatus struct {
	// ObservedGeneration identifies the evaluated spec.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// Identity contains public account information.
	Identity *PublicIdentity `json:"identity,omitempty"`
	// CredentialsRef identifies generated or imported private material.
	CredentialsRef *SecretKeyRef `json:"credentialsRef,omitempty"`
	// Digest identifies resolved public inputs.
	Digest string `json:"digest,omitempty"`
	// BitcoinAddress is the resolved public wallet payout address.
	BitcoinAddress string `json:"bitcoinAddress,omitempty"`
	// Descriptor is the supported public wallet descriptor.
	Descriptor string `json:"descriptor,omitempty"`
	// Dependencies pins public source identities.
	// +kubebuilder:validation:MaxItems=8
	Dependencies []Binding `json:"dependencies,omitempty"`
	// CredentialsUID pins imported or generated key material after resolution.
	CredentialsUID types.UID `json:"credentialsUID,omitempty"`
	// Conditions report resolution without runtime claims.
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// Storage defines per-instance volumes; absence preserves inheritance.
// +kubebuilder:validation:XValidation:rule="!has(self.ephemeral) || !self.ephemeral || (!has(self.size) && !has(self.class) && !has(self.retainOnDelete))",message="ephemeral storage excludes PVC settings"
type Storage struct {
	// Size is a Kubernetes capacity quantity.
	// +kubebuilder:validation:MaxLength=32
	Size *string `json:"size,omitempty"`
	// Class preserves explicit empty storageClassName.
	// +kubebuilder:validation:MaxLength=253
	Class *string `json:"class,omitempty"`
	// RetainOnDelete preserves a PVC after instance removal.
	RetainOnDelete *bool `json:"retainOnDelete,omitempty"`
	// Ephemeral selects emptyDir.
	Ephemeral *bool `json:"ephemeral,omitempty"`
}

// Placement controls scheduling, independently for actors and support workloads.
type Placement struct {
	// NodeSelector constrains eligible nodes.
	// +kubebuilder:validation:MaxProperties=32
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`
	// Tolerations allow explicit support-node placement.
	// +kubebuilder:validation:MaxItems=32
	Tolerations *[]corev1.Toleration `json:"tolerations,omitempty"`
	// SpreadAcrossNodes requests preferred same-kind anti-affinity.
	SpreadAcrossNodes *bool `json:"spreadAcrossNodes,omitempty"`
}

// Peers selects startup hints; seed loss does not revoke admitted runtime.
// +kubebuilder:validation:XValidation:rule="has(self.nodeRefs) != has(self.discovery)",message="select nodeRefs or discovery"
type Peers struct {
	// NodeRefs identifies explicit participant seeds.
	// +kubebuilder:validation:MaxItems=100
	// +listType=map
	// +listMapKey=name
	NodeRefs *[]NameRef `json:"nodeRefs,omitempty"`
	// Discovery resolves the selected network graph.
	// +kubebuilder:validation:Enum=Network
	Discovery *string `json:"discovery,omitempty"`
}

// ServiceRef resolves a named placeholder to participant Service DNS.
type ServiceRef struct {
	// Alias identifies the placeholder.
	// +kubebuilder:validation:MaxLength=63
	Alias string `json:"alias"`
	// Kind restricts the target API kind.
	// +kubebuilder:validation:Enum=BitcoinNode;StacksNode;StacksSigner
	Kind string `json:"kind"`
	// Name identifies a selected participant.
	// +kubebuilder:validation:MaxLength=63
	Name string `json:"name"`
	// Endpoint selects the advertised port role.
	// +kubebuilder:validation:Enum=rpc;p2p;events
	Endpoint string `json:"endpoint"`
}

// Config selects typed TOML overrides or a complete immutable config Secret.
// +kubebuilder:validation:XValidation:rule="!(has(self.overrides) && has(self.secretRef))",message="config overrides and secretRef are exclusive"
type Config struct {
	// Overrides contains TOML-compatible values validated by the resolver.
	// +kubebuilder:pruning:PreserveUnknownFields
	Overrides *runtime.RawExtension `json:"overrides,omitempty"`
	// SecretRef selects a complete configuration version.
	SecretRef *SecretKeyRef `json:"secretRef,omitempty"`
	// Compatibility permits intentionally divergent experimental actors.
	// +kubebuilder:validation:Enum=Managed;Unverified
	Compatibility *string `json:"compatibility,omitempty"`
	// ServiceRefs resolves explicit placeholders.
	// +kubebuilder:validation:MaxItems=100
	// +listType=map
	// +listMapKey=alias
	ServiceRefs []ServiceRef `json:"serviceRefs,omitempty"`
}

// ActorFields are shared inheritable workload values. No schema defaults are applied.
type ActorFields struct {
	// Image selects the actor binary.
	// +kubebuilder:validation:MaxLength=512
	Image *string `json:"image,omitempty"`
	// ImagePullPolicy controls image retrieval.
	// +kubebuilder:validation:Enum=Always;IfNotPresent;Never
	ImagePullPolicy *corev1.PullPolicy `json:"imagePullPolicy,omitempty"`
	// Resources are workload requests/limits.
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`
	// Placement applies only to actor Pods.
	Placement *Placement `json:"placement,omitempty"`
	// Storage configures instance-local persistence.
	Storage *Storage `json:"storage,omitempty"`
	// Config selects configuration customization.
	Config *Config `json:"config,omitempty"`
}

// WorkerFields controls support workload placement without execution policy.
type WorkerFields struct {
	// WorkerPlacement applies to scoped support Pods.
	WorkerPlacement *Placement `json:"workerPlacement,omitempty"`
}

// Amount is an unsigned micro-unit quantity encoded as a decimal string.
// +kubebuilder:validation:Pattern=`^(0|[1-9][0-9]{0,19})$`
// +kubebuilder:validation:MaxLength=20
// +kubebuilder:validation:XValidation:rule="uint(self) <= 18446744073709551615u",message="amount must fit uint64"
type Amount string

// Duration is a positive bounded Go duration, validated semantically by the resolver.
// +kubebuilder:validation:MaxLength=32
// +kubebuilder:validation:MinLength=2
type Duration string
