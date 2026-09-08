package transactions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestRPCRequiresExactCanonicalSuccessfulInclusion(t *testing.T) {
	signed, _ := (fakeSigner{}).Sign(context.Background(), fixture(t).policy.Spec.Policy, 0)
	for _, mode := range []string{"success", "wrong-bytes", "noncanonical", "failed-result", "missing"} {
		t.Run(mode, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/v3/transaction/"+signed.TxID {
					t.Error("wrong transaction queried")
				}
				if mode == "missing" {
					w.WriteHeader(404)
					return
				}
				body := map[string]any{"tx": signed.Bytes, "index_block_hash": strings.Repeat("b", 64), "is_canonical": true, "result": "(ok true)"}
				if mode == "wrong-bytes" {
					body["tx"] = strings.Repeat("ff", 180)
				}
				if mode == "noncanonical" {
					body["is_canonical"] = false
				}
				if mode == "failed-result" {
					body["result"] = "(err u1)"
				}
				_ = json.NewEncoder(w).Encode(body)
			}))
			defer server.Close()
			got, err := NewNodeRPC().Inclusion(context.Background(), server.URL, signed.TxID)
			switch mode {
			case "success":
				if err != nil || !got.Found || !got.Success {
					t.Fatal("valid inclusion rejected")
				}
			case "wrong-bytes":
				if err == nil {
					t.Fatal("wrong transaction accepted")
				}
			case "noncanonical", "missing":
				if err != nil || got.Found {
					t.Fatal("absence treated as inclusion")
				}
			case "failed-result":
				if err != nil || !got.Found || got.Success {
					t.Fatal("failed transfer counted")
				}
			}
		})
	}
}

func TestRPCSubmissionDoesNotFollowRedirectOrReplayLostResponse(t *testing.T) {
	signed, _ := (fakeSigner{}).Sign(context.Background(), fixture(t).policy.Spec.Policy, 0)
	for _, mode := range []string{"success", "wrong-id", "redirect", "drop"} {
		t.Run(mode, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.ContentLength <= 0 || len(r.TransferEncoding) != 0 {
					t.Error("native Core requires a fixed-length transaction body")
				}
				body, _ := io.ReadAll(r.Body)
				if fmt.Sprintf("%x", body) != signed.Bytes {
					t.Error("different bytes submitted")
				}
				switch mode {
				case "redirect":
					w.Header().Set("Location", "/another")
					w.WriteHeader(307)
				case "drop":
					connection, _, _ := w.(http.Hijacker).Hijack()
					_ = connection.Close()
				case "wrong-id":
					_ = json.NewEncoder(w).Encode(strings.Repeat("f", 64))
				default:
					_ = json.NewEncoder(w).Encode(signed.TxID)
				}
			}))
			defer server.Close()
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			err := NewNodeRPC().Submit(ctx, server.URL, signed)
			if (err == nil) != (mode == "success") || calls.Load() != 1 {
				t.Fatalf("submission replay or incorrect receipt: %v / %d", err, calls.Load())
			}
		})
	}
}

func TestAccountPreflightRequiresIndexerAndExplicitNonce(t *testing.T) {
	for _, mode := range []string{"ready", "no-index", "missing-nonce", "mainnet"} {
		t.Run(mode, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if len(r.TransferEncoding) != 0 || r.ContentLength > 0 {
					t.Error("read-only request unexpectedly carried a body")
				}
				switch {
				case r.URL.Path == "/v2/info":
					network := uint32(0x80000000)
					if mode == "mainnet" {
						network = 1
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"network_id": network})
				case strings.HasPrefix(r.URL.Path, "/v3/transaction/"):
					if mode == "no-index" {
						w.WriteHeader(501)
					} else {
						w.WriteHeader(404)
					}
				default:
					body := map[string]any{"nonce": 0, "balance": "0x000000000000000000000000000f4240"}
					if mode == "missing-nonce" {
						delete(body, "nonce")
					}
					_ = json.NewEncoder(w).Encode(body)
				}
			}))
			defer server.Close()
			got, err := NewNodeRPC().Account(context.Background(), server.URL, "ST-sender")
			if (err == nil) != (mode == "ready") {
				t.Fatalf("preflight %s: %v", mode, err)
			}
			if mode == "ready" && (got.Nonce != 0 || got.Balance != 1000000) {
				t.Fatal("incorrect canonical account state")
			}
		})
	}
}

func TestSubmissionClassifiesOnlyMatchedNativeRejection(t *testing.T) {
	signed, _ := (fakeSigner{}).Sign(context.Background(), fixture(t).policy.Spec.Policy, 0)
	for _, mode := range []string{"FeeTooLow", "BadNonce", "unknown-reason", "wrong-id", "wrong-error", "missing-reason", "malformed", "server-error"} {
		t.Run(mode, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				body := map[string]any{"txid": signed.TxID, "error": "transaction rejected", "reason": "FeeTooLow", "reason_data": map[string]string{"message": "private upstream detail"}}
				code := http.StatusBadRequest
				switch mode {
				case "BadNonce":
					body["reason"] = mode
				case "unknown-reason":
					body["reason"] = "private upstream detail"
				case "wrong-id":
					body["txid"] = strings.Repeat("f", 64)
				case "wrong-error":
					body["error"] = "upstream proxy error"
				case "missing-reason":
					delete(body, "reason")
				case "server-error":
					code = 500
				}
				w.WriteHeader(code)
				if mode == "malformed" {
					_, _ = io.WriteString(w, "incomplete JSON")
					return
				}
				_ = json.NewEncoder(w).Encode(body)
			}))
			defer server.Close()
			err := NewNodeRPC().Submit(context.Background(), server.URL, signed)
			var rejection *submissionRejection
			classified := errors.As(err, &rejection)
			expected := mode == "FeeTooLow" || mode == "BadNonce" || mode == "unknown-reason"
			if err == nil || classified != expected || calls.Load() != 1 {
				t.Fatalf("incorrect rejection classification: %v", err)
			}
			if strings.Contains(err.Error(), "private upstream detail") {
				t.Fatal("raw server detail leaked")
			}
			if classified {
				want := mode
				if mode == "unknown-reason" {
					want = "Other"
				}
				if rejection.reason != want {
					t.Fatalf("reason = %s, want %s", rejection.reason, want)
				}
			}
		})
	}
}
