package production

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestReorganizationRPCUsesExactHeaderAndNullReceipts(t *testing.T) {
	hash, parent, work := fmt.Sprintf("%064x", 2), fmt.Sprintf("%064x", 1), fmt.Sprintf("%064x", 4)
	var markers atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			ID, Method string
			Params     []any
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			return
		}
		var result any
		switch request.Method {
		case "getblockchaininfo":
			result = map[string]any{"chain": "regtest", "bestblockhash": hash, "blocks": 1, "chainwork": work}
		case "getblockheader":
			result = map[string]any{"hash": hash, "height": 1, "previousblockhash": parent, "chainwork": work}
		case "getblockhash":
			result = hash
		case "getchaintips":
			result = []any{map[string]any{"hash": hash, "status": "active"}}
		case "invalidateblock", "reconsiderblock":
			if len(request.Params) != 1 || request.Params[0] != hash {
				t.Error("marker target differs")
			}
			markers.Add(1)
		default:
			t.Errorf("unexpected method %s", request.Method)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"id": request.ID, "result": result, "error": nil})
	}))
	defer server.Close()
	rpc := NewBitcoinRPC(Credentials{})
	p, err := rpc.Tip(context.Background(), server.URL)
	if err != nil || p.Hash != hash || p.PreviousBlockHash != parent || p.Chainwork != work {
		t.Fatalf("Core header decoding: %#v %v", p, err)
	}
	if h, err := rpc.HashAt(context.Background(), server.URL, 1); err != nil || h != hash {
		t.Fatal("canonical hash failed")
	}
	if err := rpc.CheckTips(context.Background(), server.URL); err != nil {
		t.Fatal(err)
	}
	if err := rpc.Invalidate(context.Background(), server.URL, hash, "invalidate-id"); err != nil {
		t.Fatal(err)
	}
	if err := rpc.Reconsider(context.Background(), server.URL, hash, "cleanup-id"); err != nil {
		t.Fatal(err)
	}
	if markers.Load() != 2 {
		t.Fatal("unexpected marker calls")
	}
}

func TestReorganizationMutationRejectsMalformedOrLostReceiptWithoutReplay(t *testing.T) {
	for _, mode := range []string{"lost", "wrong-id", "non-null", "error"} {
		t.Run(mode, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				switch mode {
				case "lost":
					connection, _, err := w.(http.Hijacker).Hijack()
					if err == nil {
						connection.Close()
					}
				case "wrong-id":
					fmt.Fprint(w, `{"id":"different","result":null}`)
				case "non-null":
					fmt.Fprint(w, `{"id":"dispatch","result":{}}`)
				case "error":
					fmt.Fprint(w, `{"id":"dispatch","error":{"code":-1,"message":"private server detail"},"result":null}`)
				}
			}))
			defer server.Close()
			rpc := NewBitcoinRPC(Credentials{})
			if err := rpc.Reconsider(context.Background(), server.URL, fmt.Sprintf("%064x", 1), "dispatch"); err == nil {
				t.Fatal("unmatched cleanup accepted")
			}
			if calls.Load() != 1 {
				t.Fatal("unknown cleanup replayed")
			}
		})
	}
}
