package rpc

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSourceAtPinsCanonicalReadAndDistinguishesAbsence(t *testing.T) {
	tip := strings.Repeat("a", 64)
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Query().Get("tip") != tip || r.URL.Query().Get("proof") != "0" {
			t.Error("source read lost canonical tip binding")
		}
		if strings.HasSuffix(r.URL.Path, "/missing") {
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, "No contract source data found")
			return
		}
		if strings.HasSuffix(r.URL.Path, "/missing-tip") {
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, "Chain tip not found")
			return
		}
		if strings.HasSuffix(r.URL.Path, "/unavailable") {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		fmt.Fprint(w, `{"source":"(define-read-only (test) true)"}`)
	}))
	defer server.Close()
	c, err := New(Config{Endpoint: server.URL, Timeout: time.Second, MaxResponseBytes: 4096})
	if err != nil {
		t.Fatal(err)
	}
	address := "ST000000000000000000002AMW42H"
	source, found, err := c.SourceAt(context.Background(), address, "present", tip)
	if err != nil || !found || source != "(define-read-only (test) true)" {
		t.Fatalf("source: %q %v %v", source, found, err)
	}
	_, found, err = c.SourceAt(context.Background(), address, "missing", tip)
	if err != nil || found {
		t.Fatal("absent contract not distinguished")
	}
	_, _, err = c.SourceAt(context.Background(), address, "unavailable", tip)
	if err == nil {
		t.Fatal("failed observation treated as absence")
	}
	_, _, err = c.SourceAt(context.Background(), address, "missing-tip", tip)
	if err == nil {
		t.Fatal("missing canonical tip treated as absent contract")
	}
	_, _, err = c.SourceAt(context.Background(), address, "present", "bad-tip")
	if err == nil || calls != 4 {
		t.Fatal("invalid tip dispatched")
	}
}
