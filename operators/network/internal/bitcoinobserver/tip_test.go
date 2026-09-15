package bitcoinobserver

import (
	"encoding/json"
	"testing"
)

// TestTipDecoderRequiresFieldsButAcceptsZero ensures a single decode retains validation semantics.
func TestTipDecoderRequiresFieldsButAcceptsZero(t *testing.T) {
	complete := map[string]any{"height": 0, "branchlen": 0, "hash": "hash", "status": "active"}
	for _, key := range []string{"height", "branchlen", "hash", "status"} {
		for _, null := range []bool{false, true} {
			t.Run(key, func(t *testing.T) {
				value := map[string]any{}
				for k, v := range complete {
					value[k] = v
				}
				if null {
					value[key] = nil
				} else {
					delete(value, key)
				}
				data, err := json.Marshal(value)
				if err != nil {
					t.Fatal(err)
				}
				var tip Tip
				if err := json.Unmarshal(data, &tip); err == nil {
					t.Fatal("accepted absent or null tip field")
				}
			})
		}
	}
	var tip Tip
	if err := json.Unmarshal(
		[]byte(`{"height":0,"branchlen":0,"hash":"hash","status":"active"}`),
		&tip,
	); err != nil || tip.Height != 0 ||
		tip.BranchLength != 0 {
		t.Fatalf("zero rejected: %+v %v", tip, err)
	}
}
