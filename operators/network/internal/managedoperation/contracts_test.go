package managedoperation

import (
	"context"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	stacks "github.com/cylewitruk-stacks/stacks-k8s/apis/network/stacks/v1alpha1"
	network "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha1"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/accountledger"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/profiles"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/protocolcontracts"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/stacksrpc"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/stackssdk"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/stackstx"
	core "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

// testEncoder counts offline signing while native HTTP supplies actual receipt decoding.
type testEncoder struct {
	signs int
	tx    stackstx.Transaction
}

func (e *testEncoder) Sign(context.Context, string, any) (stackstx.Transaction, error) {
	e.signs++
	return e.tx, nil
}
func (*testEncoder) VerifyKey(context.Context, stackssdk.Key) error { return nil }
func (*testEncoder) EncodeArguments(context.Context, []stackssdk.Argument) ([]string, error) {
	return []string{"0x09"}, nil
}

// testAdmission isolates these tests from the independently tested Kubernetes ingress chain.
type testAdmission struct{ endpoint string }

func (a testAdmission) Admit(context.Context, *stacks.StacksAccount, string, bool) (accountledger.Target, error) {
	return accountledger.Target{Endpoint: a.endpoint, ActorUID: "actor", PodUID: "pod", ContainerID: "container"}, nil
}

// contractFixture exposes a native RPC endpoint and persistent fake Kubernetes state.
type contractFixture struct {
	r           *ContractReconciler
	c           client.Client
	key         client.ObjectKey
	encoder     *testEncoder
	publication string
	receipt     bool
	sends       int
	source      string
}

func newContractFixture(t *testing.T, hooks interceptor.Funcs) *contractFixture {
	t.Helper()
	f := &contractFixture{key: client.ObjectKey{Namespace: "test", Name: "network-contracts-sbtc"}, source: "(define-read-only (hello) (ok true))"}
	raw := make([]byte, 100)
	hash := sha512.Sum512_256(raw)
	f.encoder = &testEncoder{tx: stackstx.Transaction{TxID: hex.EncodeToString(hash[:]), Bytes: hex.EncodeToString(raw)}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case request.URL.Path == "/v2/info":
			fmt.Fprint(w, `{"network_id":2147483648,"burn_block_height":230,"stacks_tip_height":20}`)
		case strings.HasPrefix(request.URL.Path, "/v2/accounts/"):
			fmt.Fprint(w, `{"nonce":0,"balance":"0xffffffffffff","locked":"0x0","unlock_height":0}`)
		case strings.HasPrefix(request.URL.Path, "/v2/contracts/source/"):
			if f.publication == "" {
				http.NotFound(w, request)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"source": f.publication})
		case request.URL.Path == "/v2/transactions":
			f.sends++
			_ = json.NewEncoder(w).Encode(f.encoder.tx.TxID)
		case strings.HasPrefix(request.URL.Path, "/v3/transaction/"):
			if !f.receipt {
				http.NotFound(w, request)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"tx": f.encoder.tx.Bytes, "result": "(ok true)", "index_block_hash": strings.Repeat("b", 64), "is_canonical": true})
		default:
			http.NotFound(w, request)
		}
	}))
	t.Cleanup(server.Close)
	scheme := runtime.NewScheme()
	for _, add := range []func(*runtime.Scheme) error{stacks.AddToScheme, network.AddToScheme, core.AddToScheme} {
		if err := add(scheme); err != nil {
			t.Fatal(err)
		}
	}
	genesis := profiles.DefaultGenesis()
	parent := &network.StacksNetwork{ObjectMeta: metav1.ObjectMeta{Name: "network", Namespace: "test", UID: "network-uid"}, Spec: network.StacksNetworkSpec{Genesis: &genesis, Operation: &network.NetworkOperation{}}}
	owner := []metav1.OwnerReference{*metav1.NewControllerRef(parent, network.GroupVersion.WithKind("StacksNetwork"))}
	secretBytes, _ := json.Marshal(stackssdk.Key{Address: "STTEST", PrivateKey: "fixture-private-key"})
	account := &stacks.StacksAccount{ObjectMeta: metav1.ObjectMeta{Name: "network-account-deployer", Namespace: "test", UID: "account", OwnerReferences: owner, Finalizers: []string{accountledger.Finalizer}},
		Spec: stacks.StacksAccountSpec{NetworkName: parent.Name, NetworkUID: string(parent.UID), Policy: stacks.ManagedAccountPolicy{Name: "deployer", Address: "STTEST", KeySecretRef: stacks.ArtifactReference{Name: "key", Key: "key.json", Digest: digest(secretBytes)}}}}
	set := &stacks.StacksContractSet{ObjectMeta: metav1.ObjectMeta{Name: f.key.Name, Namespace: f.key.Namespace, UID: "consumer", Generation: 1, OwnerReferences: owner},
		Spec: stacks.StacksContractSetSpec{NetworkName: parent.Name, NetworkUID: string(parent.UID), Policy: stacks.ContractSetPolicy{Name: "sbtc", Account: "deployer", Contracts: []stacks.ContractArtifact{{Name: "hello", ClarityVersion: 3, SourceRef: stacks.ArtifactReference{Name: "sources", Key: "hello.clar", Digest: digest([]byte(f.source))}}}}}}
	parent.Status.Capabilities = []network.CapabilityIdentity{{Kind: "StacksContractSet", Name: set.Name, UID: string(set.UID)}}
	f.c = fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&stacks.StacksAccount{}, &stacks.StacksContractSet{}).WithObjects(parent, account, set,
		&core.Secret{ObjectMeta: metav1.ObjectMeta{Name: "key", Namespace: "test"}, Data: map[string][]byte{"key.json": secretBytes}},
		&core.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "sources", Namespace: "test"}, Data: map[string]string{"hello.clar": f.source}}).WithInterceptorFuncs(hooks).Build()
	rpc := stacksrpc.NewClient()
	ledger := &accountledger.Ledger{Client: f.c, Reader: f.c, Admission: testAdmission{endpoint: server.URL}, RPC: rpc}
	f.r = &ContractReconciler{Runtime: &Runtime{Client: f.c, Reader: f.c, RPC: rpc, Ledger: ledger, SDK: f.encoder}}
	return f
}

