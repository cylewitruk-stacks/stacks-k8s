package bitcoinobserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/bitcoinrpc"
	"github.com/prometheus/client_golang/prometheus"
)

// rpcFunc exercises timing and cancellation at the same boundary used by the HTTP client.
type rpcFunc func(context.Context, string, string, string, []any, any) error

func (f rpcFunc) Call(ctx context.Context, endpoint, id, method string, params []any, result any) error {
	return f(ctx, endpoint, id, method, params, result)
}

func TestCancellationDuringRPCDoesNotPublishOrCount(t *testing.T) {
	var mode, calls atomic.Int32
	server := rpcFixture(t, &mode, &calls)
	defer server.Close()
	transport := bitcoinrpc.New(bitcoinrpc.Credentials{Username: "observer", Password: "private-password"})
	entered := make(chan struct{})
	blocking := false
	o, err := New(rpcFunc(func(ctx context.Context, endpoint, id, method string, params []any, result any) error {
		if !blocking {
			return transport.Call(ctx, endpoint, id, method, params, result)
		}
		close(entered)
		<-ctx.Done()
		return ctx.Err()
	}), server.URL, DefaultInterval)
	if err != nil {
		t.Fatal(err)
	}
	poll(t, o)
	before := scrape(t, o)
	oldRecord := o.record
	blocking = true
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	var emitted atomic.Int32
	go func() { defer close(done); o.Run(ctx, func(Record) { emitted.Add(1) }) }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("RPC did not start")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("shutdown did not finish")
	}
	if emitted.Load() != 0 || !reflect.DeepEqual(o.record, oldRecord) || scrape(t, o) != before {
		t.Fatal("shutdown changed published facts or collection counters")
	}
	if _, err := o.Poll(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled parent: %v", err)
	}
}

