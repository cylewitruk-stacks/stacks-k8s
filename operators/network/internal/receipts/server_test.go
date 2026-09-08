package receipts

import (
	"bytes"
	"context"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"fmt"
	stacks "github.com/cylewitruk-stacks/stacks-k8s/apis/network/stacks/v1alpha1"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/accountledger"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"net/http/httptest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"strings"
	"testing"
)

// admittedIngress supplies an already qualified Pod for the receipt handler tests.
type admittedIngress struct{}

func (admittedIngress) Admit(context.Context, *stacks.StacksAccount, string, bool) (accountledger.Target, error) {
	return accountledger.Target{Endpoint: "http://10.0.0.3:20443"}, nil
}

// canonicalBlock controls native ancestry evidence independently of callback claims.
type canonicalBlock struct{ found bool }

func (c *canonicalBlock) LegacyCanonical(context.Context, string, string) (bool, error) {
	return c.found, nil
}

// TestReceiptDeliveryRequiresOriginBytesCanonicalityAndDurableAccounting asserts receipt effects and retry boundaries.
func TestReceiptDeliveryRequiresOriginBytesCanonicalityAndDurableAccounting(t *testing.T) {
	for _, mode := range []string{"success", "execution-error", "wrong-origin", "wrong-bytes", "wrong-result", "noncanonical", "write-loss", "retired"} {
		t.Run(mode, func(t *testing.T) {
			scheme := runtime.NewScheme()
			if err := stacks.AddToScheme(scheme); err != nil {
				t.Fatal(err)
			}
			raw := make([]byte, 100)
			hash := sha512.Sum512_256(raw)
			id := hex.EncodeToString(hash[:])
			block := strings.Repeat("b", 64)
			account := &stacks.StacksAccount{ObjectMeta: metav1.ObjectMeta{Name: "account", Namespace: "test", UID: "account"}, Status: stacks.StacksAccountStatus{Phase: "Pending", Transaction: &stacks.AccountTransaction{TxID: id, ConsumerUID: "consumer", Ordinal: 1, Nonce: 4}}}
			if mode == "retired" {
				account.Status.Phase = "Abandoned"
			}
			fail := mode == "write-loss"
			c := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(account).WithObjects(account).WithInterceptorFuncs(interceptor.Funcs{SubResourcePatch: func(ctx context.Context, c client.Client, sub string, obj client.Object, patch client.Patch, opts ...client.SubResourcePatchOption) error {
				if fail {
					return fmt.Errorf("accounting unavailable")
				}
				return c.SubResource(sub).Patch(ctx, obj, patch, opts...)
			}}).Build()
			canonical := &canonicalBlock{found: mode != "noncanonical"}
			server := &Server{Namespace: "test", Ledger: &accountledger.Ledger{Client: c, Reader: c, Admission: admittedIngress{}}, RPC: canonical}
			tx := map[string]string{"txid": "0x" + id, "raw_tx": "0x" + hex.EncodeToString(raw), "raw_result": "0x0703", "status": "success"}
			if mode == "wrong-bytes" {
				tx["raw_tx"] = "0x00"
			}
			if mode == "wrong-result" {
				tx["raw_result"] = "0x0803"
			}
			if mode == "execution-error" {
				tx["raw_result"] = "0x0803"
				tx["status"] = "abort_by_response"
			}
			data, _ := json.Marshal(map[string]any{"index_block_hash": "0x" + block, "transactions": []any{tx}})
			deliver := func() int {
				request := httptest.NewRequest("POST", "/new_block", bytes.NewReader(data))
				request.RemoteAddr = "10.0.0.3:1234"
				if mode == "wrong-origin" {
					request.RemoteAddr = "10.0.0.4:1234"
				}
				response := httptest.NewRecorder()
				server.ServeHTTP(response, request)
				return response.Code
			}
			code := deliver()
			if err := c.Get(context.Background(), client.ObjectKeyFromObject(account), account); err != nil {
				t.Fatal(err)
			}
			good := mode == "success" || mode == "execution-error"
			if (account.Status.Transaction.Receipt != nil) != good {
				t.Fatalf("mode %s accounted invalid evidence (%d)", mode, code)
			}
			if good {
				if code != 200 || account.Status.Transaction.Receipt.BlockID != block || account.Status.Transaction.Receipt.Success != (mode == "success") || account.Status.Transaction.Acknowledged || account.Status.NextNonce == nil || *account.Status.NextNonce != 5 {
					t.Fatalf("incorrect durable receipt: %#v", account.Status)
				}
				before := account.Status.Transaction.Receipt.DeepCopy()
				if deliver() != 200 {
					t.Fatal("duplicate delivery failed")
				}
				if err := c.Get(context.Background(), client.ObjectKeyFromObject(account), account); err != nil {
					t.Fatal(err)
				}
				if *before != *account.Status.Transaction.Receipt {
					t.Fatal("duplicate changed receipt identity")
				}
			}
			if mode == "write-loss" {
				if code != 503 {
					t.Fatal("acknowledged failed accounting")
				}
				fail = false
				if deliver() != 200 {
					t.Fatal("receipt delivery could not retry after outage")
				}
				if err := c.Get(context.Background(), client.ObjectKeyFromObject(account), account); err != nil {
					t.Fatal(err)
				}
				if account.Status.Transaction.Receipt == nil {
					t.Fatal("retried evidence not retained")
				}
			}
		})
	}
}
