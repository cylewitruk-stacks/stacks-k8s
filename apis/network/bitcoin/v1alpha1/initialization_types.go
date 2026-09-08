package v1alpha1

// RegtestInitialization declares idempotent wallet prerequisites and initial chain advancement.
type RegtestInitialization struct {
	// Target names the production target whose wallet funds the initial miner.
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern=`^[a-z0-9](?:[-a-z0-9]{0,61}[a-z0-9])?$`
	Target string `json:"target"`
	// Wallet names the watch-only descriptor wallet used by Stacks miners.
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern=`^[a-z0-9](?:[-a-z0-9]{0,61}[a-z0-9])?$`
	Wallet string `json:"wallet"`
	// InitialHeight is the initial regtest chain floor, not an ongoing height target.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=201
	InitialHeight int64 `json:"initialHeight"`
}
