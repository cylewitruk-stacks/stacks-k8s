package production

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// TestTypedRPCSurface verifies method bounds, request IDs and static authentication.
func TestTypedRPCSurface(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, password, ok := r.BasicAuth()
		if !ok || user != "producer" || password != "password" {
			t.Error("missing producer authentication")
		}
		var request struct {
			ID     string `json:"id"`
			Method string `json:"method"`
			Params []any  `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			return
		}
		calls.Add(1)
		var result any
		switch request.Method {
		case "getblockchaininfo":
			result = map[string]string{"chain": "regtest"}
		case "validateaddress":
			result = map[string]bool{"isvalid": true}
		case "generatetoaddress":
			if len(request.Params) != 3 || request.Params[0] != float64(1) || request.Params[2] != float64(1000000) {
				t.Errorf("unbounded generation arguments: %#v", request.Params)
			}
			result = []string{fmt.Sprintf("%064x", 1)}
		default:
			t.Errorf("unexpected RPC method %s", request.Method)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"id": request.ID, "result": result})
	}))
	defer server.Close()
	rpc := NewBitcoinRPC(Credentials{Username: "producer", Password: "password"})
	if err := rpc.Check(context.Background(), server.URL, "address"); err != nil {
		t.Fatal(err)
	}
	if _, err := rpc.Generate(context.Background(), server.URL, "address", "dispatch-one"); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 3 {
		t.Fatalf("RPC calls=%d", calls.Load())
	}
}

// TestRPCDoesNotReplayLostResponsesOrRedirectCredentials exercises the real HTTP transport.
func TestRPCDoesNotReplayLostResponsesOrRedirectCredentials(t *testing.T) {
	for _, mode := range []string{"connection-loss", "timeout", "redirect", "wrong-id", "invalid-hash"} {
		t.Run(mode, func(t *testing.T) {
			var calls atomic.Int32
			receiver := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("redirect followed") }))
			defer receiver.Close()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				switch mode {
				case "connection-loss":
					connection, _, err := w.(http.Hijacker).Hijack()
					if err == nil {
						connection.Close()
					}
				case "timeout":
					time.Sleep(50 * time.Millisecond)
				case "redirect":
					http.Redirect(w, r, receiver.URL, http.StatusTemporaryRedirect)
				case "wrong-id":
					fmt.Fprintf(w, `{"id":"other","result":["%064x"]}`, 1)
				case "invalid-hash":
					fmt.Fprint(w, `{"id":"dispatch","result":["not-a-hash"]}`)
				}
			}))
			defer server.Close()
			rpc := NewBitcoinRPC(Credentials{Username: "producer", Password: "password"})
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
			defer cancel()
			if _, err := rpc.Generate(ctx, server.URL, "address", "dispatch"); err == nil {
				t.Fatal("incomplete receipt accepted")
			}
			if calls.Load() != 1 {
				t.Fatalf("request replayed: %d calls", calls.Load())
			}
		})
	}
}
