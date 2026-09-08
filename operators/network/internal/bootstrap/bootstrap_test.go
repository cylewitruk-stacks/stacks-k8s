package bootstrap

import (
	"context"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha1"
	network "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha1"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/environment"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/profiles"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestBootstrapRejectsGenesisAndAccountDrift(t *testing.T) {
	genesis := profiles.DefaultGenesis()
	genesis.Balances = []network.GenesisBalance{{Name: "stacker", Address: "STTEST", Amount: 1000}}
	parent := &network.StacksNetwork{Spec: network.StacksNetworkSpec{Genesis: &genesis, Signers: []network.StacksSignerTemplate{{Name: "signer", PublicKey: "key"}}}}
	digest, err := environment.GenesisDigest(&genesis)
	if err != nil {
		t.Fatal(err)
	}
	setup := environment.Bootstrap{GenesisDigest: digest, Participants: []environment.StackingParticipant{{Signer: "signer", AccountName: "stacker", Stacker: environment.AccountKey{Address: "STTEST"}, Consensus: environment.AccountKey{Address: "STCONSENSUS", PublicKey: "key"}, Administrator: environment.AccountKey{Address: "STADMIN"}}}}
	if err = ValidateGenesis(setup, parent); err != nil {
		t.Fatal(err)
	}
	changed := parent.DeepCopy()
	changed.Spec.Genesis.Balances[0].Amount++
	if ValidateGenesis(setup, changed) == nil {
		t.Fatal("changed genesis accepted")
	}
	setup.Participants[0].Stacker.Address = "OTHER"
	if ValidateGenesis(setup, parent) == nil {
		t.Fatal("unfunded bootstrap identity accepted")
	}
	setup.Participants[0].Stacker.Address = "STTEST"
	changed = parent.DeepCopy()
	changed.Spec.Genesis.Epochs[13].StartHeight = 228
	setup.GenesisDigest, _ = environment.GenesisDigest(changed.Spec.Genesis)
	if ValidateGenesis(setup, changed) == nil {
		t.Fatal("PoX-5 bootstrap silently attempted")
	}
}

func TestSubmissionRequiresExactReceiptWithoutRetry(t *testing.T) {
	raw := []byte(strings.Repeat("x", 150))
	sum := sha512.Sum512_256(raw)
	tx := signedTransaction{TxID: hex.EncodeToString(sum[:]), Bytes: hex.EncodeToString(raw)}
	for _, mode := range []string{"exact", "different", "malformed", "unavailable"} {
		t.Run(mode, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				switch mode {
				case "exact":
					_ = json.NewEncoder(w).Encode(tx.TxID)
				case "different":
					_ = json.NewEncoder(w).Encode(strings.Repeat("0", 64))
				case "malformed":
					fmt.Fprint(w, "{")
				case "unavailable":
					w.WriteHeader(503)
				}
			}))
			defer server.Close()
			u, _ := url.Parse(server.URL)
			port, _ := strconv.Atoi(u.Port())
			s := &session{options: Options{StacksPort: port}, http: server.Client()}
			err := s.submit(context.Background(), tx)
			if (err == nil) != (mode == "exact") || calls != 1 {
				t.Fatalf("mode %s err=%v calls=%d", mode, err, calls)
			}
		})
	}
}

func TestMissingAccountFieldsFailClosed(t *testing.T) {
	for _, body := range []string{`{}`, `{"nonce":0,"balance":"0x10","locked":"0x0"}`, `{"nonce":-1,"unlock_height":0,"balance":"0x10","locked":"0x0"}`, `{"nonce":0,"unlock_height":0,"balance":"invalid","locked":"0x0"}`} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body) }))
		u, _ := url.Parse(server.URL)
		port, _ := strconv.Atoi(u.Port())
		s := &session{options: Options{StacksPort: port}, http: server.Client()}
		if _, err := s.account(context.Background(), "test"); err == nil {
			t.Fatal("malformed account admitted")
		}
		server.Close()
	}
}

