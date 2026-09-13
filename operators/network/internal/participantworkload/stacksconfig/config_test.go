package stacksconfig

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/libs/stacks/identity"
	"github.com/pelletier/go-toml/v2"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
)

// nodeFixture supplies distinct runtime and frozen public genesis values.
func nodeFixture() (NodeParameters, NodeSecrets) {
	chain := api.Chain{
		PoX:       api.PoX{RewardCycleLength: 20, PrepareLength: 5},
		Contracts: api.ContractBindings{Deployer: "ST000000000000000000002AMW42H"},
		Allocations: []api.Allocation{{
			Address:        "ST000000000000000000002AMW42H",
			AmountMicroSTX: "9007199254740993",
		}},
	}
	for i, name := range []string{
		"1.0",
		"2.0",
		"2.05",
		"2.1",
		"2.2",
		"2.3",
		"2.4",
		"2.5",
		"3.0",
		"3.1",
		"3.2",
		"3.3",
		"3.4",
		"4.0",
	} {
		chain.Epochs = append(chain.Epochs, api.Epoch{Name: name, StartHeight: int64(i * 10)})
	}
	parameters := NodeParameters{
		Name:                "node-a",
		Chain:               chain,
		P2PAddress:          "10.96.0.15",
		RPCHost:             "10.96.0.16",
		BitcoinHost:         "bitcoin-rpc.test.svc",
		BootstrapPeers:      []string{"02public@seed.test.svc:20444"},
		SignerHost:          "signer.test.svc",
		Mining:              true,
		WalletName:          "miner-wallet",
		FeeRateSatsPerVByte: 3,
	}
	return parameters, NodeSecrets{
		PrivateKey:  strings.Repeat("0", 63) + "1",
		RPCUsername: "actor",
		RPCPassword: "secret-password",
		EventToken:  "node-event-token",
	}
}

func TestNativeNodeAndSignerIdentityAndFrozenGenesis(t *testing.T) {
	p, secrets := nodeFixture()
	data, err := Node(p, secrets)
	if err != nil {
		t.Fatal(err)
	}
	var d nodeDocument
	if err := toml.Unmarshal(data, &d); err != nil {
		t.Fatal(err)
	}
	public, _ := identity.FromPrivate(secrets.PrivateKey)
	if d.Node.Seed != secrets.PrivateKey || d.Node.PeerSeed != secrets.PrivateKey ||
		d.Burnchain.MiningPublicKey != public.MiningPublicKey ||
		len(d.Burnchain.MiningPublicKey) != 130 {
		t.Fatal("native keychain or descriptor public key disagrees")
	}
	if d.Burnchain.Username != "actor" || d.Burnchain.WalletName != "miner-wallet" || d.Burnchain.FeeRate != 3 ||
		d.Node.P2PAddress != "10.96.0.15:20444" ||
		d.Node.DataURL != "http://10.96.0.16:20443" {
		t.Fatal("managed native ingress disagrees")
	}
	if d.Node.InitiativeDelay != 500 {
		t.Fatal("regtest miner retained mainnet idle initiative threshold")
	}
	if d.Balances[0].Amount != 9007199254740993 || len(d.Burnchain.Epochs) != len(p.Chain.Epochs) {
		t.Fatal("genesis precision or schedule lost")
	}
	for i, epoch := range p.Chain.Epochs {
		if d.Burnchain.Epochs[i].Name != epoch.Name || d.Burnchain.Epochs[i].Height != epoch.StartHeight {
			t.Fatal("epoch mismatch")
		}
	}
	if !d.Node.Stacker ||
		!reflect.DeepEqual(d.Observers[0].Keys, []string{"stackerdb", "block_proposal", "burn_blocks"}) {
		t.Fatal("consensus subscription missing")
	}
	signer, err := Signer(p.RPCHost, secrets.PrivateKey, secrets.EventToken)
	if err != nil {
		t.Fatal(err)
	}
	var s signerDocument
	if err := toml.Unmarshal(signer, &s); err != nil {
		t.Fatal(err)
	}
	if s.PrivateKey != secrets.PrivateKey+"01" || s.AuthPassword != d.Connection.AuthToken ||
		s.NodeHost != p.RPCHost+":20443" {
		t.Fatal("node/signer pairing disagrees")
	}
	p.Name = "late-node"
	p.Mining, p.SignerHost, p.BootstrapPeers = false, "", nil
	other, err := Node(p, secrets)
	if err != nil {
		t.Fatal(err)
	}
	var late nodeDocument
	if err := toml.Unmarshal(other, &late); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(late.Balances, d.Balances) || !reflect.DeepEqual(late.Burnchain.Epochs, d.Burnchain.Epochs) ||
		late.Burnchain.PrepareLength != d.Burnchain.PrepareLength ||
		late.Node.SBTCContract != d.Node.SBTCContract {
		t.Fatal("late join changed frozen genesis")
	}
	if late.Miner != nil || late.Burnchain.MiningPublicKey != "" || len(late.Observers) != 0 {
		t.Fatal("disabled role retained active configuration")
	}
	for _, kind := range []api.ParticipantKind{"StacksNode", "StacksSigner"} {
		generated := data
		if kind == "StacksSigner" {
			generated = signer
		}
		actual, verified, err := Apply(kind, generated, nil, nil, nil)
		if err != nil || !verified || !bytes.Equal(actual, generated) {
			t.Fatalf("valid native model rejected: %v", err)
		}
	}
}

