package rpc

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCallErrorClassificationPreservesRedaction(t *testing.T) {
	for _, test := range []struct {
		name    string
		status  int
		body    string
		kind    FailureKind
		message string
	}{
		{"http", 403, "private", FailureResponse, "RPC HTTP status 403"},
		{"size", 200, strings.Repeat("x", 65537), FailureBound, "RPC receipt unreadable or oversized"},
		{
			"envelope", 200, `{"id":"wrong","result":"private"}`,
			FailureResponse, "RPC did not return a matching success receipt",
		},
		{
			"rpc-error", 200, `{"id":"test","error":{"message":"private"}}`,
			FailureResponse, "RPC did not return a matching success receipt",
		},
		{"decode", 200, `{"id":"test","result":"private"}`, FailureResponse, "RPC result could not be decoded"},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(test.status)
				_, _ = fmt.Fprint(w, test.body)
			}))
			defer server.Close()
			var result int
			err := New(Credentials{}).Call(t.Context(), server.URL, "test", MethodGetBlockchainInfo, nil, &result)
			var typed *CallError
			if !errors.As(err, &typed) || typed.Kind != test.kind || err.Error() != test.message ||
				strings.Contains(err.Error(), "private") {
				t.Fatalf("classification/diagnostic changed: %v", err)
			}
			if errors.Unwrap(err) != nil {
				t.Fatal("raw cause retained")
			}
		})
	}
}

func TestTransportClassificationDoesNotExposeEndpoint(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	var result any
	err := New(Credentials{}).Call(ctx, "http://private-endpoint.invalid", "id", MethodGetBlockchainInfo, nil, &result)
	var typed *CallError
	if !errors.As(err, &typed) || typed.Kind != FailureTransport ||
		err.Error() != "RPC transport did not yield a receipt" {
		t.Fatalf("transport error: %v", err)
	}
}
