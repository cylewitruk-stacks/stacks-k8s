package v1alpha2

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
)

// GroupVersion identifies the continuous observation API.
var GroupVersion = schema.GroupVersion{Group: "observation.stacks.org", Version: "v1alpha2"}

// SchemeBuilder registers telemetry objects.
var SchemeBuilder = runtime.NewSchemeBuilder(func(s *runtime.Scheme) error {
	s.AddKnownTypes(GroupVersion, &NetworkTelemetry{}, &NetworkTelemetryList{})
	metav1.AddToGroupVersion(s, GroupVersion)
	return nil
})

// AddToScheme installs telemetry types.
var AddToScheme = SchemeBuilder.AddToScheme

const (
	// KindNetworkTelemetry identifies a recording declaration.
	KindNetworkTelemetry = "NetworkTelemetry"
	// ResourceNetworkTelemetries is the plural API resource.
	ResourceNetworkTelemetries = "networktelemetries"
	// LabelTelemetryUID binds generated recording resources.
	LabelTelemetryUID = "observation.stacks.org/telemetry-uid"
	// ConditionWorkloadsReady describes collector deployment health, not source continuity.
	ConditionWorkloadsReady = "WorkloadsReady"
	// ConditionNetworkResolved records initial exact-UID admission.
	ConditionNetworkResolved = "NetworkResolved"
	// ReasonAdmitted records successful identity admission.
	ReasonAdmitted = "Admitted"
	// ReasonNetworkUnavailable records unavailable or replaced source identity.
	ReasonNetworkUnavailable = "NetworkUnavailable"
	// ReasonProvisioning records pending collector workloads.
	ReasonProvisioning = "Provisioning"
	// ReasonAvailable records ready collector workloads.
	ReasonAvailable = "Available"
	// ReasonOwnershipConflict refuses adoption of foreign resources.
	ReasonOwnershipConflict = "OwnershipConflict"
)

// Retention fixes table TTL at first creation. A new recording selects a different TTL.
type Retention struct {
	// Window is the log/object TTL; native metrics use the backend profile's fixed 24h TTL.
	// TTL is not a hard byte quota or immediate physical erasure guarantee.
	// +kubebuilder:validation:Enum="1h";"6h";"24h"
	Window string `json:"window"`
}

// Sources selects independent recording sources. Recorder health and actor container CPU/memory are always recorded.
// Node collectors and their health metrics exist only when logs or native metrics are enabled.
type Sources struct {
	// Objects captures allowlisted public Kubernetes observations.
	Objects bool `json:"objects"`
	// Logs captures redacted workload stdout/stderr.
	Logs bool `json:"logs"`
	// Metrics scrapes named native metrics ports; actor container CPU/memory does not depend on this field.
	Metrics bool `json:"metrics"`
}

// NetworkTelemetrySpec configures a same-namespace recording independent of network ownership.
// +kubebuilder:validation:XValidation:rule="self.networkName == oldSelf.networkName && self.networkUID == oldSelf.networkUID",message="network identity is immutable"
// +kubebuilder:validation:XValidation:rule="self.retention == oldSelf.retention && self.storageSecretRef == oldSelf.storageSecretRef",message="storage and retention require a new recording"
// +kubebuilder:validation:XValidation:rule="self.sources.objects || self.sources.logs || self.sources.metrics",message="select at least one source"
type NetworkTelemetrySpec struct {
	// NetworkName selects the source root; its UID must match before collector creation.
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
	// +kubebuilder:validation:MaxLength=63
	NetworkName string `json:"networkName"`
	// NetworkUID prevents same-name replacement from inheriting collection.
	// +kubebuilder:validation:Type=string
	// +kubebuilder:validation:Pattern=`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`
	NetworkUID types.UID `json:"networkUID"`
	// StorageSecretRef names administrator-provisioned endpoint and ingestion credentials.
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
	// +kubebuilder:validation:MaxLength=63
	StorageSecretRef string `json:"storageSecretRef"`
	// Retention sets immutable table TTL.
	Retention Retention `json:"retention"`
	// Sources controls future capture; changes can restart collectors and create explicit gaps.
	Sources Sources `json:"sources"`
}

