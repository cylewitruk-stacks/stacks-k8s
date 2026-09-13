package v1alpha2

import (
	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// BitcoinScheduledTarget preserves one admitted weighted participant identity.
type BitcoinScheduledTarget struct {
	// Participant binds the exact selected BitcoinNode incarnation.
	Participant common.Binding `json:"participant"`
	// Weight is sampled independently of target availability.
	// +kubebuilder:validation:Minimum=1
	Weight int32 `json:"weight"`
}

// BitcoinSchedulingStatus projects the effective network baseline onto the current producer.
type BitcoinSchedulingStatus struct {
	// Override identifies active temporary timing while admission retains the latest baseline.
	Override *BitcoinActiveOverride `json:"override,omitempty"`
	// Initialization identifies the retained network scheduler authority.
	Initialization common.Binding `json:"initialization"`
	// AdmissionDigest binds the complete admitted configuration and dependency snapshots.
	// +kubebuilder:validation:MaxLength=71
	AdmissionDigest string `json:"admissionDigest"`
	// PolicyDigest identifies the admitted production configuration.
	// +kubebuilder:validation:MaxLength=71
	PolicyDigest string `json:"policyDigest"`
	// Schedule is the exact effective timing value.
	Schedule *BitcoinBlockScheduleSpec `json:"schedule,omitempty"`
	// ScheduleRef pins an admitted reusable schedule when one was selected.
	ScheduleRef *common.Binding `json:"scheduleRef,omitempty"`
	// ScheduleGeneration records the observed immutable schedule revision; zero means inline.
	// +kubebuilder:validation:Minimum=0
	ScheduleGeneration int64 `json:"scheduleGeneration,omitempty"`
	// Targets preserves target order and exact participant UIDs independently of health.
	// +kubebuilder:validation:MaxItems=100
	// +listType=atomic
	Targets []BitcoinScheduledTarget `json:"targets,omitempty"`
	// NextOpportunityAt is the next durable opportunity deadline, without catch-up work.
	NextOpportunityAt *metav1.Time `json:"nextOpportunityAt,omitempty"`
	// Opportunities counts due baseline selections across producer replacements.
	// +kubebuilder:validation:Minimum=0
	Opportunities int64 `json:"opportunities"`
	// Assigned counts selected offers published to their sole execution record.
	// +kubebuilder:validation:Minimum=0
	Assigned int64 `json:"assigned"`
	// Acknowledged counts exact baseline generation receipts, independently of assignment ordering.
	// +kubebuilder:validation:Minimum=0
	Acknowledged int64 `json:"acknowledged"`
	// LastAcknowledgedAt preserves the latest original attributable baseline receipt time.
	LastAcknowledgedAt *metav1.Time `json:"lastAcknowledgedAt,omitempty"`
	// ObservedAt records when current baseline target availability was checked.
	ObservedAt *metav1.Time `json:"observedAt,omitempty"`
	// EligibleTargets counts currently ready admitted targets independently of the committed draw.
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=100
	EligibleTargets int32 `json:"eligibleTargets"`
	// ProgressWindowSeconds bounds expected receipt progress using the effective cadence upper bound.
	// +kubebuilder:validation:Minimum=130
	// +kubebuilder:validation:Maximum=10810
	ProgressWindowSeconds int64 `json:"progressWindowSeconds,omitempty"`
	// Skipped counts selections lost to unavailable or busy targets without redraw.
	// +kubebuilder:validation:Minimum=0
	Skipped int64 `json:"skipped"`
	// Unassigned counts selected opportunities that expired or were withdrawn before assignment.
	// +kubebuilder:validation:Minimum=0
	Unassigned int64 `json:"unassigned"`
	// Reason classifies the current baseline scheduling state.
	// +kubebuilder:validation:MaxLength=128
	Reason string `json:"reason"`
}

// BitcoinReceiptCursor prevents duplicate counting when different nodes complete out of order.
type BitcoinReceiptCursor struct {
	// ExecutionUID pins the network-retained execution record.
	// +kubebuilder:validation:Type=string
	// +kubebuilder:validation:MaxLength=128
	ExecutionUID types.UID `json:"executionUID"`
	// Offer is the latest accounted offer on this one execution record.
	// +kubebuilder:validation:Minimum=1
	Offer int64 `json:"offer"`
}

// BitcoinBaselineStatus retains the sole durable selection authority after initialization.
type BitcoinBaselineStatus struct {
	// Scheduling contains bounded effective policy and cumulative network baseline facts.
	Scheduling BitcoinSchedulingStatus `json:"scheduling"`
	// Sequence is monotonic across bootstrap and every baseline producer incarnation.
	// +kubebuilder:validation:Minimum=0
	Sequence int64 `json:"sequence"`
	// SelectedTarget is the one committed draw for the current opportunity.
	SelectedTarget *common.Binding `json:"selectedTarget,omitempty"`
	// Stage records durable selection, publication or final non-execution disposition.
	// +kubebuilder:validation:Enum=Selected;Offered;Assigned;Skipped;Unassigned
	Stage BaselineStage `json:"stage,omitempty"`
	// Receipts retains accounting cursors for network-owned execution records after target removal.
	// +kubebuilder:validation:MaxItems=1000
	// +listType=map
	// +listMapKey=executionUID
	Receipts []BitcoinReceiptCursor `json:"receipts,omitempty"`
}
