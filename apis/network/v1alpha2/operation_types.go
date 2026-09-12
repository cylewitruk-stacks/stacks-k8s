package v1alpha2

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// TransactionExecutionStatus reports bounded cumulative facts from one worker session.
type TransactionExecutionStatus struct {
	// Offered counts signed transactions whose one submission attempt began.
	Offered uint64 `json:"offered"`
	// Accepted counts exact transaction acknowledgements, without claiming inclusion.
	Accepted uint64 `json:"accepted"`
	// Included counts exact transactions observed in canonical blocks.
	Included uint64 `json:"included"`
	// Rejected counts matching allowlisted native validation refusals.
	Rejected uint64 `json:"rejected"`
	// Uncertain counts attempts without an exact acknowledgement or definite validation refusal.
	Uncertain uint64 `json:"uncertain"`
	// PostconditionObserved counts exact desired-state/next-nonce observations without TxID attribution.
	// +kubebuilder:validation:Minimum=0
	PostconditionObserved uint64 `json:"postconditionObserved,omitempty"`
	// LastPostcondition retains the latest desired-state observation, never transaction inclusion.
	LastPostcondition *TransactionPostcondition `json:"lastPostcondition,omitempty"`
	// LastTxID identifies the most recent submission attempt.
	// +kubebuilder:validation:Pattern=`^[0-9a-f]{64}$`
	LastTxID string `json:"lastTxID,omitempty"`
	// LastInclusion identifies the most recently observed canonical inclusion.
	LastInclusion *TransactionInclusion `json:"lastInclusion,omitempty"`
}

// TransactionInclusion records a canonical observation, not irreversible finality.
type TransactionInclusion struct {
	// TxID identifies the exact submitted transaction.
	// +kubebuilder:validation:Pattern=`^[0-9a-f]{64}$`
	TxID string `json:"txID"`
	// BlockID identifies the canonical block observed by the target node.
	// +kubebuilder:validation:Pattern=`^[0-9a-f]{64}$`
	BlockID string `json:"blockID"`
	// Success reports the transaction execution result.
	Success bool `json:"success"`
	// ObservedAt preserves the original observation time across status-write retries.
	ObservedAt metav1.Time `json:"observedAt"`
}