func TestBootstrapFreshLedgerExclusion(t *testing.T) {
	parent := &network.StacksNetwork{ObjectMeta: meta.ObjectMeta{Name: "network", UID: "parent"}, Spec: network.StacksNetworkSpec{BitcoinBlockProduction: &bitcoin.ProductionPolicy{Paused: true}}, Status: network.StacksNetworkStatus{BitcoinProductionUID: "policy"}}
	ledger := &bitcoin.BitcoinProductionTarget{Spec: bitcoin.BitcoinProductionTargetSpec{NetworkName: "network", NetworkUID: "parent", ProductionUID: "policy", Policy: bitcoin.TargetPolicy{Target: "bitcoin", Paused: true}}}
	if err := validateFreshBitcoin(parent, ledger, 0); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*bitcoin.BitcoinProductionTarget){
		func(l *bitcoin.BitcoinProductionTarget) { l.Spec.NetworkUID = "foreign" },
		func(l *bitcoin.BitcoinProductionTarget) { l.Spec.Policy.Paused = false },
		func(l *bitcoin.BitcoinProductionTarget) { l.Status.Action = &bitcoin.GenerationReservation{} },
		func(l *bitcoin.BitcoinProductionTarget) {
			l.Status.Reorganization = &bitcoin.ReorganizationReservation{}
		},
		func(l *bitcoin.BitcoinProductionTarget) { l.Status.DispatchState = "Armed" },
		func(l *bitcoin.BitcoinProductionTarget) { l.Status.BlocksProduced = 1 },
	} {
		copy := ledger.DeepCopy()
		change(copy)
		if validateFreshBitcoin(parent, copy, 0) == nil {
			t.Fatal("nonfresh or reserved ledger accepted")
		}
	}
	if validateFreshBitcoin(parent, ledger, 1) == nil {
		t.Fatal("advanced chain accepted")
	}
}

func TestSubmissionRejectionDiagnosticsNeverRetryOrExposeBody(t *testing.T) {
	raw := []byte(strings.Repeat("x", 150))
	sum := sha512.Sum512_256(raw)
	tx := signedTransaction{TxID: hex.EncodeToString(sum[:]), Bytes: hex.EncodeToString(raw)}
	for _, mode := range []string{"fee", "nonce", "other", "wrong-id", "wrong-envelope", "missing-reason", "proxy", "malformed", "oversized"} {
		t.Run(mode, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				code := 400
				body := map[string]any{"txid": "0x" + tx.TxID, "error": "transaction rejected", "reason": "FeeTooLow", "reason_data": "private-detail"}
				switch mode {
				case "nonce":
					body["reason"] = "BadNonce"
				case "other":
					body["reason"] = "private-detail"
				case "wrong-id":
					body["txid"] = strings.Repeat("0", 64)
				case "wrong-envelope":
					body["error"] = "private-detail"
				case "missing-reason":
					delete(body, "reason")
				case "proxy":
					code = 403
				case "oversized":
					body["reason_data"] = strings.Repeat("x", 4<<20)
				}
				w.WriteHeader(code)
				if mode == "malformed" {
					fmt.Fprint(w, "private-detail{")
					return
				}
				_ = json.NewEncoder(w).Encode(body)
			}))
			defer server.Close()
			u, _ := url.Parse(server.URL)
			port, _ := strconv.Atoi(u.Port())
			s := &session{options: Options{StacksPort: port}, http: server.Client()}
			err := s.submit(context.Background(), tx)
			if err == nil || calls != 1 || strings.Contains(err.Error(), "private-detail") {
				t.Fatalf("err=%v calls=%d", err, calls)
			}
			matched := mode == "fee" || mode == "nonce" || mode == "other"
			if strings.Contains(err.Error(), "ingress rejected submission") != matched {
				t.Fatalf("wrong classification: %v", err)
			}
			if !matched && !strings.Contains(err.Error(), "unconfirmed") {
				t.Fatalf("unconfirmed response lost uncertainty: %v", err)
			}
		})
	}
}
