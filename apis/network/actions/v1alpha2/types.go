package v1alpha2

import (
	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
)

// GroupVersion identifies the atomic action API.
var GroupVersion = schema.GroupVersion{Group: "actions.stacks.org", Version: "v1alpha2"}

// AddToScheme registers the supported atomic actions.
func AddToScheme(s *runtime.Scheme) error {
	s.AddKnownTypes(
		GroupVersion,
		&BitcoinBlockGeneration{},
		&BitcoinBlockGenerationList{},
		&BitcoinReorganization{},
		&BitcoinReorganizationList{},
	)
	metav1.AddToGroupVersion(s, GroupVersion)
	return nil
}

// LocalReference selects one same-namespace resource of the field's fixed kind.
type LocalReference struct {
	// Name is the exact Kubernetes resource name.
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern=`^[a-z0-9](?:[-a-z0-9]{0,61}[a-z0-9])?$`
	Name string `json:"name"`
}

// BitcoinBlockGeneration requests a finite number of acknowledged regtest blocks.
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Blocks",type=integer,JSONPath=`.status.blocksGenerated`
type BitcoinBlockGeneration struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	// Spec is immutable from creation.
	Spec BitcoinBlockGenerationSpec `json:"spec"`
	// Status contains bounded mechanism facts, never a protocol verdict.
	Status BitcoinBlockGenerationStatus `json:"status,omitempty"`
}

// BitcoinBlockGenerationList contains generation actions.
// +kubebuilder:object:root=true
type BitcoinBlockGenerationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	// Items are generation requests.
	Items []BitcoinBlockGeneration `json:"items"`
}

// BitcoinBlockGenerationSpec bounds one target and its receipt-relative cadence.
// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="action spec is immutable"
// +kubebuilder:validation:XValidation:rule="self.cadence.mode != 'Explicit' || (has(self.cadence.delaysSeconds) ? size(self.cadence.delaysSeconds) : 0) == self.count - 1",message="explicit cadence requires count minus one delays"
type BitcoinBlockGenerationSpec struct {
	// NetworkUID pins the canonical network incarnation.
	// +kubebuilder:validation:Type=string
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=128
	NetworkUID types.UID `json:"networkUID"`
	// BitcoinNodeRef names one logical BitcoinNode participant in the pinned network.
	BitcoinNodeRef LocalReference `json:"bitcoinNodeRef"`
	// Count bounds acknowledged single-block RPCs.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=100
	Count int32 `json:"count"`
	// Cadence controls delays between receipts and subsequent single-block requests.
	Cadence GenerationCadence `json:"cadence"`
	// Address receives the regtest coinbase outputs.
	// +kubebuilder:validation:MinLength=14
	// +kubebuilder:validation:MaxLength=128
	Address string `json:"address"`
	// Timeout includes all pending time from API-server creation.
	// +kubebuilder:validation:Type=string
	// +kubebuilder:validation:MaxLength=32
	// +kubebuilder:validation:XValidation:rule="duration(self) > duration('0s')",message="timeout must be positive"
	// +kubebuilder:validation:XValidation:rule="duration(self) <= duration('10m')",message="timeout cannot exceed 10m"
	Timeout metav1.Duration `json:"timeout"`
}

// NetworkIdentity binds admission to the current declaration catalog, not global health.
type NetworkIdentity struct {
	// Name identifies the admitted parent.
	Name string `json:"name"`
	// UID fixes its incarnation.
	UID string `json:"uid"`
	// ObservedGeneration records the declaration generation at admission.
	ObservedGeneration int64 `json:"observedGeneration"`
}

// TargetIdentity records the exact leaf and execution identity admitted for this action.
type TargetIdentity struct {
	// Configuration pins the immutable actor configuration object.
	Configuration common.Binding `json:"configuration"`
	// Credentials pins immutable control credentials without exposing their values.
	Credentials common.Binding `json:"credentials"`
	// APIVersion fixes the actor API.
	APIVersion string `json:"apiVersion"`
	// Kind fixes the actor type.
	Kind string `json:"kind"`
	// Name identifies the compiled leaf.
	Name string `json:"name"`
	// UID fixes the leaf incarnation.
	UID string `json:"uid"`
	// SpecDigest binds actor configuration intent.
	SpecDigest string `json:"specDigest"`
	// ConfigDigest binds approved configuration bytes.
	ConfigDigest string `json:"configDigest"`
	// StatefulSetUID identifies the owning workload incarnation.
	StatefulSetUID string `json:"statefulSetUID"`
	// Revision identifies the admitted workload rollout.
	Revision string `json:"revision"`
	// PodUID identifies the admitted Pod.
	PodUID string `json:"podUID"`
	// ContainerID records the admitted process container.
	ContainerID string `json:"containerID"`
	// RuntimeImageID identifies the executed image.
	RuntimeImageID string `json:"runtimeImageID"`
}

// PolicyIdentity binds the action to the administrator's ledger/configuration profile.
type PolicyIdentity struct {
	// UID identifies the execution ledger.
	UID string `json:"uid"`
	// Generation records its policy generation at admission.
	Generation int64 `json:"generation"`
	// Digest identifies approved RPC configuration.
	Digest string `json:"digest"`
}

// BitcoinBlockGenerationStatus implements the common lifecycle and finite progress.
type BitcoinBlockGenerationStatus struct {
	// AdmittedExecution pins the retained shared executor record.
	AdmittedExecution *common.Binding `json:"admittedExecution,omitempty"`
	// ObservedGeneration identifies the immutable request evaluated.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// Phase projects lifecycle conditions and durable mechanism facts.
	// +kubebuilder:validation:Enum=Pending;Admitted;Active;Recovering;Completed;Recovered;Failed;Inconclusive
	Phase Phase `json:"phase,omitempty"`
	// AdmittedAt records the first durable reservation.
	AdmittedAt *metav1.Time `json:"admittedAt,omitempty"`
	// StartedAt records the first potentially executable authorization.
	StartedAt *metav1.Time `json:"startedAt,omitempty"`
	// ExpiresAt is creation time plus timeout.
	ExpiresAt *metav1.Time `json:"expiresAt,omitempty"`
	// FinishedAt records the frozen terminal outcome time.
	FinishedAt *metav1.Time `json:"finishedAt,omitempty"`
	// CorrelationID snapshots the admission correlation label.
	CorrelationID string `json:"correlationID,omitempty"`
	// AdmittedNetwork fixes the owning network identity.
	AdmittedNetwork *NetworkIdentity `json:"admittedNetwork,omitempty"`
	// AdmittedTarget fixes the leaf and runtime identity.
	AdmittedTarget *TargetIdentity `json:"admittedTarget,omitempty"`
	// AdmittedPolicy identifies the execution profile.
	AdmittedPolicy *PolicyIdentity `json:"admittedPolicy,omitempty"`
	// Conditions record stable lifecycle reasons.
	// +kubebuilder:validation:MaxItems=4
	// +kubebuilder:validation:XValidation:rule="self.all(c, c.type in ['Admitted', 'Progressing', 'EffectObserved', 'CleanupComplete'])",message="unsupported action condition"
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
	// BlocksGenerated counts RPC receipts durably attributed to this action.
	BlocksGenerated int32 `json:"blocksGenerated,omitempty"`
	// LastBlockHash is the latest receipt, not canonical membership evidence.
	LastBlockHash string `json:"lastBlockHash,omitempty"`
	// LastDispatchID acknowledges the ledger receipt copied into status.
	LastDispatchID string `json:"lastDispatchID,omitempty"`
}
