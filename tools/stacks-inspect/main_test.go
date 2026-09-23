package main

import (
	"bytes"
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
)

const (
	tipA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	tipB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	tipC = "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
)

// invoke decodes independent JSON records and never imports the production wire shape.
func invoke(t *testing.T, kind string, r request) ([]map[string]any, string, error) {
	t.Helper()
	input, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	err = run(context.Background(), []string{kind}, bytes.NewReader(input), &output)
	d := json.NewDecoder(bytes.NewReader(output.Bytes()))
	var records []map[string]any
	for d.More() {
		var v map[string]any
		if e := d.Decode(&v); e != nil {
			t.Fatal(e)
		}
		records = append(records, v)
	}
	return records, output.String(), err
}

func TestBitcoinPinsAncestryAndReportsPartialReads(t *testing.T) {
	for _, failBlock := range []bool{false, true} {
		t.Run(fmt.Sprint(failBlock), func(t *testing.T) {
			var methods []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var q struct {
					ID     string            `json:"id"`
					Method string            `json:"method"`
					Params []json.RawMessage `json:"params"`
				}
				if r.Method != "POST" || json.NewDecoder(r.Body).Decode(&q) != nil {
					t.Error("unexpected request")
					w.WriteHeader(500)
					return
				}
				methods = append(methods, q.Method)
				var result any
				switch q.Method {
				case "getblockchaininfo":
					result = map[string]any{"chain": "regtest", "blocks": 2, "bestblockhash": tipA}
				case "getblockheader":
					var h string
					_ = json.Unmarshal(q.Params[0], &h)
					height := 2
					previous := tipB
					if h == tipB {
						height = 1
						previous = tipC
					}
					if h == tipC {
						height = 0
						previous = ""
					}
					result = map[string]any{
						"hash":              h,
						"height":            height,
						"previousblockhash": previous,
						"merkleroot":        tipC,
						"time":              123,
					}
				case "getblock":
					if failBlock {
						w.WriteHeader(500)
						_, _ = w.Write([]byte("PRIVATE"))
						return
					}
					var h string
					_ = json.Unmarshal(q.Params[0], &h)
					result = map[string]any{"hash": h, "tx": []string{tipC}}
				default:
					t.Errorf("mutation or unexpected RPC %s", q.Method)
					w.WriteHeader(500)
					return
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"id": q.ID, "result": result})
			}))
			defer server.Close()
			records, raw, err := invoke(
				t,
				"bitcoin",
				request{
					Endpoint:       server.URL,
					Attribution:    "test/pod-uid",
					TimeoutSeconds: 3,
					Blocks:         2,
					CompareTip:     tipB,
				},
			)
			if (err != nil) != failBlock || len(methods) != 5 {
				t.Fatalf("err=%v methods=%v", err, methods)
			}
			if strings.Contains(raw, "PRIVATE") || strings.Contains(raw, server.URL) {
				t.Fatal("private data exposed")
			}
			var relation string
			for _, r := range records {
				if r["kind"] == "ancestry" {
					relation = r["facts"].(map[string]any)["relation"].(string)
				}
			}
			if relation != "comparison-is-ancestor" {
				t.Fatal(relation)
			}
			if records[len(records)-1]["facts"].(map[string]any)["complete"] == failBlock {
				t.Fatal("wrong completion")
			}
		})
	}
}

func TestStacksReadsShareExactTipAndKeepNativeUnavailable(t *testing.T) {
	tip := ""
	sets := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Error("non-read method")
		}
		switch r.URL.Path {
		case "/v2/info":
			_, _ = fmt.Fprintf(
				w,
				`{"network_id":2147483648,
"burn_block_height":379,
"stacks_tip_height":12,
"stacks_tip":"%s",
"stacks_tip_consensus_hash":"%s",
"pox_consensus":"%s",
"is_fully_synced":true}`,
				tipA,
				strings.Repeat("b", 40),
				strings.Repeat("c", 40),
			)
		case "/v2/pox":
			tip = r.URL.Query().Get("tip")
			_, _ = fmt.Fprint(
				w,
				`{"contract_id":"ST000000000000000000002AMW42H.pox-5",
"current_burnchain_block_height":379,
"reward_cycle_id":18,
"reward_cycle_length":20,
"next_cycle":{"min_threshold_ustx":"123",
"blocks_until_prepare_phase":-2}}`,
			)
		default:
			sets++
			if len(tip) != 64 || r.URL.Query().Get("tip") != tip {
				t.Error("unpinned read")
			}
			w.WriteHeader(400)
			_, _ = fmt.Fprint(
				w,
				`{"err_type":"not_available_try_again",
"err_msg":"Could not read reward set. Prepare phase may not have started for this cycle yet. `+
					`Err = PoXAnchorBlockRequired",
"private":"SECRET"}`,
			)
		}
	}))
	defer server.Close()
	records, raw, err := invoke(t, "stacks", request{Endpoint: server.URL, Attribution: "actor", TimeoutSeconds: 3})
	if err != nil || sets != 2 || len(records) != 5 || !strings.Contains(raw, "PoXAnchorBlockRequired") ||
		strings.Contains(raw, "SECRET") {
		t.Fatalf("%v %s", err, raw)
	}
	for _, record := range records {
		if record["kind"] == "reward-set" {
			facts := record["facts"].(map[string]any)
			if len(facts) != 4 || facts["available"] != false || facts["prepared"] != nil ||
				facts["unavailableReason"] != "PoXAnchorBlockRequired" {
				t.Fatal(facts)
			}
		}
	}
}

