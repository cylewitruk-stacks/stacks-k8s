package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// GroupVersion identifies the Bitcoin capability API.
var GroupVersion = schema.GroupVersion{Group: "bitcoin.stacks.org", Version: "v1alpha1"}

// AddToScheme registers Bitcoin capability resources.
func AddToScheme(scheme *runtime.Scheme) error {
	scheme.AddKnownTypes(GroupVersion, &BitcoinBlockProduction{}, &BitcoinBlockProductionList{})
	metav1.AddToGroupVersion(scheme, GroupVersion)
	return nil
}

// ProductionPolicy requests one block per interval on one declared actor.
type ProductionPolicy struct {
	// Target names a Bitcoin actor in the owning network.
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern=`^[a-z0-9](?:[-a-z0-9]{0,61}[a-z0-9])?$`
	Target string `json:"target"`
	// IntervalSeconds is the delay after a completed dispatch; missed ticks are discarded.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=86400
	IntervalSeconds int32 `json:"intervalSeconds"`
	// Address receives generated regtest coinbase outputs.
	// +kubebuilder:validation:MinLength=14
	// +kubebuilder:validation:MaxLength=128
	Address string `json:"address"`
	// Paused stops new dispatches without cancelling one already armed.
	Paused bool `json:"paused,omitempty"`
}

// BitcoinBlockProduction maintains aggregate-owned fixed-cadence regtest production.
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Blocks",type=integer,JSONPath=`.status.blocksProduced`
type BitcoinBlockProduction struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	// Spec is compiled from the owning network.
	Spec BitcoinBlockProductionSpec `json:"spec"`
	// Status retains dispatch evidence for this network's production lifetime.
	Status BitcoinBlockProductionStatus `json:"status,omitempty"`
}

// BitcoinBlockProductionList contains production resources.
// +kubebuilder:object:root=true
type BitcoinBlockProductionList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []BitcoinBlockProduction `json:"items"`
}

// BitcoinBlockProductionSpec binds one policy to an immutable network and target.
// +kubebuilder:validation:XValidation:rule="self.networkName == oldSelf.networkName && self.networkUID == oldSelf.networkUID && self.policy.target == oldSelf.policy.target",message="network and production target are immutable"
type BitcoinBlockProductionSpec struct {
	// NetworkName identifies the owning StacksNetwork.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	NetworkName string `json:"networkName"`
	// NetworkUID prevents admission after parent replacement.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=64
	NetworkUID string `json:"networkUID"`
	// Policy supplies the current target, cadence, destination and pause state.
	Policy ProductionPolicy `json:"policy"`
}

// BitcoinBlockProductionStatus is both the bounded dispatch ledger and user-facing state.
type BitcoinBlockProductionStatus struct {
	// ObservedGeneration identifies the policy reflected by Phase.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// Phase is Waiting, Collecting, Accounting, Running, Paused, Blocked, or Abandoned.
	Phase string `json:"phase,omitempty"`
	// Message explains the current operational state without credential material.
	Message string `json:"message,omitempty"`
	// DispatchState is empty before first use, Idle after a receipt, or Armed until resolved.
	// +kubebuilder:validation:Enum=Idle;Armed
	DispatchState string `json:"dispatchState,omitempty"`
	// DispatchID uniquely identifies the last authorization, including its process-start nonce.
	DispatchID string `json:"dispatchID,omitempty"`
	// TargetUID records the admitted leaf incarnation.
	TargetUID string `json:"targetUID,omitempty"`
	// PodUID records the admitted Pod incarnation.
	PodUID string `json:"podUID,omitempty"`
	// ContainerID records the admitted Bitcoin process container.
	ContainerID string `json:"containerID,omitempty"`
	// BlocksProduced counts blocks acknowledged and atomically accounted by this ledger.
	BlocksProduced int64 `json:"blocksProduced,omitempty"`
	// LastBlockHash is the latest acknowledged block; it is not a canonical-chain assertion.
	LastBlockHash string `json:"lastBlockHash,omitempty"`
	// LastCompletedAt anchors cadence across ordinary controller restarts.
	LastCompletedAt *metav1.Time `json:"lastCompletedAt,omitempty"`
}
