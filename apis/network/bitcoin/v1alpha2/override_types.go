package v1alpha2

import (
	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// BitcoinBlockScheduleOverride changes timing for one bounded production interval.
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:validation:XValidation:rule="self.spec == oldSelf.spec",message="schedule overrides are immutable"
type BitcoinBlockScheduleOverride struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	// Spec fixes one environment, producer and bounded schedule.
	Spec BitcoinBlockScheduleOverrideSpec `json:"spec"`
	// Status projects the retained scheduler activation.
	Status BitcoinBlockScheduleOverrideStatus `json:"status,omitempty"`
}

// BitcoinBlockScheduleOverrideList contains bounded overrides.
// +kubebuilder:object:root=true
type BitcoinBlockScheduleOverrideList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	// Items contains overrides.
	Items []BitcoinBlockScheduleOverride `json:"items"`
}

// BitcoinBlockScheduleOverrideSpec changes timing without changing targets or pause.
// +kubebuilder:validation:XValidation:rule="has(self.schedule) != has(self.scheduleRef)",message="select exactly one schedule source"
type BitcoinBlockScheduleOverrideSpec struct {
	// NetworkUID pins the canonical network incarnation.
	// +kubebuilder:validation:Type=string
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=128
	NetworkUID types.UID `json:"networkUID"`
	// ProductionRef selects the logical BitcoinBlockProduction participant.
	ProductionRef common.NameRef `json:"productionRef"`
	// Schedule contains immutable inline timing.
	Schedule *BitcoinBlockScheduleSpec `json:"schedule,omitempty"`
	// ScheduleRef selects one immutable same-namespace schedule.
	ScheduleRef *common.NameRef `json:"scheduleRef,omitempty"`
	// Duration starts at activation, including time spent paused.
	// +kubebuilder:validation:XValidation:rule="duration(self) > duration('0s') && duration(self) <= duration('1h')",message="duration must be positive and at most 1h"
	Duration common.Duration `json:"duration"`
}

// BitcoinActiveOverride is the sole retained override authority in the initialization record.
type BitcoinActiveOverride struct {
	// Override pins the user-owned request.
	Override common.Binding `json:"override"`
	// Production pins the admitted producer incarnation.
	Production common.Binding `json:"production"`
	// Schedule snapshots the effective cadence.
	Schedule BitcoinBlockScheduleSpec `json:"schedule"`
	// ScheduleRef pins a selected immutable reusable schedule.
	ScheduleRef *common.Binding `json:"scheduleRef,omitempty"`
	// StartedAt is the original successful activation time.
	StartedAt metav1.Time `json:"startedAt"`
	// ExpiresAt bounds future overridden opportunities.
	ExpiresAt metav1.Time `json:"expiresAt"`
}

// BitcoinBlockScheduleOverrideStatus reports activation independently of baseline policy edits.
type BitcoinBlockScheduleOverrideStatus struct {
	// Phase reports deterministic contention and bounded activation.
	// +kubebuilder:validation:Enum=Pending;Active;Completed;Expired;Cancelled
	Phase OverridePhase `json:"phase,omitempty"`
	// ObservedGeneration identifies the immutable request evaluated.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// Admission preserves the actual retained scheduler activation.
	Admission *BitcoinActiveOverride `json:"admission,omitempty"`
	// PendingExpiresAt is creation time plus the fixed five-minute admission window.
	PendingExpiresAt *metav1.Time `json:"pendingExpiresAt,omitempty"`
	// Reason describes current eligibility or terminal disposition.
	// +kubebuilder:validation:MaxLength=128
	Reason string `json:"reason,omitempty"`
}
