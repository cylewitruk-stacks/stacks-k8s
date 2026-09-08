package stacksrpc

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRejectionRequiresMatchedNativeEnvelope(t *testing.T) {
	id := strings.Repeat("a", 64)
	for _, tc := range []struct {
		name                      string
		status                    int
		id, message, reason, want string
	}{
		{"fee", 400, "0x" + id, "transaction rejected", "FeeTooLow", "FeeTooLow"},
		{"nonce", 400, id, "transaction rejected", "BadNonce", "BadNonce"},
		{"other", 400, id, "transaction rejected", "ConflictingNonceInMempool", "Other"},
		{"arbitrary-reason", 400, id, "transaction rejected", "private\nserver detail", "Other"},
		{"proxy", 403, id, "transaction rejected", "BadNonce", ""},
		{"server", 500, id, "transaction rejected", "BadNonce", ""},
		{"success-code", 200, id, "transaction rejected", "BadNonce", ""},
		{"wrong-id", 400, strings.Repeat("b", 64), "transaction rejected", "BadNonce", ""},
		{"missing-id", 400, "", "transaction rejected", "BadNonce", ""},
		{"wrong-message", 400, id, "rejected", "BadNonce", ""},
		{"missing-reason", 400, id, "transaction rejected", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data, _ := json.Marshal(map[string]any{"txid": tc.id, "error": tc.message, "reason": tc.reason, "reason_data": map[string]any{"secret": "discard"}})
			if got := RejectionReason(tc.status, data, id); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
	for _, data := range []string{"{", "null", `{"reason":1}`, `{"txid":"` + id + `","error":"transaction rejected","reason":"BadNonce"} trailing`} {
		if got := RejectionReason(400, []byte(data), id); got != "" {
			t.Fatalf("malformed rejection classified: %q", got)
		}
	}
}
