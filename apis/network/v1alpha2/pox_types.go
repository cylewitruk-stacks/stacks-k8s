package v1alpha2

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// TransactionPostcondition records desired-state convergence without attributing it to a TxID.
// Another writer sharing the key may have established the same state.
type TransactionPostcondition struct {
	// TxID references the pending offered transaction; it does not assert inclusion.
	// +kubebuilder:validation:Pattern=`^[0-9a-f]{64}$`
	TxID string `json:"txID"`
	// Kind names the bounded operation whose exact public state was observed.
	// +kubebuilder:validation:Enum=PoX4Enrollment;PoX4Extension;ContractDeployment;RegistryInitialization;ManagerDeployment;SignerRegistration;PoX5Enrollment;PoX5Extension
	Kind string `json:"kind"`
	// StateDigest identifies the verified public state, excluding observation time.
	// +kubebuilder:validation:Pattern=`^sha256:[0-9a-f]{64}$`
	StateDigest string `json:"stateDigest"`
	// StacksTip is the canonical index block ID used for every pinned native read.
	// +kubebuilder:validation:Pattern=`^[0-9a-f]{64}$`
	StacksTip string `json:"stacksTip"`
	// ObservedAt preserves the successful state observation across API retries.
	ObservedAt metav1.Time `json:"observedAt"`
}

// PoX4EnrollmentObservation reports exact current native enrollment independently of inclusion.
type PoX4EnrollmentObservation struct {
	// Holder identifies the directly locked account.
	// +kubebuilder:validation:MaxLength=64
	// +kubebuilder:validation:Pattern=`^ST[0-9A-HJKMNP-TV-Z]{26,39}$`
	Holder string `json:"holder"`
	// SignerPublicKey is the compressed consensus key in the target reward-set entry.
	// +kubebuilder:validation:Pattern=`^(02|03)[0-9a-f]{64}$`
	SignerPublicKey string `json:"signerPublicKey"`
	// AmountMicroSTX is the exact uint128 native stake, encoded as canonical decimal.
	// +kubebuilder:validation:MaxLength=39
	// +kubebuilder:validation:Pattern=`^(0|[1-9][0-9]*)$`
	AmountMicroSTX string `json:"amountMicroSTX"`
	// FirstCycle starts the currently recorded reward coverage.
	// +kubebuilder:validation:Minimum=0
	FirstCycle uint64 `json:"firstCycle"`
	// EndCycleExclusive is the first uncovered reward cycle.
	// +kubebuilder:validation:Minimum=1
	EndCycleExclusive uint64 `json:"endCycleExclusive"`
	// TargetCycle is the frozen initial cycle or current maintenance observation cycle.
	// +kubebuilder:validation:Minimum=0
	TargetCycle uint64 `json:"targetCycle"`
	// TargetCycleMatched requires exact holder, signer, payout and stake in the reward-set entry.
	TargetCycleMatched bool `json:"targetCycleMatched"`
	// StacksTip is the canonical index block ID bracketing state and nonce reads.
	// +kubebuilder:validation:Pattern=`^[0-9a-f]{64}$`
	StacksTip string `json:"stacksTip"`
	// BurnHeight is the canonical processed Bitcoin height during observation.
	// +kubebuilder:validation:Minimum=0
	BurnHeight uint64 `json:"burnHeight"`
	// ObservedAt records the successful bracketed observation time.
	ObservedAt metav1.Time `json:"observedAt"`
}
