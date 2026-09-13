package rpc

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/cylewitruk-stacks/stacks-k8s/libs/stacks/transaction"
)

func TestSubmissionRejectionRequiresExactNativeEnvelope(t *testing.T) {
	tx := transaction.Transaction{Bytes: []byte{1, 2, 3}}
	tx.TxID = transaction.ID(tx.Bytes)
	for _, test := range []struct {
		name                      string
		status                    int
		id, message, reason, want string
	}{
		{"fee", 400, "0x" + tx.TxID, "transaction rejected", "FeeTooLow", "FeeTooLow"},
		{"nonce", 400, tx.TxID, "transaction rejected", "BadNonce", "BadNonce"},
		{
			"conflict",
			400,
			tx.TxID,
			"transaction rejected",
			"ConflictingNonceInMempool",
			"ConflictingNonceInMempool",
		},
		{"funds", 400, tx.TxID, "transaction rejected", "NotEnoughFunds", "NotEnoughFunds"},
		{"database", 400, tx.TxID, "transaction rejected", "ServerFailureDatabase", "Other"},
		{"other", 400, tx.TxID, "transaction rejected", "private server detail", "Other"},
		{"proxy", 403, tx.TxID, "transaction rejected", "BadNonce", ""},
		{"server", 500, tx.TxID, "transaction rejected", "BadNonce", ""},
		{"success", 200, tx.TxID, "transaction rejected", "BadNonce", ""},
		{"mismatched", 400, strings.Repeat("a", 64), "transaction rejected", "BadNonce", ""},
		{"id-case", 400, strings.ToUpper(tx.TxID), "transaction rejected", "BadNonce", ""},
		{"missing-id", 400, "", "transaction rejected", "BadNonce", ""},
		{"wrong-error", 400, tx.TxID, "rejected", "BadNonce", ""},
		{"missing-reason", 400, tx.TxID, "transaction rejected", "", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			data, _ := json.Marshal(
				map[string]any{
					"txid":        test.id,
					"error":       test.message,
					"reason":      test.reason,
					"reason_data": map[string]string{"secret": "private server detail"},
				},
			)
			calls := 0
			c := clientFor(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				raw, _ := io.ReadAll(r.Body)
				if hex.EncodeToString(raw) != "010203" {
					t.Error("submitted bytes changed")
				}
				w.WriteHeader(test.status)
				_, _ = w.Write(data)
			})
			err := c.Submit(context.Background(), tx)
			var rejection *SubmissionRejection
			matched := errors.As(err, &rejection)
			if err == nil || matched != (test.want != "") || calls != 1 {
				t.Fatalf("classification %v, calls %d", err, calls)
			}
			if matched &&
				(rejection.Reason() != test.want ||
					rejection.TxID() != tx.TxID ||
					rejection.Definite() != (test.want != "Other")) {
				t.Fatalf("wrong retained identity/classification: %+v", rejection)
			}
			if strings.Contains(err.Error(), "private") {
				t.Fatal("raw rejection leaked")
			}
		})
	}
	for _, raw := range []string{
		"{",
		"null",
		`{"reason":1}`,
		`{"txid":"` + tx.TxID + `","error":"transaction rejected","reason":"BadNonce"} trailing`,
	} {
		if ClassifySubmissionRejection(400, []byte(raw), tx.TxID) != nil {
			t.Fatal("malformed rejection classified")
		}
	}
}

func TestOversizedRejectionRemainsUnknown(t *testing.T) {
	tx := transaction.Transaction{Bytes: []byte{1, 2, 3}}
	tx.TxID = transaction.ID(tx.Bytes)
	c := clientFor(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(400)
		_, _ = io.WriteString(w, `{"txid":"`+tx.TxID+`","error":"transaction rejected","reason":"BadNonce"}`)
	})
	c.maxBytes = 10
	err := c.Submit(context.Background(), tx)
	var rejection *SubmissionRejection
	if err == nil || errors.As(err, &rejection) {
		t.Fatal("truncated response classified as definite rejection")
	}
}
