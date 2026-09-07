//go:build sdk

package environment

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	network "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha1"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/profiles"
	"github.com/pelletier/go-toml/v2"
	core "k8s.io/api/core/v1"
)

func TestProvisioningSharesGenesisAndSeparatesCredentials(t *testing.T) {
	doc, err := Stacks(StacksOptions{Namespace: "test", Name: "custom", Image: "stacks:test", Interval: 10, SDKDirectory: filepath.Join("..", "..", "transactions")})
	if err != nil {
		t.Fatal(err)
	}
	parent := doc.Items[4].(*network.StacksNetwork)
	secrets := map[string]*core.Secret{}
	for _, item := range doc.Items {
		if secret, ok := item.(*core.Secret); ok {
			if secret.Immutable == nil || !*secret.Immutable {
				t.Fatal("mutable secret")
			}
			secrets[secret.Name] = secret
		}
	}
	var account map[string]string
	if err = json.Unmarshal([]byte(secrets["stacks-transaction-account"].StringData["account.json"]), &account); err != nil {
		t.Fatal(err)
	}
	if parent.Spec.StacksTransactionProduction.Sender != account["sender"] {
		t.Fatal("sender binding mismatch")
	}
	if account["configDigest"] != Digest(secrets["stacks-ingress-config"].StringData["config.toml"]) {
		t.Fatal("worker config binding mismatch")
	}
	for _, item := range doc.Items {
		if item == secrets["stacks-transaction-account"] {
			continue
		}
		data, _ := json.Marshal(item)
		if strings.Contains(string(data), account["privateKey"]) {
			t.Fatal("transfer private key leaked into another resource")
		}
	}
	for _, name := range []string{"stacks-miner-config", "stacks-ingress-config"} {
		var parsed struct {
			Balances []struct {
				Address string `toml:"address"`
				Amount  int64  `toml:"amount"`
			} `toml:"ustx_balance"`
		}
		if err = toml.Unmarshal([]byte(secrets[name].StringData["config.toml"]), &parsed); err != nil {
			t.Fatal(err)
		}
		if len(parsed.Balances) != len(parent.Spec.Genesis.Balances) {
			t.Fatal("genesis mismatch")
		}
		for i, b := range parsed.Balances {
			if b.Address != parent.Spec.Genesis.Balances[i].Address || b.Amount != parent.Spec.Genesis.Balances[i].Amount {
				t.Fatal("genesis allocations disagree")
			}
		}
	}
	if !parent.Spec.BitcoinBlockProduction.Paused || !parent.Spec.StacksTransactionProduction.Paused {
		t.Fatal("production starts before bootstrap")
	}
	for _, node := range parent.Spec.StacksNodes {
		if !node.Suspended {
			t.Fatal("node starts before bootstrap")
		}
	}
	if parent.Spec.Genesis.PoX == nil || len(parent.Spec.Genesis.Epochs) != 14 {
		t.Fatal("network snapshot omitted protocol defaults")
	}
}

func TestReusableSeededProfileRequiresMatchingKey(t *testing.T) {
	keys, err := derive(filepath.Join("..", "..", "transactions"), []string{strings.Repeat("1", 64)})
	if err != nil {
		t.Fatal(err)
	}
	genesis := profiles.DefaultGenesis()
	genesis.Balances = []network.GenesisBalance{{Name: "stacker", Address: keys[0].Address, Amount: 1000000000000000}}
	options := StacksOptions{Namespace: "test", Name: "test", Image: "stacks:test", Interval: 10, SDKDirectory: filepath.Join("..", "..", "transactions"), Genesis: &genesis}
	if _, err = Stacks(options); err == nil {
		t.Fatal("profile's seeded account silently replaced")
	}
	options.AccountKeys = map[string]string{"stacker": strings.Repeat("2", 64)}
	if _, err = Stacks(options); err == nil {
		t.Fatal("wrong private key accepted")
	}
	options.AccountKeys["stacker"] = strings.Repeat("1", 64)
	doc, err := Stacks(options)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Bootstrap.Signer.Address != keys[0].Address {
		t.Fatal("profile account was not reused")
	}
	if len(genesis.Balances) != 1 {
		t.Fatal("profile mutated during provisioning")
	}
}
