// Package stacksrpc decodes native Stacks RPC response facts without execution policy.
package stacksrpc

import (
	"encoding/json"
	"net/http"
	"strings"
)

// RejectionReason classifies only a native rejection tied to the submitted TxID.
// An empty result means unconfirmed; a class does not establish permanent non-execution.
func RejectionReason(status int, data []byte, txid string) string {
	if status != http.StatusBadRequest || len(txid) != 64 {
		return ""
	}
	var rejection struct {
		TxID   string `json:"txid"`
		Error  string `json:"error"`
		Reason string `json:"reason"`
	}
	if json.Unmarshal(data, &rejection) != nil || strings.TrimPrefix(rejection.TxID, "0x") != txid || rejection.Error != "transaction rejected" || rejection.Reason == "" {
		return ""
	}
	switch rejection.Reason {
	case "FeeTooLow", "BadNonce":
		return rejection.Reason
	default:
		return "Other"
	}
}
