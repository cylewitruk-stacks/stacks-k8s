package bitcoinobserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	bitcoinrpc "github.com/cylewitruk-stacks/stacks-k8s/libs/bitcoin/rpc"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// rpcFixture serves real HTTP envelopes and records methods without exposing credentials in failure messages.
func rpcFixture(t *testing.T, mode *atomic.Int32, calls *atomic.Int32) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		user, pass, ok := r.BasicAuth()
		if !ok || user != "observer" || pass != "private-password" {
			t.Error("wrong principal")
		}
		var request struct {
			ID     string `json:"id"`
			Method string `json:"method"`
		}
		if json.NewDecoder(r.Body).Decode(&request) != nil {
			t.Error("invalid request")
			return
		}
		if mode.Load() == 1 {
			http.Error(w, "private-server-detail", http.StatusForbidden)
			return
		}
		if mode.Load() == 2 {
			_, _ = w.Write([]byte(strings.Repeat("x", 65537)))
			return
		}
		var result any
		switch request.Method {
		case "getblockchaininfo":
			result = map[string]any{
				"chain":                "regtest",
				"blocks":               7,
				"headers":              7,
				"bestblockhash":        strings.Repeat("a", 64),
				"chainwork":            strings.Repeat("0", 63) + "e",
				"initialblockdownload": false,
			}
			if mode.Load() == 10 {
				result.(map[string]any)["chain"] = "main"
			}
			if mode.Load() == 3 {
				delete(result.(map[string]any), "blocks")
			}
		case "getchaintips":
			result = []any{
				map[string]any{"height": 7, "hash": strings.Repeat("a", 64), "branchlen": 0, "status": "active"},
			}
			if mode.Load() == 8 || mode.Load() == 11 {
				count := 129
				if mode.Load() == 11 {
					count = 128
				}
				entries := make([]any, count)
				for i := range entries {
					entries[i] = result.([]any)[0]
				}
				result = entries
			}
			if mode.Load() == 9 {
				result.([]any)[0].(map[string]any)["status"] = "unsupported-private-status"
			}
		case "getnetworkinfo":
			result = map[string]any{"connections": 2}
		case "getmempoolinfo":
			result = map[string]any{"size": 0, "bytes": 0, "usage": 100}
		case "getnettotals":
			result = map[string]any{"totalbytesrecv": 1000, "totalbytessent": 2000, "connections": -99, "size": -99}
		default:
			t.Error("unexpected RPC method")
			http.Error(w, "unsupported", 400)
			return
		}
		id := request.ID
		if mode.Load() == 4 {
			id = "wrong"
		}
		if mode.Load() == 5 {
			result = nil
		}
		if mode.Load() == 7 && request.Method == "getchaintips" {
			result = []any{map[string]any{"hash": strings.Repeat("a", 64), "status": "active"}}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
	}))
}

// scrape collects the public Prometheus representation without making an RPC request.
func scrape(t *testing.T, o *Observer) string {
	t.Helper()
	registry := prometheus.NewRegistry()
	if err := registry.Register(o); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	promhttp.HandlerFor(registry, promhttp.HandlerOpts{}).
		ServeHTTP(w, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/metrics", nil))
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
	return w.Body.String()
}

func TestObserverFailureFreshnessAndRecovery(t *testing.T) {
	var mode, calls atomic.Int32
	server := rpcFixture(t, &mode, &calls)
	defer server.Close()
	o, err := New(
		bitcoinrpc.New(bitcoinrpc.Credentials{Username: "observer", Password: "private-password"}),
		server.URL,
		10*time.Second,
	)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1000, 0)
	o.now = func() time.Time { return now }
	before := scrape(t, o)
	if strings.Contains(before, "bitcoin_block_height") || calls.Load() != 0 {
		t.Fatal("scrape polled or invented protocol state")
	}
	for _, bad := range []int32{1, 2, 3, 4, 5, 7, 8, 9, 10} {
		mode.Store(0)
		record := poll(t, o)
		if !record.Success || len(record.Tips) != 1 || record.Chain.Blocks != 7 {
			t.Fatalf("good sample: %+v", record)
		}
		body := scrape(t, o)
		if !strings.Contains(body, "bitcoin_block_height 7") || strings.Contains(body, strings.Repeat("a", 64)) ||
			!strings.Contains(body, "bitcoin_connections 2") ||
			!strings.Contains(body, "bitcoin_mempool_transactions 0") {
			t.Fatal("missing height or hash label")
		}
		count := calls.Load()
		_ = scrape(t, o)
		if calls.Load() != count {
			t.Fatal("scrape called RPC")
		}
		now = now.Add(16 * time.Second)
		if strings.Contains(scrape(t, o), "bitcoin_block_height") {
			t.Fatal("stale gauge retained")
		}
		mode.Store(bad)
		record = poll(t, o)
		expected := map[int32]string{
			1:  "response",
			2:  "bound",
			3:  "validation",
			4:  "response",
			5:  "validation",
			7:  "validation",
			8:  "bound",
			9:  "validation",
			10: "validation",
		}
		if string(record.FailedReason) != expected[bad] {
			t.Fatalf("mode %d: reason=%q", bad, record.FailedReason)
		}
		data, _ := json.Marshal(record)
		if record.Success || record.Chain != nil || len(record.Tips) > 0 || strings.Contains(string(data), "private") {
			t.Fatalf("failed sample: %s", data)
		}
		body = scrape(t, o)
		if strings.Contains(body, "bitcoin_block_height") || !strings.Contains(body, "bitcoin_observer_success 0") ||
			!strings.Contains(body, "bitcoin_observer_last_success_timestamp_seconds") {
			t.Fatal("failure freshened gauges")
		}
	}
	mode.Store(0)
	if !poll(t, o).Success || !strings.Contains(scrape(t, o), "bitcoin_observer_success 1") {
		t.Fatal("recovery failed")
	}
}

func TestObserverBoundsAndCancellation(t *testing.T) {
	var mode, calls atomic.Int32
	server := rpcFixture(t, &mode, &calls)
	defer server.Close()
	rpc := bitcoinrpc.New(bitcoinrpc.Credentials{Username: "observer", Password: "private-password"})
	for _, interval := range []time.Duration{0, 4 * time.Second, 301 * time.Second} {
		if _, err := New(rpc, server.URL, interval); err == nil {
			t.Fatal("invalid interval accepted")
		}
	}
	o, _ := New(rpc, server.URL, 5*time.Second)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { defer close(done); o.Run(ctx, func(Record) { cancel() }) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("poll loop did not stop")
	}
	if calls.Load() != 5 {
		t.Fatalf("poll calls=%d", calls.Load())
	}
}

func TestObserverConcurrentScrapes(t *testing.T) {
	var mode, calls atomic.Int32
	server := rpcFixture(t, &mode, &calls)
	defer server.Close()
	o, _ := New(
		bitcoinrpc.New(bitcoinrpc.Credentials{Username: "observer", Password: "private-password"}),
		server.URL,
		5*time.Second,
	)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range 10 {
			poll(t, o)
		}
	}()
	for range 10 {
		_ = scrape(t, o)
	}
	<-done
}

// poll asserts that an ordinary sample completes rather than being abandoned at shutdown.
func poll(t *testing.T, o *Observer) Record {
	t.Helper()
	record, err := o.Poll(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	return record
}
