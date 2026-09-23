package rpc

import (
	"context"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cylewitruk-stacks/stacks-k8s/libs/stacks/clarity"
)

const preparedKey = "0279be667ef9dcbbac55a06295ce870b07029bfcdb2dce28d959f2815b16f81798"

func TestCanonicalViewAndPreparedSetPinEpochTwoTip(t *testing.T) {
	tip := strings.Repeat("11", 32)
	consensus := strings.Repeat("22", 20)
	burn := strings.Repeat("33", 20)
	raw, _ := hex.DecodeString(tip + consensus)
	sum := sha512.Sum512_256(raw)
	index := hex.EncodeToString(sum[:])
	c := clientFor(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/info" && r.URL.Query().Get("tip") != index {
			t.Error("native read did not pin canonical index block ID")
		}
		switch r.URL.Path {
		case "/v2/info":
			if _, err := fmt.Fprintf(
				w,
				`{"network_id":2147483648,"burn_block_height":240,"stacks_tip_height":20,`+
					`"stacks_tip":"%s","stacks_tip_consensus_hash":"%s","pox_consensus":"%s",`+
					`"is_fully_synced":true}`,
				tip,
				consensus,
				burn,
			); err != nil {
				t.Error(err)
			}
		case "/v3/stacker_set/12":
			if _, err := fmt.Fprintf(
				w,
				`{"stacker_set":{"reward_set_version":0,"signers":[{"signing_key":"%s",`+
					`"weight":7,"stacked_amt":340282366920938463463374607431768211455}],`+
					`"pox_ustx_threshold":9007199254740993}}`,
				preparedKey,
			); err != nil {
				t.Error(err)
			}
		case "/v2/pox":
			if _, err := fmt.Fprintf(
				w,
				`{"contract_id":"%s.pox-4","current_burnchain_block_height":240,`+
					`"reward_cycle_id":12,"reward_cycle_length":20,`+
					`"next_cycle":{"min_threshold_ustx":1,"blocks_until_prepare_phase":15}}`,
				testAddress,
			); err != nil {
				t.Error(err)
			}
		case "/v2/accounts/" + testAddress:
			if _, err := io.WriteString(w, `{"nonce":1,"balance":"0x1","locked":"0x0","unlock_height":0}`); err != nil {
				t.Error(err)
			}
		case "/v2/contracts/call-read/" + testAddress + "/pox-4/example":
			if _, err := io.WriteString(w, `{"okay":true,"result":"0x03"}`); err != nil {
				t.Error(err)
			}
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
		}
	})
	ctx := context.Background()
	view, err := c.ChainView(ctx)
	if err != nil || view.IndexBlockID != index || view.BurnHeight != 240 || view.ConsensusHash != consensus {
		t.Fatalf("canonical view: %+v %v", view, err)
	}
	set, err := c.StackerSet(ctx, 12, view.IndexBlockID)
	if err != nil || !set.Available || set.Signers[0].Weight != 7 ||
		set.Signers[0].StackedAmount.Integer.String() != "340282366920938463463374607431768211455" ||
		set.Threshold.Integer.String() != "9007199254740993" {
		t.Fatalf("native epoch-2 set: %+v %v", set, err)
	}
	if _, err := c.PoXAt(ctx, index); err != nil {
		t.Fatal(err)
	}
	if _, err := c.AccountAt(ctx, testAddress, index); err != nil {
		t.Fatal(err)
	}
	if _, err := c.ReadOnlyAt(ctx, index, testAddress, testAddress, "pox-4", "example", []clarity.Value{}); err != nil {
		t.Fatal(err)
	}
}

func TestPreparedSetDistinguishesWaitingFromMalformedOrFailedReads(t *testing.T) {
	for _, test := range []struct {
		name    string
		code    int
		body    string
		wantErr bool
	}{
		{"waiting", 400, `{"err_type":"not_available_try_again"}`, false},
		{"other error", 400, `{"err_type":"other"}`, true},
		{"missing endpoint", 404, `{}`, true},
		{"absent signer field", 200, `{"stacker_set":{"pox_ustx_threshold":1}}`, true},
		{
			"duplicate signer",
			200,
			fmt.Sprintf(
				`{"stacker_set":{"signers":[{"signing_key":"%s","weight":1,"stacked_amt":1},`+
					`{"signing_key":"%s","weight":1,"stacked_amt":1}],"pox_ustx_threshold":1}}`,
				preparedKey,
				preparedKey,
			),
			true,
		},
		{
			"overflow stake",
			200,
			fmt.Sprintf(
				`{"stacker_set":{"signers":[{"signing_key":"%s","weight":1,`+
					`"stacked_amt":340282366920938463463374607431768211456}],"pox_ustx_threshold":1}}`,
				preparedKey,
			),
			true,
		},
		{
			"future version",
			200,
			`{"stacker_set":{"reward_set_version":2,"signers":[],"pox_ustx_threshold":1}}`,
			true,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			c := clientFor(
				t,
				func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(test.code)
					if _, err := io.WriteString(w, test.body); err != nil {
						t.Error(err)
					}
				},
			)
			set, err := c.StackerSet(context.Background(), 12, strings.Repeat("1", 64))
			if (err != nil) != test.wantErr || set.Available {
				t.Fatalf("availability fabricated: %+v %v", set, err)
			}
		})
	}
}

// Native unavailability text is classified only against the pinned full message.
func TestStackerSetUnavailableReasonIsAllowlisted(t *testing.T) {
	for _, test := range []struct {
		message string
		reason  PreparedSetReason
	}{
		{"Could not read reward set. Prepare phase may not have started for this cycle yet. " +
			"Err = PoXAnchorBlockRequired", PreparedSetAnchorRequired},
		{"PRIVATE PoXAnchorBlockRequired", PreparedSetUnavailable},
		{"", PreparedSetUnavailable},
	} {
		t.Run(string(test.reason)+test.message, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).
					Encode(map[string]string{"err_type": "not_available_try_again", "err_msg": test.message})
			}))
			defer server.Close()
			client, err := New(Config{Endpoint: server.URL, Timeout: time.Second, MaxResponseBytes: 4096})
			if err != nil {
				t.Fatal(err)
			}
			got, err := client.StackerSet(context.Background(), 19, strings.Repeat("a", 64))
			if err != nil || got.Available || got.UnavailableReason != test.reason {
				t.Fatalf("%+v %v", got, err)
			}
		})
	}
}