func TestValidationAndCancellationNeverMutate(t *testing.T) {
	for _, kind := range []string{"stacks", "bitcoin"} {
		var out bytes.Buffer
		if err := run(
			context.Background(),
			[]string{kind},
			strings.NewReader(`{"endpoint":"http://user:password@localhost","attribution":"x","timeoutSeconds":1}`),
			&out,
		); err == nil ||
			out.Len() != 0 {
			t.Fatal("invalid request accepted")
		}
	}
	server := httptest.NewServer(
		http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { <-r.Context().Done() }),
	)
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var out bytes.Buffer
	input := fmt.Sprintf(`{"endpoint":%q,"attribution":"actor","timeoutSeconds":1}`, server.URL)
	if err := run(
		ctx,
		[]string{"stacks"},
		strings.NewReader(input),
		&out,
	); err == nil ||
		!strings.Contains(out.String(), `"complete":false`) {
		t.Fatal(err, out.String())
	}
}

func TestFailedReadsRetainRequestedIdentityAndRejectPrivateGenesisField(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var q struct {
			ID     string `json:"id"`
			Method string `json:"method"`
		}
		_ = json.NewDecoder(r.Body).Decode(&q)
		result := map[string]any{"chain": "regtest", "blocks": 0, "bestblockhash": tipA}
		if q.Method == "getblockheader" {
			result = map[string]any{
				"hash":              tipA,
				"height":            0,
				"time":              0,
				"merkleroot":        tipB,
				"previousblockhash": "PRIVATE",
			}
		}
		if q.Method != "getblockchaininfo" && q.Method != "getblockheader" {
			t.Error("unexpected RPC", q.Method)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"id": q.ID, "result": result})
	}))
	defer server.Close()
	records, raw, err := invoke(
		t,
		"bitcoin",
		request{Endpoint: server.URL, Attribution: "actor", TimeoutSeconds: 2, Blocks: 1},
	)
	if err == nil || strings.Contains(raw, "PRIVATE") {
		t.Fatal(err, raw)
	}
	for _, r := range records {
		if r["kind"] == "bitcoin-header" {
			if r["subject"].(map[string]any)["blockHash"] != tipA || r["facts"] != nil {
				t.Fatal(r)
			}
		}
	}
}

func TestInspectRejectsOversizedDocumentWithoutReadingProtocol(t *testing.T) {
	var out bytes.Buffer
	if err := run(
		context.Background(),
		[]string{"stacks"},
		strings.NewReader("{}"+strings.Repeat(" ", 65535)+"{}"),
		&out,
	); err == nil ||
		!strings.Contains(err.Error(), "byte bound") {
		t.Fatal(err)
	}
}

func TestBitcoinRelationsAndTransactionBounds(t *testing.T) {
	fork := strings.Repeat("d", 64)
	for _, tt := range []struct {
		name, tip, compare string
		blocks             int
		relation           string
		calls              int
	}{
		{"same", tipA, tipA, 1, "same-tip", 3},
		{"comparison-ancestor", tipA, tipB, 2, "comparison-is-ancestor", 5},
		{"primary-ancestor", tipB, tipA, 2, "primary-is-ancestor", 7},
		{"fork", tipA, fork, 3, "shared-ancestor", 9},
		{"bounded", tipA, fork, 1, "unknown-within-bound", 5},
	} {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				var q struct {
					ID     string            `json:"id"`
					Method string            `json:"method"`
					Params []json.RawMessage `json:"params"`
				}
				_ = json.NewDecoder(r.Body).Decode(&q)
				var result any
				switch q.Method {
				case "getblockchaininfo":
					result = map[string]any{"chain": "regtest", "blocks": 2, "bestblockhash": tipA}
				case "getblockheader":
					var h string
					_ = json.Unmarshal(q.Params[0], &h)
					height, previous := 2, tipB
					if h == tipB {
						height, previous = 1, tipC
					}
					if h == tipC {
						height, previous = 0, ""
					}
					result = map[string]any{
						"hash":              h,
						"height":            height,
						"previousblockhash": previous,
						"merkleroot":        tipC,
						"time":              123,
					}
				case "getblock":
					var h string
					_ = json.Unmarshal(q.Params[0], &h)
					ids := make([]string, 129)
					for i := range ids {
						ids[i] = fmt.Sprintf("%064x", i+1)
					}
					result = map[string]any{"hash": h, "tx": ids}
				default:
					t.Error("unexpected RPC", q.Method)
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"id": q.ID, "result": result})
			}))
			defer server.Close()
			records, _, err := invoke(
				t,
				"bitcoin",
				request{
					Endpoint:       server.URL,
					Attribution:    "actor",
					TimeoutSeconds: 3,
					Blocks:         tt.blocks,
					Tip:            tt.tip,
					CompareTip:     tt.compare,
				},
			)
			if calls != tt.calls {
				t.Fatalf("RPC calls=%d want=%d", calls, tt.calls)
			}
			if err != nil {
				t.Fatal(err)
			}
			relation := ""
			summaries := 0
			for _, r := range records {
				if r["kind"] == "ancestry" {
					relation = r["facts"].(map[string]any)["relation"].(string)
				}
				if r["kind"] == "bitcoin-transactions" {
					summaries++
					facts := r["facts"].(map[string]any)
					if facts["count"] != float64(129) || facts["truncated"] != true ||
						len(facts["txids"].([]any)) != 128 {
						t.Fatal(facts)
					}
				}
			}
			if relation != tt.relation || summaries != (tt.calls-1)/2 {
				t.Fatal(relation, summaries)
			}
		})
	}
}