func TestOverridesRejectProtectedWritesAndLossyValues(t *testing.T) {
	p, secrets := nodeFixture()
	generated, _ := Node(p, secrets)
	for _, raw := range []string{
		`{"node":{"seed":"unchanged"}}`,
		`{"node":"replacement"}`,
		`{"burnchain":{"epochs":[]}}`,
		`{"events_observer":[]}`,
		`{"node":{"pox_5_bond_admin":"other"}}`,
		`{"tuning":{"value":null}}`,
		`{"tuning":{"value":9223372036854775808}}`,
		`null`,
		`{} {}`,
	} {
		t.Run(raw, func(t *testing.T) {
			if _, _, err := Apply(
				"StacksNode",
				generated,
				nil,
				&common.Config{Overrides: &runtime.RawExtension{Raw: []byte(raw)}},
				nil,
			); err == nil {
				t.Fatal("accepted invalid override")
			}
		})
	}
	raw := []byte(
		`{"node":{"wait_time_for_blocks":7},"tuning":{"large":9007199254740993,` +
			`"ratio":1.5,"enabled":true,"peers":["one","two"]}}`,
	)
	data, verified, err := Apply(
		"StacksNode",
		generated,
		nil,
		&common.Config{Overrides: &runtime.RawExtension{Raw: raw}},
		nil,
	)
	if err != nil || !verified {
		t.Fatalf("valid overrides: %v", err)
	}
	d, err := document(data)
	if err != nil {
		t.Fatal(err)
	}
	if value, _ := at(d, "tuning.large"); value != int64(9007199254740993) {
		t.Fatal("integer precision lost")
	}
	if value, _ := at(d, "node.wait_time_for_blocks"); value != int64(7) {
		t.Fatal("unprotected setting not merged")
	}
	if value, _ := at(d, "node.seed"); value != secrets.PrivateKey {
		t.Fatal("merge removed protected sibling")
	}
}

func TestCustomConfigurationAgreementAndUnverified(t *testing.T) {
	p, secrets := nodeFixture()
	generated, _ := Node(p, secrets)
	config := &common.Config{
		SecretRef:   &common.SecretKeyRef{Name: "custom", Key: "node.toml"},
		ServiceRefs: []common.ServiceRef{{Alias: "bitcoin", Kind: "BitcoinNode", Name: "core", Endpoint: "p2p"}},
	}
	custom := bytes.ReplaceAll(generated, []byte(p.BitcoinHost), []byte("${SERVICE:bitcoin}"))
	data, verified, err := Apply("StacksNode", generated, custom, config, map[string]string{"bitcoin": p.BitcoinHost})
	if err != nil || !verified || !bytes.Equal(data, generated) {
		t.Fatalf("valid complete custom config: %v", err)
	}
	custom = bytes.ReplaceAll(generated, []byte("nakamoto-neon"), []byte("divergent"))
	if _, _, err := Apply(
		"StacksNode",
		generated,
		custom,
		config,
		map[string]string{"bitcoin": p.BitcoinHost},
	); err == nil {
		t.Fatal("accepted managed genesis divergence")
	}
	config.Compatibility = ptr.To("Unverified")
	if _, verified, err := Apply(
		"StacksNode",
		generated,
		custom,
		config,
		map[string]string{"bitcoin": p.BitcoinHost},
	); err != nil ||
		verified {
		t.Fatalf("explicit divergence not preserved: %v", err)
	}
	if _, _, err := Apply(
		"StacksNode",
		generated,
		[]byte("[broken\nsecret-secret"),
		config,
		map[string]string{"bitcoin": p.BitcoinHost},
	); err == nil ||
		strings.Contains(err.Error(), "secret-secret") {
		t.Fatal("parser failure leaked private input")
	}
	if _, _, err := Apply(
		"StacksNode",
		generated,
		[]byte("date=2026-01-01"),
		config,
		map[string]string{"bitcoin": p.BitcoinHost},
	); err == nil {
		t.Fatal("accepted unsupported timestamp scalar")
	}
}