func (f *contractFixture) reconcile(t *testing.T) {
	t.Helper()
	if _, err := f.r.Reconcile(context.Background(), ctrl.Request{NamespacedName: f.key}); err != nil {
		t.Fatal(err)
	}
}

func TestContractPublicationResumesFromExactReceiptWithoutReplay(t *testing.T) {
	f := newContractFixture(t, interceptor.Funcs{})
	f.reconcile(t)
	if f.sends != 1 || f.encoder.signs != 1 {
		t.Fatalf("publication counts: %d/%d", f.sends, f.encoder.signs)
	}
	// A fresh controller instance retains no sequence position or pending transaction in memory.
	f.r = &ContractReconciler{Runtime: f.r.Runtime}
	f.reconcile(t)
	if f.sends != 1 {
		t.Fatal("restart resubmitted publication")
	}
	f.receipt, f.publication = true, f.source
	f.reconcile(t)
	set := &stacks.StacksContractSet{}
	if err := f.c.Get(context.Background(), f.key, set); err != nil {
		t.Fatal(err)
	}
	if set.Status.Phase != "Ready" || set.Status.BurnHeight != 230 || len(set.Status.Receipts) != 1 || set.Status.Receipts[0].Transaction.TxID != f.encoder.tx.TxID {
		t.Fatalf("missing deployment evidence: %+v", set.Status)
	}
	f.reconcile(t)
	if f.sends != 1 || f.encoder.signs != 1 {
		t.Fatal("observed contract caused another mutation")
	}
}

func TestExistingContractRequiresExactSourceWithoutReadingSigningKey(t *testing.T) {
	for _, matching := range []bool{true, false} {
		t.Run(fmt.Sprint(matching), func(t *testing.T) {
			f := newContractFixture(t, interceptor.Funcs{})
			f.publication = f.source
			if !matching {
				f.publication = "(define-read-only (different) u1)"
			}
			if err := f.c.Delete(context.Background(), &core.Secret{ObjectMeta: metav1.ObjectMeta{Name: "key", Namespace: "test"}}); err != nil {
				t.Fatal(err)
			}
			f.reconcile(t)
			set := &stacks.StacksContractSet{}
			if err := f.c.Get(context.Background(), f.key, set); err != nil {
				t.Fatal(err)
			}
			if (set.Status.Phase == "Ready") != matching || f.sends != 0 || f.encoder.signs != 0 {
				t.Fatalf("source adoption: %+v sends=%d signs=%d", set.Status, f.sends, f.encoder.signs)
			}
		})
	}
}

func TestConsumerReceiptWriteFailureRetainsAccountUntilAcknowledgement(t *testing.T) {
	failed := false
	f := newContractFixture(t, interceptor.Funcs{SubResourcePatch: func(ctx context.Context, c client.Client, sub string, object client.Object, patch client.Patch, options ...client.SubResourcePatchOption) error {
		if set, ok := object.(*stacks.StacksContractSet); ok && len(set.Status.Receipts) > 0 && !failed {
			failed = true
			return fmt.Errorf("receipt write unavailable")
		}
		return c.SubResource(sub).Patch(ctx, object, patch, options...)
	}})
	f.reconcile(t)
	f.receipt, f.publication = true, f.source
	f.reconcile(t)
	account := &stacks.StacksAccount{}
	key := client.ObjectKey{Namespace: "test", Name: "network-account-deployer"}
	if err := f.c.Get(context.Background(), key, account); err != nil {
		t.Fatal(err)
	}
	if account.Status.Transaction.Acknowledged {
		t.Fatal("failed consumer write released account")
	}
	f.reconcile(t)
	if err := f.c.Get(context.Background(), key, account); err != nil {
		t.Fatal(err)
	}
	if !account.Status.Transaction.Acknowledged || f.sends != 1 {
		t.Fatal("receipt did not recover without replay")
	}
}

func TestContractDependencyCyclesAndMissingArtifactsNeverMutate(t *testing.T) {
	_, err := protocolcontracts.Order([]stacks.ContractArtifact{{Name: "a", DependsOn: []string{"b"}}, {Name: "b", DependsOn: []string{"a"}}})
	if err == nil {
		t.Fatal("dependency cycle accepted")
	}
	f := newContractFixture(t, interceptor.Funcs{})
	source := &core.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "sources", Namespace: "test"}}
	if err := f.c.Delete(context.Background(), source); err != nil {
		t.Fatal(err)
	}
	f.reconcile(t)
	if f.sends != 0 || f.encoder.signs != 0 {
		t.Fatal("missing artifact authorized a transaction")
	}
}