func TestSharedDeadlineReportsFailedStepWithoutAttributingCause(t *testing.T) {
	var mode, calls atomic.Int32
	server := rpcFixture(t, &mode, &calls)
	defer server.Close()
	transport := bitcoinrpc.New(bitcoinrpc.Credentials{Username: "observer", Password: "private-password"})
	var budgetAtSecond time.Duration
	o, err := New(rpcFunc(func(ctx context.Context, endpoint, id, method string, params []any, result any) error {
		if method == "getchaintips" {
			deadline, ok := ctx.Deadline()
			if !ok {
				return fmt.Errorf("no poll deadline")
			}
			budgetAtSecond = time.Until(deadline)
			<-ctx.Done()
			return ctx.Err()
		}
		// The first response consumes most of the shared budget while still succeeding.
		if method == "getblockchaininfo" {
			timer := time.NewTimer(PollTimeout - 500*time.Millisecond)
			defer timer.Stop()
			select {
			case <-timer.C:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return transport.Call(ctx, endpoint, id, method, params, result)
	}), server.URL, DefaultInterval)
	if err != nil {
		t.Fatal(err)
	}
	record := poll(t, o)
	if record.Success || record.FailedMethod != "getchaintips" || record.FailedReason != "deadline" ||
		budgetAtSecond > time.Second || record.Chain != nil || calls.Load() != 1 {
		t.Fatalf("deadline classification or early-stop failed: %+v calls=%d", record, calls.Load())
	}
	if o.failures != 1 || !strings.Contains(scrape(t, o), "bitcoin_observer_poll_errors_total 1") {
		t.Fatal("poll deadline not counted")
	}
}

func TestCompleteSampleBoundsAndMetricVocabulary(t *testing.T) {
	var mode, calls atomic.Int32
	server := rpcFixture(t, &mode, &calls)
	defer server.Close()
	o, err := New(
		bitcoinrpc.New(bitcoinrpc.Credentials{Username: "observer", Password: "private-password"}),
		server.URL,
		DefaultInterval,
	)
	if err != nil {
		t.Fatal(err)
	}
	mode.Store(11)
	if record := poll(t, o); !record.Success || len(record.Tips) != 128 {
		t.Fatal("maximum complete inventory rejected")
	}
	registry := prometheus.NewPedanticRegistry()
	if err := registry.Register(o); err != nil {
		t.Fatal(err)
	}
	families, err := registry.Gather()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"bitcoin_observer_success": "GAUGE", "bitcoin_observer_last_success_timestamp_seconds": "GAUGE",
		"bitcoin_observer_last_attempt_timestamp_seconds": "GAUGE", "bitcoin_observer_poll_errors_total": "COUNTER",
		"bitcoin_block_height": "GAUGE", "bitcoin_header_height": "GAUGE", "bitcoin_initial_block_download": "GAUGE",
		"bitcoin_chain_tips": "GAUGE", "bitcoin_connections": "GAUGE", "bitcoin_mempool_transactions": "GAUGE",
		"bitcoin_mempool_bytes": "GAUGE", "bitcoin_mempool_usage_bytes": "GAUGE",
		"bitcoin_network_received_bytes_total": "COUNTER", "bitcoin_network_sent_bytes_total": "COUNTER",
	}
	got := map[string]string{}
	for _, family := range families {
		got[family.GetName()] = family.GetType().String()
		if len(family.Metric) != 1 || len(family.Metric[0].Label) != 0 {
			t.Fatal("unexpected metric series or labels")
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("metric contract: %v", got)
	}
	mode.Store(8)
	before := calls.Load()
	record := poll(t, o)
	if record.Success || record.FailedReason != "bound" || record.FailedMethod != "getchaintips" ||
		calls.Load()-before != 2 {
		t.Fatal("branch rejection did not stop the complete sample")
	}
	families, err = registry.Gather()
	if err != nil {
		t.Fatal(err)
	}
	if len(families) != 4 {
		t.Fatal("partial protocol metrics escaped")
	}
	data, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "bestblockhash") {
		t.Fatal("partial protocol record escaped")
	}
}

func TestTransportAndResponseFailuresRemainRedacted(t *testing.T) {
	// A refused connection is transport; an HTTP rejection is response, regardless of private text.
	for _, status := range []int{0, http.StatusForbidden} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, "private-server-detail", status)
			}))
			defer server.Close()
			if status == 0 {
				server.Close()
			}
			o, err := New(bitcoinrpc.New(bitcoinrpc.Credentials{}), server.URL, DefaultInterval)
			if err != nil {
				t.Fatal(err)
			}
			record := poll(t, o)
			expected := FailureResponse
			if status == 0 {
				expected = FailureTransport
			}
			if record.FailedReason != expected {
				t.Fatalf("wrong failure: %+v", record)
			}
			data, _ := json.Marshal(record)
			if strings.Contains(string(data), "private") {
				t.Fatal("server error escaped")
			}
		})
	}
}

// TestRequestAndUnknownFailuresDoNotClaimResponseOrTransportFaults pins vocabulary at the RPC boundary.
func TestRequestAndUnknownFailuresDoNotClaimResponseOrTransportFaults(t *testing.T) {
	for _, test := range []struct {
		name, endpoint, reason string
		rpc                    RPC
	}{
		{"request", "http://invalid\nendpoint", "request", bitcoinrpc.New(bitcoinrpc.Credentials{})},
		{
			"unknown-error", "http://unused", "unknown",
			rpcFunc(func(context.Context, string, string, string, []any, any) error {
				return errors.New("private unclassified error")
			}),
		},
		{"unknown-kind", "http://unused", "unknown", rpcFunc(func(context.Context, string, string, string, []any, any) error {
			return &bitcoinrpc.CallError{Kind: bitcoinrpc.FailureKind("future")}
		})},
	} {
		t.Run(test.name, func(t *testing.T) {
			o, err := New(test.rpc, test.endpoint, DefaultInterval)
			if err != nil {
				t.Fatal(err)
			}
			record := poll(t, o)
			if record.Success || string(record.FailedReason) != test.reason ||
				record.FailedMethod != "getblockchaininfo" {
				t.Fatalf("incorrect classification: %+v", record)
			}
			data, err := json.Marshal(record)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(data), "private") || strings.Contains(string(data), "endpoint") {
				t.Fatal("raw diagnostic escaped")
			}
		})
	}
}
