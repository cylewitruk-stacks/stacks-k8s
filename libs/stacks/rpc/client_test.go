package rpc

import (
	"context"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cylewitruk-stacks/stacks-k8s/libs/stacks/clarity"
	"github.com/cylewitruk-stacks/stacks-k8s/libs/stacks/transaction"
)

const testAddress = "ST000000000000000000002AMW42H"

// clientFor starts a local native node fixture with explicit transport bounds.
func clientFor(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	c, e := New(Config{Endpoint: server.URL, Timeout: time.Second, MaxResponseBytes: 1 << 20})
	if e != nil {
		t.Fatal(e)
	}
	return c
}

func TestNativeObservations(t *testing.T) {
	c := clientFor(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/info":
			if _, err := fmt.Fprintf(
				w,
				`{"network_id":1,"burn_block_height":203,"stacks_tip_height":12,"stacks_tip":"%s"}`,
				strings.Repeat("1", 64),
			); err != nil {
				t.Error(err)
			}
		case "/v2/accounts/" + testAddress:
			if _, err := io.WriteString(
				w,
				`{"nonce":18446744073709551615,"balance":"0xffffffffffffffffffffffffffffffff",`+
					`"locked":"0x1","unlock_height":300}`,
			); err != nil {
				t.Error(err)
			}
		case "/v2/pox":
			if _, err := fmt.Fprintf(
				w,
				`{"contract_id":"%s.pox-4","current_burnchain_block_height":203,`+
					`"reward_cycle_id":10,"reward_cycle_length":20,`+
					`"next_cycle":{"min_threshold_ustx":340282366920938463463374607431768211455,`+
					`"blocks_until_prepare_phase":-1}}`,
				testAddress,
			); err != nil {
				t.Error(err)
			}
		case "/v2/data_var/" + testAddress + "/pox-4/example":
			if _, err := io.WriteString(w, `{"data":"0x03"}`); err != nil {
				t.Error(err)
			}
		case "/v2/contracts/call-read/" + testAddress + "/pox-4/example":
			body, _ := io.ReadAll(r.Body)
			if r.Method != "POST" || !strings.Contains(string(body), `"arguments":["0x03"]`) {
				t.Error("wrong read-only body")
			}
			if _, err := io.WriteString(
				w,
				`{"okay":true,"result":"0x070100000000000000000000000000000001"}`,
			); err != nil {
				t.Error(err)
			}
		case "/v2/contracts/source/" + testAddress + "/example":
			if _, err := io.WriteString(w, `{"source":"(ok u1)"}`); err != nil {
				t.Error(err)
			}
		default:
			http.NotFound(w, r)
		}
	})
	ctx := context.Background()
	info, e := c.Info(ctx)
	if e != nil || info.NetworkID != 1 || info.BurnHeight != 203 {
		t.Fatalf("info: %+v %v", info, e)
	}
	account, e := c.Account(ctx, testAddress)
	if e != nil || account.Balance.Integer.String() != "340282366920938463463374607431768211455" ||
		account.Nonce != ^uint64(0) {
		t.Fatalf("account: %+v %v", account, e)
	}
	pox, e := c.PoX(ctx)
	if e != nil || pox.MinThreshold.Integer.String() != "340282366920938463463374607431768211455" ||
		pox.BlocksUntilPrepare != -1 {
		t.Fatalf("pox: %+v %v", pox, e)
	}
	variable, e := c.DataVariable(ctx, testAddress, "pox-4", "example")
	if e != nil || variable.Type != clarity.True {
		t.Fatalf("variable: %+v %v", variable, e)
	}
	result, e := c.ReadOnly(ctx, testAddress, testAddress, "pox-4", "example", []clarity.Value{{Type: clarity.True}})
	if e != nil || result.Type != clarity.ResponseOK {
		t.Fatalf("read-only: %+v %v", result, e)
	}
	source, found, e := c.Source(ctx, testAddress, "example")
	if e != nil || !found || source != "(ok u1)" {
		t.Fatalf("source: %v %v", found, e)
	}
	_, found, e = c.Source(ctx, testAddress, "missing")
	if e != nil || found {
		t.Fatal("absence conflated with error")
	}
}