// SourceReason is a bounded source-health diagnostic.
// WatchInterrupted remains accepted for persisted status compatibility; current recorders do not emit it.
// +kubebuilder:validation:Enum=Available;Unavailable;APINotInstalled;AccessDenied;ReadUnavailable;WatchInterrupted
type SourceReason string

const (
	// SourceAvailable reports a successful read or active subscription.
	SourceAvailable SourceReason = "Available"
	// SourceUnavailable reports a source without a current observation.
	SourceUnavailable SourceReason = "Unavailable"
	// SourceAPINotInstalled identifies a missing optional Kubernetes API.
	SourceAPINotInstalled SourceReason = "APINotInstalled"
	// SourceAccessDenied identifies insufficient read permissions.
	SourceAccessDenied SourceReason = "AccessDenied"
	// SourceReadUnavailable identifies a failed source request without exposing its raw response.
	SourceReadUnavailable SourceReason = "ReadUnavailable"
)

// SourceStatus reports bounded point-in-time source health; records live in the backend.
type SourceStatus struct {
	// Name identifies the Kubernetes resource source.
	Name string `json:"name"`
	// Available reports a successful current list/watch or export path.
	Available bool `json:"available"`
	// Reason identifies the current source health boundary.
	Reason SourceReason `json:"reason"`
	// ObservedAt is the latest source health check, not a completeness watermark.
	ObservedAt metav1.Time `json:"observedAt"`
	// Gaps counts discontinuities observed by the current recorder process.
	Gaps int64 `json:"gaps"`
}

// RecordingStatus belongs exclusively to the recorder field manager.
type RecordingStatus struct {
	// ObservedGeneration binds these observations to the recorder's admitted source settings.
	ObservedGeneration int64 `json:"observedGeneration"`
	// PodUID identifies this recorder session; restarts do not imply continuous capture.
	PodUID types.UID `json:"podUID"`
	// HeartbeatAt timestamps the latest recorder status publication.
	HeartbeatAt metav1.Time `json:"heartbeatAt"`
	// BackendReady reports the latest acknowledged record export.
	BackendReady bool `json:"backendReady"`
	// Sources contains current-process source observations.
	// +listType=map
	// +listMapKey=name
	// +kubebuilder:validation:MaxItems=24
	Sources []SourceStatus `json:"sources,omitempty"`
}

// NetworkTelemetryStatus separates controller and recorder ownership.
type NetworkTelemetryStatus struct {
	// Admitted records initial exact-root admission; collection can outlive that root.
	Admitted bool `json:"admitted,omitempty"`
	// TablePrefix locates this recording's independently retained tables.
	TablePrefix string `json:"tablePrefix,omitempty"`
	// Conditions are written only by the workload controller.
	// +listType=map
	// +listMapKey=type
	// +kubebuilder:validation:MaxItems=4
	Conditions []metav1.Condition `json:"conditions,omitempty"`
	// Recording is written only by the recorder using server-side apply.
	Recording *RecordingStatus `json:"recording,omitempty"`
}

// NetworkTelemetry declares passive continuous collection; deletion removes collectors, not backend tables.
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=ntel
// +kubebuilder:printcolumn:name="Network",type=string,JSONPath=`.spec.networkName`
// +kubebuilder:printcolumn:name="Admitted",type=boolean,JSONPath=`.status.admitted`
// +kubebuilder:printcolumn:name="Backend",type=boolean,JSONPath=`.status.recording.backendReady`
type NetworkTelemetry struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              NetworkTelemetrySpec   `json:"spec"`
	Status            NetworkTelemetryStatus `json:"status,omitempty"`
}

// NetworkTelemetryList lists recordings.
// +kubebuilder:object:root=true
type NetworkTelemetryList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []NetworkTelemetry `json:"items"`
}
