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
		{"CadenceFixed", CadenceFixed, `"Fixed"`},
		{"CadenceUniform", CadenceUniform, `"Uniform"`},
		{"OfferBootstrap", OfferBootstrap, `"Bootstrap"`},
		{"OfferBaseline", OfferBaseline, `"Baseline"`},
		{"RPCCreateWallet", RPCCreateWallet, `"CreateWallet"`},
		{"RPCLoadWallet", RPCLoadWallet, `"LoadWallet"`},
		{"RPCImportDescriptor", RPCImportDescriptor, `"ImportDescriptor"`},
		{"RPCUnloadWallet", RPCUnloadWallet, `"UnloadWallet"`},
		{"RPCGenerate", RPCGenerate, `"Generate"`},
		{"RPCInvalidateBlock", RPCInvalidateBlock, `"InvalidateBlock"`},
		{"RPCReconsiderBlock", RPCReconsiderBlock, `"ReconsiderBlock"`},
		{"ExecutionIdle", ExecutionIdle, `"Idle"`},
		{"ExecutionArmed", ExecutionArmed, `"Armed"`},
		{"ExecutionBlocked", ExecutionBlocked, `"Blocked"`},
		{"ExecutionAbandoned", ExecutionAbandoned, `"Abandoned"`},
		{"InitializationWaiting", InitializationWaiting, `"Waiting"`},
		{"InitializationPreparing", InitializationPreparing, `"Preparing"`},
		{"InitializationPaused", InitializationPaused, `"Paused"`},
		{"InitializationHeld", InitializationHeld, `"Held"`},
		{"InitializationBlocked", InitializationBlocked, `"Blocked"`},
		{"InitializationAbandoned", InitializationAbandoned, `"Abandoned"`},
		{"DrainDrained", DrainDrained, `"Drained"`},
		{"DrainUncertain", DrainUncertain, `"Uncertain"`},
		{"ControlPaused", ControlPaused, `"Paused"`},
		{"BaselineSelected", BaselineSelected, `"Selected"`},
		{"BaselineOffered", BaselineOffered, `"Offered"`},
		{"BaselineAssigned", BaselineAssigned, `"Assigned"`},
		{"BaselineSkipped", BaselineSkipped, `"Skipped"`},
		{"BaselineUnassigned", BaselineUnassigned, `"Unassigned"`},
		{"OverridePending", OverridePending, `"Pending"`},
		{"OverrideActive", OverrideActive, `"Active"`},
		{"OverrideCompleted", OverrideCompleted, `"Completed"`},
		{"OverrideExpired", OverrideExpired, `"Expired"`},
		{"OverrideCancelled", OverrideCancelled, `"Cancelled"`},
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
