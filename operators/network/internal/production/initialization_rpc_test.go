package production

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestWalletPreparationObservesBeforeRetryingNamedOperations verifies repeatable setup without mining authority duplication.
func TestWalletPreparationObservesBeforeRetryingNamedOperations(t *testing.T) {
	for _, loseAck := range []bool{false, true} {
		t.Run(fmt.Sprint(loseAck), func(t *testing.T) {
			created, imported, creates, imports := false, false, 0, 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var request struct {
					ID     string `json:"id"`
					Method string `json:"method"`
					Params []any  `json:"params"`
				}
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Error(err)
					return
				}
				var result any
				switch request.Method {
				case "listwallets":
					result = []string{}
					if created {
						result = []string{"miner"}
					}
				case "listwalletdir":
					result = map[string]any{"wallets": []any{}}
				case "createwallet":
					creates++
					created = true
					if len(request.Params) != 7 || request.Params[0] != "miner" || request.Params[1] != true || request.Params[2] != true || request.Params[5] != true {
						t.Errorf("unexpected wallet policy: %v", request.Params)
					}
					if loseAck {
						w.WriteHeader(503)
						return
					}
					result = map[string]string{"name": "miner"}
				case "getwalletinfo":
					result = map[string]any{"walletname": "miner", "private_keys_enabled": false, "descriptors": true}
				case "getdescriptorinfo":
					result = map[string]any{"descriptor": "addr(public)#checksum", "hasprivatekeys": false}
				case "listdescriptors":
					descriptors := []any{}
					if imported {
						descriptors = append(descriptors, map[string]string{"desc": "addr(public)#checksum"})
					}
					result = map[string]any{"descriptors": descriptors}
				case "importdescriptors":
					imports++
					imported = true
					result = []any{map[string]bool{"success": true}}
				default:
					t.Errorf("wallet initialization called %s", request.Method)
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"id": request.ID, "result": result})
			}))
			defer server.Close()
			rpc := NewBitcoinRPC(Credentials{Username: "producer", Password: "test"})
			err := rpc.PrepareWallet(context.Background(), server.URL, "miner", "public")
			if (err != nil) != loseAck {
				t.Fatalf("lost acknowledgement classification: %v", err)
			}
			for range 2 {
				if err := rpc.PrepareWallet(context.Background(), server.URL, "miner", "public"); err != nil {
					t.Fatal(err)
				}
			}
			if creates != 1 || imports != 1 {
				t.Fatalf("setup blindly replayed named operations: create=%d import=%d", creates, imports)
			}
		})
	}
}