func TestMalformedObservations(t *testing.T) {
	for _, body := range []string{
		`{}`,
		`{"network_id":1,"burn_block_height":0,"stacks_tip_height":0}`,
		`{"nonce":0,"balance":"0x-1","locked":"0x0","unlock_height":0}`,
		`{"nonce":0,"balance":"0x100000000000000000000000000000000","locked":"0x0","unlock_height":0}`,
	} {
		t.Run(body, func(t *testing.T) {
			c := clientFor(t, func(w http.ResponseWriter, _ *http.Request) {
				if _, err := io.WriteString(w, body); err != nil {
					t.Error(err)
				}
			})
			if _, e := c.Info(context.Background()); e == nil {
				t.Fatal("accepted incomplete info")
			}
			if _, e := c.Account(context.Background(), testAddress); e == nil {
				t.Fatal("accepted incomplete account")
			}
			if _, e := c.PoX(context.Background()); e == nil {
				t.Fatal("accepted incomplete pox")
			}
		})
	}
	for _, data := range []string{"0x", "0xff", "0x0300", "03"} {
		c := clientFor(t, func(w http.ResponseWriter, _ *http.Request) {
			if _, err := fmt.Fprintf(w, `{"data":%q}`, data); err != nil {
				t.Error(err)
			}
		})
		if _, e := c.DataVariable(context.Background(), testAddress, "sample", "value"); e == nil {
			t.Fatal("accepted invalid Clarity")
		}
	}
}

func TestSubmissionAndExactInclusion(t *testing.T) {
	tx := transaction.Transaction{Bytes: []byte{1, 2, 3}}
	tx.TxID = transaction.ID(tx.Bytes)
	var count atomic.Int32
	c := clientFor(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			count.Add(1)
			raw, _ := io.ReadAll(r.Body)
			if hex.EncodeToString(raw) != "010203" {
				t.Error("submission bytes changed")
			}
			if _, err := fmt.Fprintf(w, `"0x%s"`, tx.TxID); err != nil {
				t.Error(err)
			}
			return
		}
		if _, err := fmt.Fprintf(
			w,
			`{"tx":"010203","result":"(ok true)","index_block_hash":"%s","is_canonical":true}`,
			strings.Repeat("a", 64),
		); err != nil {
			t.Error(err)
		}
	})
	if e := c.Submit(context.Background(), tx); e != nil {
		t.Fatal(e)
	}
	if count.Load() != 1 {
		t.Fatal("submission retried")
	}
	inclusion, e := c.Inclusion(context.Background(), tx.TxID)
	if e != nil || !inclusion.Found || !inclusion.Success {
		t.Fatalf("inclusion %+v %v", inclusion, e)
	}
	for _, body := range []string{`"different"`, `{"error":"secret rejection"}`} {
		count.Store(0)
		c := clientFor(t, func(w http.ResponseWriter, _ *http.Request) {
			count.Add(1)
			w.WriteHeader(500)
			if _, err := io.WriteString(w, body); err != nil {
				t.Error(err)
			}
		})
		e := c.Submit(context.Background(), tx)
		if e == nil || strings.Contains(e.Error(), "secret") || count.Load() != 1 {
			t.Fatalf("ambiguous submission: %v count %d", e, count.Load())
		}
	}
	c = clientFor(t, func(w http.ResponseWriter, _ *http.Request) {
		if _, err := fmt.Fprintf(
			w,
			`{"tx":"010204","result":"(ok true)","index_block_hash":"%s","is_canonical":true}`,
			strings.Repeat("a", 64),
		); err != nil {
			t.Error(err)
		}
	})
	if _, e := c.Inclusion(context.Background(), tx.TxID); e == nil {
		t.Fatal("accepted mismatched consensus bytes")
	}
}

func TestTransportBoundsAndRedirects(t *testing.T) {
	var count atomic.Int32
	c := clientFor(t, func(w http.ResponseWriter, r *http.Request) {
		count.Add(1)
		http.Redirect(w, r, "/v2/info", http.StatusTemporaryRedirect)
	})
	if _, e := c.Info(context.Background()); e == nil || count.Load() != 1 {
		t.Fatal("followed redirect")
	}
	c = clientFor(t, func(w http.ResponseWriter, _ *http.Request) {
		if _, err := io.WriteString(w, strings.Repeat("x", 200)); err != nil {
			t.Error(err)
		}
	})
	c.maxBytes = 100
	if _, e := c.Info(context.Background()); e == nil {
		t.Fatal("accepted oversized body")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := c.Info(ctx); e == nil {
		t.Fatal("ignored cancellation")
	}
	for _, endpoint := range []string{
		"ftp://localhost",
		"http://user:pass@localhost",
		"http://localhost/path",
		"http://localhost?x=1",
		"http://localhost#fragment",
	} {
		if _, e := New(Config{Endpoint: endpoint, Timeout: time.Second, MaxResponseBytes: 100}); e == nil {
			t.Fatal("accepted invalid endpoint")
		}
	}
	count.Store(0)
	c = clientFor(t, func(_ http.ResponseWriter, _ *http.Request) { count.Add(1) })
	if _, e := c.Account(context.Background(), "../info"); e == nil || count.Load() != 0 {
		t.Fatal("sent invalid address")
	}
}