func TestFailedRewardReadsRetainCyclesAndInferredOverflowStops(t *testing.T) {
	for _, overflow := range []bool{false, true} {
		t.Run(fmt.Sprint(overflow), func(t *testing.T) {
			sets := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/v2/info":
					_ = json.NewEncoder(w).
						Encode(map[string]any{
							"network_id":        2147483648,
							"burn_block_height": 379, "stacks_tip_height": 12, "stacks_tip": tipA,
							"stacks_tip_consensus_hash": strings.Repeat("b", 40),
							"pox_consensus":             strings.Repeat("c", 40), "is_fully_synced": true,
						})
				case "/v2/pox":
					cycle := uint64(18)
					if overflow {
						cycle = ^uint64(0)
					}
					_ = json.NewEncoder(w).
						Encode(map[string]any{
							"contract_id":                    "ST000000000000000000002AMW42H.pox-5",
							"current_burnchain_block_height": 379,
							"reward_cycle_id":                cycle,
							"reward_cycle_length":            20,
							"next_cycle": map[string]any{
								"min_threshold_ustx":         "123",
								"blocks_until_prepare_phase": -2,
							},
						})
				default:
					sets++
					w.WriteHeader(500)
					_, _ = w.Write([]byte("PRIVATE"))
				}
			}))
			defer server.Close()
			records, raw, err := invoke(
				t,
				"stacks",
				request{Endpoint: server.URL, Attribution: "actor", TimeoutSeconds: 2},
			)
			if err == nil || strings.Contains(raw, "PRIVATE") {
				t.Fatal(err, raw)
			}
			if overflow {
				if sets != 0 {
					t.Fatal("overflow sent reward-set read")
				}
				return
			}
			cycles := map[float64]bool{}
			for _, r := range records {
				if r["kind"] == "reward-set" {
					subject := r["subject"].(map[string]any)
					cycles[subject["cycle"].(float64)] = true
					if len(subject["indexBlockID"].(string)) != 64 || r["error"] == nil || r["facts"] != nil {
						t.Fatal(r)
					}
				}
			}
			if sets != 2 || !cycles[18] || !cycles[19] {
				t.Fatal(sets, cycles)
			}
		})
	}
}

// brokenOutput models a full or disconnected evidence sink.
type brokenOutput struct{ err error }

func (w brokenOutput) Write([]byte) (int, error) { return 0, w.err }

func TestInspectStopsOnOutputFailureAndBoundsResponseAndActiveRead(t *testing.T) {
	for _, mode := range []string{"output", "oversized", "deadline"} {
		t.Run(mode, func(t *testing.T) {
			var calls atomic.Int32
			release := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				switch mode {
				case "deadline":
					select {
					case <-r.Context().Done():
					case <-release:
					}
				case "oversized":
					_, _ = w.Write([]byte(strings.Repeat("x", 65537)))
				default:
					_ = json.NewEncoder(w).
						Encode(map[string]any{
							"id":     "chain",
							"result": map[string]any{"chain": "regtest", "blocks": 2, "bestblockhash": tipA},
						})
				}
			}))
			defer server.Close()
			defer close(release)
			input := fmt.Sprintf(`{"endpoint":%q,"attribution":"actor","timeoutSeconds":1,"blocks":1}`, server.URL)
			var output bytes.Buffer
			var sink io.Writer = &output
			marker := errors.New("evidence sink full")
			if mode == "output" {
				sink = brokenOutput{marker}
			}
			err := run(context.Background(), []string{"bitcoin"}, strings.NewReader(input), sink)
			if err == nil || calls.Load() != 1 {
				t.Fatal(err, calls.Load())
			}
			if mode == "deadline" && !errors.Is(err, context.DeadlineExceeded) {
				t.Fatal(err)
			}
			if mode == "output" && !errors.Is(err, marker) {
				t.Fatal(err)
			}
			if mode != "output" && !strings.Contains(output.String(), `"complete":false`) {
				t.Fatal(output.String())
			}
		})
	}
}
