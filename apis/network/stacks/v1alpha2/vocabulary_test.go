package v1alpha2

import (
	"encoding/json"
	"testing"
)

// TestVocabularyWireValues pins serialized contracts independently of controller call sites.
func TestVocabularyWireValues(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value any
		wire  string
	}{
		{"FaucetDecisionPending", FaucetDecisionPending, `"Pending"`},
		{"FaucetDecisionAdmitted", FaucetDecisionAdmitted, `"Admitted"`},
		{"FaucetDecisionRejected", FaucetDecisionRejected, `"Rejected"`},
		{"FaucetDecisionExpired", FaucetDecisionExpired, `"Expired"`},
		{"FaucetExecutionSubmitted", FaucetExecutionSubmitted, `"Submitted"`},
		{"FaucetExecutionCompleted", FaucetExecutionCompleted, `"Completed"`},
		{"FaucetExecutionRejected", FaucetExecutionRejected, `"Rejected"`},
		{"FaucetExecutionExpired", FaucetExecutionExpired, `"Expired"`},
		{"FaucetExecutionInconclusive", FaucetExecutionInconclusive, `"Inconclusive"`},
		{"FaucetPending", FaucetPending, `"Pending"`},
		{"FaucetSubmitted", FaucetSubmitted, `"Submitted"`},
		{"FaucetCompleted", FaucetCompleted, `"Completed"`},
		{"FaucetRejected", FaucetRejected, `"Rejected"`},
		{"FaucetInconclusive", FaucetInconclusive, `"Inconclusive"`},
		{"FaucetExpired", FaucetExpired, `"Expired"`},
		{"ConditionCompleted", ConditionCompleted, `"Completed"`},
		{"RejectionReasonPrefix", RejectionReasonPrefix, `"Rejected"`},
		{"RegistryInitializationExplicitTestRegistry", RegistryInitializationExplicitTestRegistry, `"ExplicitTestRegistry"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := json.Marshal(tc.value)
			if err != nil {
				t.Fatal(err)
			}
			if string(raw) != tc.wire {
				t.Fatalf("wire value %s, want %s", raw, tc.wire)
			}
		})
	}
}
