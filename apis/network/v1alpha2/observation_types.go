package v1alpha2

// ObservationPolicy reports immutable release-level freshness and progress parameters.
type ObservationPolicy struct {
	// Profile identifies the release protocol profile.
	// +kubebuilder:validation:MaxLength=63
	Profile string `json:"profile"`
	// PollIntervalSeconds is the native observation cadence.
	// +kubebuilder:validation:Minimum=1
	PollIntervalSeconds int32 `json:"pollIntervalSeconds"`
	// RPCAllowanceSeconds bounds native read operations.
	// +kubebuilder:validation:Minimum=1
	RPCAllowanceSeconds int32 `json:"rpcAllowanceSeconds"`
	// HeartbeatIntervalSeconds bounds coalesced successful observation publication.
	// +kubebuilder:validation:Minimum=1
	HeartbeatIntervalSeconds int32 `json:"heartbeatIntervalSeconds"`
	// ProgressGraceSeconds is the minimum recent-progress horizon before the RPC allowance.
	// +kubebuilder:validation:Minimum=1
	ProgressGraceSeconds int32 `json:"progressGraceSeconds"`
}
