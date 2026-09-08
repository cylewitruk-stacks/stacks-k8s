package v1alpha1

// GenerationCadence defines one finite mechanism's timing; the first block is immediately eligible.
// +kubebuilder:validation:XValidation:rule="has(self.intervalSeconds) == (self.mode == 'Fixed')",message="intervalSeconds is required only for Fixed"
// +kubebuilder:validation:XValidation:rule="has(self.minSeconds) == (self.mode == 'Uniform') && has(self.maxSeconds) == (self.mode == 'Uniform')",message="minSeconds and maxSeconds are required only for Uniform"
// +kubebuilder:validation:XValidation:rule="self.mode != 'Uniform' || self.minSeconds <= self.maxSeconds",message="uniform bounds must be ordered"
// +kubebuilder:validation:XValidation:rule="!has(self.delaysSeconds) || self.mode == 'Explicit'",message="delaysSeconds is allowed only for Explicit"
type GenerationCadence struct {
	// Mode selects immediate, fixed, inclusive uniform, or explicit inter-block delays.
	// +kubebuilder:validation:Enum=Immediate;Fixed;Uniform;Explicit
	// +kubebuilder:validation:MaxLength=9
	Mode string `json:"mode"`
	// IntervalSeconds is the fixed receipt-relative delay.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=60
	IntervalSeconds int32 `json:"intervalSeconds,omitempty"`
	// MinSeconds is the inclusive lower uniform bound.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=60
	MinSeconds int32 `json:"minSeconds,omitempty"`
	// MaxSeconds is the inclusive upper uniform bound.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=60
	MaxSeconds int32 `json:"maxSeconds,omitempty"`
	// DelaysSeconds contains count minus one ordered delays; zero adds no intentional wait.
	// +kubebuilder:validation:MaxItems=99
	// +kubebuilder:validation:items:Minimum=0
	// +kubebuilder:validation:items:Maximum=60
	// +listType=atomic
	DelaysSeconds []int32 `json:"delaysSeconds,omitempty"`
}
