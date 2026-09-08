//go:build sdk

package environment

import (
	"encoding/json"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/testsupport"
	"path/filepath"
	"strings"
	"testing"

	network "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha1"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/profiles"
	"github.com/pelletier/go-toml/v2"
	core "k8s.io/api/core/v1"
)

func TestProvisioningSharesGenesisAndSeparatesCredentials(t *testing.T) {
	doc, err := StacksWithContracts(StacksOptions{Namespace: "test", Name: "custom", Image: "stacks:test", Interval: 10, SDKDirectory: filepath.Join("..", "..", "transactions")}, testsupport.ContractSources())
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
	if parent.Spec.BitcoinBlockProduction.Paused || parent.Spec.StacksTransactionProduction.Paused || parent.Spec.Operation == nil || parent.Spec.BitcoinBlockProduction.Initialization == nil {
		t.Fatal("standard network does not request automatic managed operation")
	}
	for _, node := range parent.Spec.StacksNodes {
		if node.Suspended {
			t.Fatal("standard network still requires manual actor startup")
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
	if _, err = StacksWithContracts(options, testsupport.ContractSources()); err == nil {
		t.Fatal("profile's seeded account silently replaced")
	}
	options.AccountKeys = map[string]string{"stacker": strings.Repeat("2", 64)}
	if _, err = StacksWithContracts(options, testsupport.ContractSources()); err == nil {
		t.Fatal("wrong private key accepted")
	}
	options.AccountKeys["stacker"] = strings.Repeat("1", 64)
	doc, err := StacksWithContracts(options, testsupport.ContractSources())
	if err != nil {
		t.Fatal(err)
	}
	if doc.Bootstrap.Participants[0].Stacker.Address != keys[0].Address {
		t.Fatal("profile account was not reused")
	}
	if len(genesis.Balances) != 1 {
		t.Fatal("profile mutated during provisioning")
	}
}

func TestStandardGenesisAndManagedAuthorityIsolation(t *testing.T) {
	doc, err := StacksWithContracts(StacksOptions{Namespace: "pox5", Name: "test", Image: "stacks:4", Interval: 10, SDKDirectory: filepath.Join("..", "..", "transactions")}, testsupport.ContractSources())
	if err != nil {
		t.Fatal(err)
	}
	parent := doc.Items[4].(*network.StacksNetwork)
	bridge := doc.Bootstrap.Bridge
	participant := doc.Bootstrap.Participants[0]
	if parent.Spec.Genesis.PoX5.SBTCContract != bridge.Deployer.Address+".sbtc-token" || parent.Spec.Genesis.Epochs[13].StartHeight != profiles.PoX5ActivationHeight {
		t.Fatal("PoX-5 bindings not resolved")
	}
	if parent.Spec.Signers[0].PublicKey != participant.Consensus.PublicKey {
		t.Fatal("consensus identity mismatch")
	}
	external := []AccountKey{participant.Stacker, participant.Administrator, bridge.Deployer, bridge.Aggregate}
	external = append(external, bridge.Signers...)
	seen := map[string]bool{participant.Consensus.Address: true}
	for _, account := range external {
		if seen[account.Address] {
			t.Fatal("participant roles share an identity")
		}
		seen[account.Address] = true
		for _, item := range doc.Items {
			allowed := map[string]string{participant.Stacker.Address: "stacks-managed-stacker", participant.Administrator.Address: "stacks-managed-manager-admin", bridge.Deployer.Address: "stacks-managed-sbtc-deployer"}
			if secret, ok := item.(*core.Secret); ok && secret.Name == allowed[account.Address] {
				continue
			}
			data, _ := json.Marshal(item)
			if strings.Contains(string(data), strings.TrimSuffix(account.PrivateKey, "01")) {
				t.Fatal("managed authority leaked outside its designated Secret")
			}
		}
	}
	for _, item := range doc.Items {
		if secret, ok := item.(*core.Secret); ok && (secret.Name == "stacks-miner-config" || secret.Name == "stacks-ingress-config") {
			var parsed struct {
				Node struct {
					Token    string `toml:"pox_5_sbtc_contract"`
					Registry string `toml:"pox_5_sbtc_registry_contract"`
				} `toml:"node"`
			}
			if err = toml.Unmarshal([]byte(secret.StringData["config.toml"]), &parsed); err != nil {
				t.Fatal(err)
			}
			if parsed.Node.Token != parent.Spec.Genesis.PoX5.SBTCContract || parsed.Node.Registry != parent.Spec.Genesis.PoX5.SBTCRegistryContract {
				t.Fatal("node protocol bindings differ")
			}
		}
	}
}

// TestLateEpochKeepsArtifactsWithoutEnrollingUnusedFunding separates funding from managed participation.
func TestLateEpochKeepsArtifactsWithoutEnrollingUnusedFunding(t *testing.T) {
	genesis := profiles.DefaultGenesis()
	genesis.Balances = []network.GenesisBalance{{Name: "unused-experiment", Address: "ST2CY5V39NHDPWSXMW9QDT3HC3GD6Q6XX4CFRK9AG", Amount: 12345}}
	doc, err := StacksWithContracts(StacksOptions{Namespace: "late", Name: "test", Image: "stacks:4", Interval: 10, SDKDirectory: filepath.Join("..", "..", "transactions"), Genesis: &genesis}, testsupport.ContractSources())
	if err != nil {
		t.Fatal(err)
	}
	parent := doc.Items[4].(*network.StacksNetwork)
	if parent.Spec.Genesis.Epochs[13].StartHeight != 1000005 || parent.Spec.Genesis.PoX5 == nil || len(parent.Spec.Operation.ContractSets) != 1 {
		t.Fatal("epoch schedule changed provisioning of standard artifacts")
	}
	found := false
	for _, balance := range parent.Spec.Genesis.Balances {
		if balance.Name == "unused-experiment" {
			found = balance.Amount == 12345
			for _, account := range parent.Spec.Operation.Accounts {
				if account.Address == balance.Address {
					t.Fatal("funding implicitly enrolled account")
				}
			}
		}
	}
	if !found || len(parent.Spec.Operation.Accounts) != 3 || len(parent.Spec.Operation.Participants) != 1 {
		t.Fatal("unused funding changed managed roles")
	}
	if parent.Spec.StacksTransactionProduction.MinimumBurnHeight != genesis.Epochs[8].StartHeight+1 {
		t.Fatal("transfer receipt activation does not follow the epoch schedule")
	}
}
