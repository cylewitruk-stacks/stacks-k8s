package v1alpha2

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// PoX5EnrollmentObservation reports exact canonical direct stake and manager authorization.
type PoX5EnrollmentObservation struct {
	// Holder is the non-custodial staker principal.
	// +kubebuilder:validation:MaxLength=64
	// +kubebuilder:validation:Pattern=`^ST[0-9A-HJKMNP-TV-Z]{26,39}$`
	Holder string `json:"holder"`
	// Manager is the administrator's exact direct-signer contract principal.
	// +kubebuilder:validation:MaxLength=80
	// +kubebuilder:validation:Pattern=`^ST[0-9A-HJKMNP-TV-Z]{26,39}\.direct-signer$`
	Manager string `json:"manager"`
	// ManagerSourceDigest hashes the exact source including its holder binding.
	// +kubebuilder:validation:Pattern=`^sha256:[0-9a-f]{64}$`
	ManagerSourceDigest string `json:"managerSourceDigest"`
	// SignerPublicKey is the registered, actively authorized compressed consensus key.
	// +kubebuilder:validation:Pattern=`^(02|03)[0-9a-f]{64}$`
	SignerPublicKey string `json:"signerPublicKey"`
	// AmountMicroSTX is the exact canonical uint128 stake.
	// +kubebuilder:validation:MaxLength=39
	// +kubebuilder:validation:Pattern=`^[1-9][0-9]*$`
	AmountMicroSTX string `json:"amountMicroSTX"`
	// DelegatedAmountMicroSTX is the manager's native delegated amount for TargetCycle.
	// +kubebuilder:validation:MaxLength=39
	// +kubebuilder:validation:Pattern=`^[1-9][0-9]*$`
	DelegatedAmountMicroSTX string `json:"delegatedAmountMicroSTX"`
	// FirstCycle starts the holder's current lock coverage.
	// +kubebuilder:validation:Minimum=0
	FirstCycle uint64 `json:"firstCycle"`
	// EndCycleExclusive is the first uncovered cycle.
	// +kubebuilder:validation:Minimum=1
	EndCycleExclusive uint64 `json:"endCycleExclusive"`
	// TargetCycle is the frozen bootstrap or current maintenance cycle.
	// +kubebuilder:validation:Minimum=0
	TargetCycle uint64 `json:"targetCycle"`
	// TargetCycleMatched requires exact holder membership and manager signer-set inclusion.
	TargetCycleMatched bool `json:"targetCycleMatched"`
	// UnlockHeight is the native account lock boundary, verified against reward-cycle conversion.
	// +kubebuilder:validation:Minimum=1
	UnlockHeight uint64 `json:"unlockHeight"`
	// StacksTip is the canonical index block ID used for every pinned read.
	// +kubebuilder:validation:Pattern=`^[0-9a-f]{64}$`
	StacksTip string `json:"stacksTip"`
	// BurnHeight is the processed Bitcoin height in the same canonical view.
	// +kubebuilder:validation:Minimum=0
	BurnHeight uint64 `json:"burnHeight"`
	// ObservedAt records when the complete canonical observation succeeded.
	ObservedAt metav1.Time `json:"observedAt"`
}
