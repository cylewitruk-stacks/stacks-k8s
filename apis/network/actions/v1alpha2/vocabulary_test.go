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
		{"PhasePending", PhasePending, `"Pending"`},
		{"PhaseAdmitted", PhaseAdmitted, `"Admitted"`},
		{"PhaseActive", PhaseActive, `"Active"`},
		{"PhaseRecovering", PhaseRecovering, `"Recovering"`},
		{"PhaseCompleted", PhaseCompleted, `"Completed"`},
		{"PhaseRecovered", PhaseRecovered, `"Recovered"`},
		{"PhaseFailed", PhaseFailed, `"Failed"`},
		{"PhaseInconclusive", PhaseInconclusive, `"Inconclusive"`},
		{"CadenceImmediate", CadenceImmediate, `"Immediate"`},
		{"CadenceFixed", CadenceFixed, `"Fixed"`},
		{"CadenceUniform", CadenceUniform, `"Uniform"`},
		{"CadenceExplicit", CadenceExplicit, `"Explicit"`},
		{"KindBitcoinBlockGeneration", KindBitcoinBlockGeneration, `"BitcoinBlockGeneration"`},
		{"KindBitcoinReorganization", KindBitcoinReorganization, `"BitcoinReorganization"`},
		{"ReasonIdentityDiverged", ReasonIdentityDiverged, `"IdentityDiverged"`},
		{"ReasonEffectUncertain", ReasonEffectUncertain, `"EffectUncertain"`},
		{"ReasonCleanupDeadlineExceeded", ReasonCleanupDeadlineExceeded, `"CleanupDeadlineExceeded"`},
		{"ReasonActionCancelled", ReasonActionCancelled, `"ActionCancelled"`},
		{"ReasonActionDeadlineExceeded", ReasonActionDeadlineExceeded, `"ActionDeadlineExceeded"`},
		{"ReasonMechanismFailed", ReasonMechanismFailed, `"MechanismFailed"`},
		{"ConditionAdmitted", ConditionAdmitted, `"Admitted"`},
		{"ConditionCleanupComplete", ConditionCleanupComplete, `"CleanupComplete"`},
		{"ConditionEffectObserved", ConditionEffectObserved, `"EffectObserved"`},
		{"ConditionProgressing", ConditionProgressing, `"Progressing"`},
		{"ReasonAdmissionObserved", ReasonAdmissionObserved, `"AdmissionObserved"`},
		{"ReasonAdmissionUnavailable", ReasonAdmissionUnavailable, `"AdmissionUnavailable"`},
		{"ReasonCleanupEvidence", ReasonCleanupEvidence, `"CleanupEvidence"`},
		{"ReasonEnvironmentDisposed", ReasonEnvironmentDisposed, `"EnvironmentDisposed"`},
		{"ReasonExecutionUnavailable", ReasonExecutionUnavailable, `"ExecutionUnavailable"`},
		{"ReasonNoDispatch", ReasonNoDispatch, `"NoDispatch"`},
		{"ReasonPending", ReasonPending, `"Pending"`},
		{"ReasonReceiptEvidence", ReasonReceiptEvidence, `"ReceiptEvidence"`},
		{"ReasonReceiptsComplete", ReasonReceiptsComplete, `"ReceiptsComplete"`},
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
