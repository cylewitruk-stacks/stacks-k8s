package rpc

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestNativeCallPreservesEnvelopeAndAuthentication(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, password, ok := r.BasicAuth()
		var body struct {
			ID     string `json:"id"`
			Method string `json:"method"`
			Params []any  `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if !ok || user != "observer" || password != "private" || body.ID != "receipt" ||
			body.Method != "getblockchaininfo" ||
			body.Params == nil {
			t.Error("wire contract changed")
		}
		_, _ = io.WriteString(w, `{"id":"receipt","result":{"blocks":3},"error":null}`)
	}))
	defer server.Close()
	var result struct {
		Blocks int `json:"blocks"`
	}
	if err := New(
		Credentials{Username: "observer", Password: "private"},
	).Call(t.Context(), server.URL, "receipt", "getblockchaininfo", nil, &result); err != nil ||
		result.Blocks != 3 {
		t.Fatal(err, result)
	}
}

func TestMutationDoesNotFollowRedirectOrReplayLostResponse(t *testing.T) {
	for _, redirect := range []bool{false, true} {
		var calls atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls.Add(1)
			if redirect {
				w.Header().Set("Location", "/other")
				w.WriteHeader(http.StatusTemporaryRedirect)
				return
			}
			_, _ = io.Copy(io.Discard, r.Body)
			connection, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Error(err)
				return
			}
			_ = connection.Close()
		}))
		_, err := New(Credentials{}).Generate(t.Context(), server.URL, "address", "receipt")
		server.Close()
		if err == nil || calls.Load() != 1 {
			t.Fatal("mutation was accepted or replayed", err, calls.Load())
		}
	}
}

func TestCallHonorsCallerDeadline(t *testing.T) {
	server := httptest.NewServer(
		http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			_, _ = io.Copy(io.Discard, r.Body)
			<-r.Context().Done()
		}),
	)
	defer server.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	var result any
	err := New(Credentials{}).Call(ctx, server.URL, "id", "getblockchaininfo", nil, &result)
	if err == nil || ctx.Err() != context.DeadlineExceeded || strings.Contains(err.Error(), server.URL) {
		t.Fatal("deadline/redaction changed", err)
	}
}
