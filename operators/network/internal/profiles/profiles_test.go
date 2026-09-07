package profiles

import (
	"reflect"
	"strings"
	"testing"

	network "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha1"
	"github.com/pelletier/go-toml/v2"
)

func TestSharedGenesisAndPairedSigner(t *testing.T) {
	genesis := DefaultGenesis()
	genesis.Balances = []network.GenesisBalance{{Name: "funded", Address: "STTEST", Amount: 987654321}}
	var shared any
	for _, role := range []network.StacksNodeRole{network.StacksNodeMiner, network.StacksNodeSigner, network.StacksNodeFollower} {
		text, err := Stacks(StacksContext{Actor: string(role), Role: role, Genesis: &genesis, BitcoinService: "${SERVICE:bitcoin}", BitcoinRPCPort: 18443, BitcoinP2PPort: 18444, SignerService: "${SERVICE:signer}", AuthToken: "quote\"and\\slash", MiningPublicKey: "public"})
		if err != nil {
			t.Fatal(err)
		}
		var parsed map[string]any
		if err = toml.Unmarshal([]byte(text), &parsed); err != nil {
			t.Fatal(err)
		}
		burn := parsed["burnchain"].(map[string]any)
		got := []any{burn["epochs"], burn["pox_prepare_length"], burn["pox_reward_length"], parsed["ustx_balance"]}
		if shared == nil {
			shared = got
		} else if !reflect.DeepEqual(shared, got) {
			t.Fatal("actor roles disagree on shared genesis")
		}
		if parsed["node"].(map[string]any)["miner"] != (role == network.StacksNodeMiner) {
			t.Fatal("wrong miner role")
		}
		if parsed["connection_options"].(map[string]any)["auth_token"] != "quote\"and\\slash" {
			t.Fatal("TOML quoting changed value")
		}
	}
	text, err := Signer(SignerContext{PrivateKey: "key", NodeHost: "${SERVICE:signer-node}:20443", AuthToken: "quote\"and\\slash"})
	if err != nil {
		t.Fatal(err)
	}
	var signer map[string]any
	if err = toml.Unmarshal([]byte(text), &signer); err != nil {
		t.Fatal(err)
	}
	if signer["auth_password"] != "quote\"and\\slash" {
		t.Fatal("signer authentication mismatch")
	}
}

func TestGenesisValidationAndIsolation(t *testing.T) {
	for _, mutate := range []func(*network.GenesisSpec){
		func(g *network.GenesisSpec) { g.Epochs[5].StartHeight = -1 },
		func(g *network.GenesisSpec) { g.Epochs = g.Epochs[:3] },
		func(g *network.GenesisSpec) { g.Epochs[5].Name = "2.0" },
		func(g *network.GenesisSpec) { g.PoX.PrepareLength = g.PoX.RewardCycleLength },
		func(g *network.GenesisSpec) {
			g.Balances = []network.GenesisBalance{{Address: "a", Amount: 1}, {Address: "a", Amount: 2}}
		},
		func(g *network.GenesisSpec) {
			g.Balances = []network.GenesisBalance{{Name: "same", Address: "a", Amount: 1}, {Name: "same", Address: "b", Amount: 2}}
		},
	} {
		g := DefaultGenesis()
		mutate(&g)
		if _, err := ResolveGenesis(&g); err == nil {
			t.Fatal("invalid genesis accepted")
		}
	}
	g := DefaultGenesis()
	g.Balances = []network.GenesisBalance{{Address: "z", Amount: 1}, {Address: "a", Amount: 2}}
	before := g.DeepCopy()
	resolved, err := ResolveGenesis(&g)
	if err != nil {
		t.Fatal(err)
	}
	resolved.Epochs[1].StartHeight = 999
	resolved.PoX.PrepareLength = 999
	if !reflect.DeepEqual(&g, before) {
		t.Fatal("renderer mutated caller genesis")
	}
	text, err := Stacks(StacksContext{Actor: "test", Genesis: &g})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Index(text, `address = "a"`) > strings.Index(text, `address = "z"`) {
		t.Fatal("balances not deterministically ordered")
	}
}

func TestTestGenesisSelectionIsExplicit(t *testing.T) {
	genesis := DefaultGenesis()
	disabled := false
	genesis.UseTestGenesisChainstate = &disabled
	text, err := Stacks(StacksContext{Actor: "follower", Genesis: &genesis})
	if err != nil {
		t.Fatal(err)
	}
	var parsed map[string]any
	if err = toml.Unmarshal([]byte(text), &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed["node"].(map[string]any)["use_test_genesis_chainstate"] != false {
		t.Fatal("explicit empty-chain selection was overridden")
	}
}

func TestMinerRequiresMiningPublicKey(t *testing.T) {
	if text, err := Stacks(StacksContext{Role: network.StacksNodeMiner}); err == nil || text != "" {
		t.Fatalf("missing mining key rendered: text=%q err=%v", text, err)
	}
}

func TestTOML10StringEscapes(t *testing.T) {
	cases := []struct{ value, encoded string }{
		{"\x00\a\x7f", `"\u0000\u0007\u007F"`},
		{"\t\b\f\n\r", `"\u0009\u0008\u000C\u000A\u000D"`},
		{"quote\"and\\slash", `"quote\"and\\slash"`},
		{"Stockholm · 日本 🦀", `"Stockholm · 日本 🦀"`},
	}
	for _, tc := range cases {
		encoded, err := quoteTOML(tc.value)
		if err != nil || encoded != tc.encoded {
			t.Fatalf("encoding %q: got %q err=%v, want %q", tc.value, encoded, err, tc.encoded)
		}
		// Exact encodings above assert TOML 1.0 syntax independently of this parser.
		rendered, err := Signer(SignerContext{AuthToken: tc.value})
		if err != nil {
			t.Fatal(err)
		}
		var parsed map[string]any
		if err = toml.Unmarshal([]byte(rendered), &parsed); err != nil || parsed["auth_password"] != tc.value {
			t.Fatalf("rendered value did not round-trip: %v", err)
		}
	}
	if _, err := quoteTOML(string([]byte{0xff})); err == nil {
		t.Fatal("invalid UTF-8 accepted")
	}
	if _, err := Signer(SignerContext{AuthToken: string([]byte{0xff})}); err == nil {
		t.Fatal("invalid UTF-8 rendered")
	}
}
