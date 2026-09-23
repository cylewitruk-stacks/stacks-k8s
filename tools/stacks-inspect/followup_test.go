package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/libs/bitcoin/rpc"
)

// Explicit cycles replace inference and public JSON never exposes library codec internals.
func TestExplicitCyclesAndPublicStacksFacts(t *testing.T) {
	const key = "0279be667ef9dcbbac55a06295ce870b07029bfcdb2dce28d959f2815b16f81798"
	const amount = "340282366920938463463374607431768211455"
	const threshold = "9007199254740993"
	var paths []string
	tip := ""
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if r.Method != "GET" {
			t.Error("non-read")
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
			_, _ = fmt.Fprintf(
				w,
				`{"contract_id":"ST000000000000000000002AMW42H.pox-5",
"current_burnchain_block_height":379,
"reward_cycle_id":18,
"reward_cycle_length":20,
"next_cycle":{"min_threshold_ustx":"%s",
"blocks_until_prepare_phase":-2}}`,
				threshold,
			)
		case "/v3/stacker_set/5":
			if len(tip) != 64 || r.URL.Query().Get("tip") != tip {
				t.Error("unpinned read")
			}
			_, _ = fmt.Fprintf(
				w,
				`{"stacker_set":{"reward_set_version":0,
"signers":[{"signing_key":"%s",
"weight":7,
"stacked_amt":%s}],
"pox_ustx_threshold":%s}}`,
				key,
				amount,
				threshold,
			)
		default:
			t.Error("unexpected inferred cycle", r.URL.Path)
			w.WriteHeader(500)
		}
	}))
	defer server.Close()
	records, raw, err := invoke(
		t,
		"stacks",
		request{Endpoint: server.URL, Attribution: "actor", TimeoutSeconds: 3, Cycles: []uint64{5}},
	)
	if err != nil {
		t.Fatal(err, raw)
	}
	if !reflect.DeepEqual(paths, []string{"/v2/info", "/v2/pox", "/v3/stacker_set/5"}) || len(records) != 4 {
		t.Fatal(paths, raw)
	}
	var want map[string]any
	expected := fmt.Sprintf(
		`{"tip":"%s",
"cycle":5,
"available":true,
"prepared":{"version":0,
"signers":[{"publicKey":"%s",
"weight":7,
"stackedAmountMicroSTX":"%s"}],
"thresholdMicroSTX":"%s"}}`,
		tip,
		key,
		amount,
		threshold,
	)
	if err := json.Unmarshal([]byte(expected), &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(records[2]["facts"], want) {
		t.Fatal("reward wire shape", records[2])
	}
	expected = fmt.Sprintf(
		`{"tip":"%s",
"pox":{"contract":"ST000000000000000000002AMW42H.pox-5",
"burnHeight":379,
"rewardCycle":18,
"cycleLength":20,
"minThresholdMicroSTX":"%s",
"blocksUntilPrepare":-2}}`,
		tip,
		threshold,
	)
	want = nil
	if err := json.Unmarshal([]byte(expected), &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(records[1]["facts"], want) {
		t.Fatal("PoX wire shape", records[1])
	}
	facts := records[0]["facts"].(map[string]any)
	if len(facts) != 8 || facts["indexBlockID"] != tip || facts["fullySynced"] != true ||
		facts["networkID"] != float64(2147483648) {
		t.Fatal(facts)
	}
	// The same completion rule applies to Stacks reads, including inferred work left over.
	for _, cycles := range [][]uint64{{5}, {5, 6}} {
		paths = nil
		ctx, cancel := context.WithCancel(t.Context())
		out := &cancelOutput{cancel: cancel, kind: "reward-set"}
		input, e := json.Marshal(request{Endpoint: server.URL, Attribution: "actor", TimeoutSeconds: 3, Cycles: cycles})
		if e != nil {
			t.Fatal(e)
		}
		e = run(ctx, []string{"stacks"}, bytes.NewReader(input), out)
		cancel()
		if len(paths) != 3 {
			t.Fatal(paths)
		}
		if len(cycles) == 1 {
			if e != nil || !strings.Contains(out.String(), `"complete":true`) {
				t.Fatal(e, out.String())
			}
		} else if !errors.Is(e, context.Canceled) || !strings.Contains(out.String(), `"complete":false`) {
			t.Fatal(e, out.String())
		}
	}
}

// A known parent is reusable only when the comparison's height link agrees.
func TestKnownBitcoinIntersectionRejectsInconsistentHeight(t *testing.T) {
	height := uint64(1)
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var q struct {
			ID     string `json:"id"`
			Method string `json:"method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&q); err != nil {
			t.Error(err)
		}
		var result any
		switch q.Method {
		case "getblockheader":
			result = map[string]any{
				"hash":              tipA,
				"height":            3,
				"previousblockhash": tipB,
				"merkleroot":        tipC,
				"time":              123,
			}
		case "getblock":
			result = map[string]any{"hash": tipA, "tx": []string{tipC}}
		default:
			t.Error("duplicate read", q.Method)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"id": q.ID, "result": result})
	}))
	defer server.Close()
	var output bytes.Buffer
	w := &writer{encoder: json.NewEncoder(&output)}
	seen, complete, err := walk(
		t.Context(),
		bitcoin.New(bitcoin.Credentials{}),
		request{Endpoint: server.URL, Blocks: 2},
		w,
		tipA,
		map[string]blockHeader{tipB: {Hash: tipB, Height: &height}},
	)
	if err == nil || complete || calls != 2 || len(seen) != 1 ||
		!strings.Contains(output.String(), `"error":"invalid hash-linked header intersection"`) {
		t.Fatal(err, calls, seen, output.String())
	}
}

// Interrupted comparison walks cannot support a bounded ancestry claim.
func TestBitcoinOmitsAncestryAfterComparisonFailure(t *testing.T) {
	for _, cancelAfterPrimary := range []bool{false, true} {
		t.Run(fmt.Sprint(cancelAfterPrimary), func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				var q struct {
					ID     string            `json:"id"`
					Method string            `json:"method"`
					Params []json.RawMessage `json:"params"`
				}
				if err := json.NewDecoder(r.Body).Decode(&q); err != nil {
					t.Error(err)
				}
				if q.Method == "getblockheader" && !cancelAfterPrimary {
					var hash string
					if err := json.Unmarshal(q.Params[0], &hash); err != nil {
						t.Error(err)
					}
					if hash == tipB {
						w.WriteHeader(http.StatusInternalServerError)
						return
					}
				}
				var result any
				switch q.Method {
				case "getblockchaininfo":
					result = map[string]any{"chain": "regtest", "blocks": 2, "bestblockhash": tipA}
				case "getblockheader":
					result = map[string]any{
						"hash": tipA, "height": 2, "previousblockhash": tipC,
						"merkleroot": tipC, "time": 123,
					}
				case "getblock":
					result = map[string]any{"hash": tipA, "tx": []string{tipC}}
				default:
					t.Error("unexpected RPC", q.Method)
				}
				if err := json.NewEncoder(w).Encode(map[string]any{"id": q.ID, "result": result}); err != nil {
					t.Error(err)
				}
			}))
			defer server.Close()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			out := &cancelOutput{cancel: cancel, kind: ""}
			if cancelAfterPrimary {
				out.kind = "bitcoin-transactions"
			}
			input, err := json.Marshal(request{
				Endpoint: server.URL, Attribution: "actor", TimeoutSeconds: 3,
				Blocks: 1, Tip: tipA, CompareTip: tipB,
			})
			if err != nil {
				t.Fatal(err)
			}
			err = run(ctx, []string{"bitcoin"}, bytes.NewReader(input), out)
			wantCalls := 4
			if cancelAfterPrimary {
				wantCalls = 3
			}
			if err == nil || calls != wantCalls || strings.Contains(out.String(), `"kind":"ancestry"`) ||
				!strings.Contains(out.String(), `"complete":false`) {
				t.Fatal(err, calls, out.String())
			}
			if cancelAfterPrimary && !errors.Is(err, context.Canceled) {
				t.Fatal("cancellation lost", err)
			}
		})
	}
}

// A failed primary header still permits bounded comparison evidence, but no ancestry claim.
func TestBitcoinOmitsAncestryAfterPrimaryHeaderFailure(t *testing.T) {
	var methods []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var q struct {
			ID     string            `json:"id"`
			Method string            `json:"method"`
			Params []json.RawMessage `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&q); err != nil {
			t.Error(err)
		}
		methods = append(methods, q.Method)
		var hash string
		if len(q.Params) > 0 {
			if err := json.Unmarshal(q.Params[0], &hash); err != nil {
				t.Error(err)
			}
		}
		if q.Method == "getblockheader" && hash == tipA {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		var result any
		switch q.Method {
		case "getblockchaininfo":
			result = map[string]any{"chain": "regtest", "blocks": 2, "bestblockhash": tipA}
		case "getblockheader":
			result = map[string]any{
				"hash": tipB, "height": 1, "previousblockhash": tipC,
				"merkleroot": tipC, "time": 123,
			}
		case "getblock":
			result = map[string]any{"hash": tipB, "tx": []string{tipC}}
		default:
			t.Error("unexpected RPC", q.Method)
		}
		if err := json.NewEncoder(w).Encode(map[string]any{"id": q.ID, "result": result}); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()
	records, raw, err := invoke(t, "bitcoin", request{
		Endpoint: server.URL, Attribution: "actor", TimeoutSeconds: 3,
		Blocks: 1, Tip: tipA, CompareTip: tipB,
	})
	if err == nil || !reflect.DeepEqual(methods, []string{
		"getblockchaininfo", "getblockheader", "getblockheader", "getblock",
	}) || strings.Contains(raw, `"kind":"ancestry"`) {
		t.Fatal(err, methods, raw)
	}
	if records[1]["subject"].(map[string]any)["blockHash"] != tipA || records[1]["facts"] != nil ||
		records[len(records)-1]["facts"].(map[string]any)["complete"] != false {
		t.Fatal(raw)
	}
}

// A failed comparison summary does not discard ancestry proven by both header walks.
func TestBitcoinComparisonSummaryFailureRetainsSharedAncestor(t *testing.T) {
	var calls []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var q struct {
			ID     string            `json:"id"`
			Method string            `json:"method"`
			Params []json.RawMessage `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&q); err != nil {
			t.Error(err)
		}
		var hash string
		if len(q.Params) > 0 {
			if err := json.Unmarshal(q.Params[0], &hash); err != nil {
				t.Error(err)
			}
		}
		calls = append(calls, q.Method+":"+hash)
		if q.Method == "getblock" && hash == tipB {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("PRIVATE"))
			return
		}
		var result any
		switch q.Method {
		case "getblockchaininfo":
			result = map[string]any{"chain": "regtest", "blocks": 1, "bestblockhash": tipA}
		case "getblockheader":
			height, previous := 1, tipC
			if hash == tipC {
				height, previous = 0, ""
			}
			result = map[string]any{
				"hash": hash, "height": height, "previousblockhash": previous,
				"merkleroot": tipC, "time": 123,
			}
		case "getblock":
			result = map[string]any{"hash": hash, "tx": []string{tipC}}
		default:
			t.Error("unexpected RPC", q.Method)
		}
		if err := json.NewEncoder(w).Encode(map[string]any{"id": q.ID, "result": result}); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()
	records, raw, err := invoke(t, "bitcoin", request{
		Endpoint: server.URL, Attribution: "actor", TimeoutSeconds: 3,
		Blocks: 2, Tip: tipA, CompareTip: tipB,
	})
	wantCalls := []string{
		"getblockchaininfo:",
		"getblockheader:" + tipA, "getblock:" + tipA,
		"getblockheader:" + tipC, "getblock:" + tipC,
		"getblockheader:" + tipB, "getblock:" + tipB,
	}
	if err == nil || !reflect.DeepEqual(calls, wantCalls) ||
		strings.Contains(raw, "PRIVATE") || strings.Contains(raw, server.URL) {
		t.Fatal(err, calls, raw)
	}
	if len(records) != 9 || records[6]["kind"] != "bitcoin-transactions" ||
		records[6]["subject"].(map[string]any)["blockHash"] != tipB || records[6]["facts"] != nil ||
		records[7]["kind"] != "ancestry" ||
		records[7]["facts"].(map[string]any)["relation"] != "shared-ancestor" ||
		records[8]["facts"].(map[string]any)["complete"] != false {
		t.Fatal(raw)
	}
}

// cancelOutput cancels after a chosen completed record, without failing its write.
type cancelOutput struct {
	bytes.Buffer
	cancel context.CancelFunc
	kind   string
}

// Write represents cancellation racing just after successful evidence persistence.
func (w *cancelOutput) Write(p []byte) (int, error) {
	n, err := w.Buffer.Write(p)
	if strings.Contains(string(p), `"kind":"`+w.kind+`"`) {
		w.cancel()
	}
	return n, err
}

// Cancellation after all requested work succeeds is different from unfinished work.
func TestCancellationAccountsActualCompletedWork(t *testing.T) {
	for _, blocks := range []int{1, 2} {
		t.Run(fmt.Sprint(blocks), func(t *testing.T) {
			calls := 0
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				var q struct {
					ID     string `json:"id"`
					Method string `json:"method"`
				}
				_ = json.NewDecoder(r.Body).Decode(&q)
				var result any
				switch q.Method {
				case "getblockchaininfo":
					result = map[string]any{"chain": "regtest", "blocks": 2, "bestblockhash": tipA}
				case "getblockheader":
					result = map[string]any{
						"hash":              tipA,
						"height":            2,
						"previousblockhash": tipB,
						"merkleroot":        tipC,
						"time":              123,
					}
				case "getblock":
					result = map[string]any{"hash": tipA, "tx": []string{tipC}}
				default:
					t.Error(q.Method)
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"id": q.ID, "result": result})
			}))
			defer s.Close()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			out := &cancelOutput{cancel: cancel, kind: "bitcoin-transactions"}
			input, err := json.Marshal(
				request{Endpoint: s.URL, Attribution: "actor", TimeoutSeconds: 3, Blocks: blocks, CompareTip: tipA},
			)
			if err != nil {
				t.Fatal(err)
			}
			err = run(ctx, []string{"bitcoin"}, bytes.NewReader(input), out)
			if calls != 3 {
				t.Fatal(calls)
			}
			if blocks == 1 {
				if err != nil || !strings.Contains(out.String(), `"complete":true`) {
					t.Fatal(err, out.String())
				}
			} else if !errors.Is(err, context.Canceled) || !strings.Contains(out.String(), `"complete":false`) {
				t.Fatal(err, out.String())
			}
		})
	}
}
