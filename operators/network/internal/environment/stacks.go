package environment

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	stacks "github.com/cylewitruk-stacks/stacks-k8s/apis/network/stacks/v1alpha1"
	network "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha1"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/profiles"
	core "k8s.io/api/core/v1"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation"
)

// StacksOptions supplies the fresh network identity and optional public genesis recipe.
type StacksOptions struct {
	Namespace, Name, Image, SDKDirectory string
	Interval                             int32
	Genesis                              *network.GenesisSpec
	// AccountKeys optionally supplies private seeds for named stacker/transfer profile entries.
	AccountKeys map[string]string
}

// Document contains resources and external bootstrap inputs; its credential-bearing bytes are private.
type Document struct {
	APIVersion string    `json:"apiVersion"`
	Kind       string    `json:"kind"`
	Items      []any     `json:"items"`
	Bootstrap  Bootstrap `json:"bootstrap"`
}

// Bootstrap binds the external bootstrap to its network and selected seeded account.
type Bootstrap struct {
	NetworkName   string           `json:"networkName"`
	Namespace     string           `json:"namespace"`
	GenesisDigest string           `json:"genesisDigest"`
	SignerAccount string           `json:"signerAccount"`
	Bitcoin       BitcoinBootstrap `json:"bitcoin"`
	Signer        SignerAccount    `json:"signer"`
}

// BitcoinBootstrap is the separately held wallet/bootstrap RPC capability.
type BitcoinBootstrap struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Address  string `json:"address"`
}

// SignerAccount contains the SDK encoding for the externally held stacking account.
type SignerAccount struct {
	PrivateKey string `json:"privateKey"`
	Address    string `json:"address"`
	PublicKey  string `json:"publicKey"`
	PoXAddress string `json:"poxAddress"`
}

// keyEncoding contains public SDK-derived encodings for one seed.
type keyEncoding struct {
	Address         string `json:"address"`
	PublicKey       string `json:"publicKey"`
	MiningPublicKey string `json:"miningPublicKey"`
	BitcoinAddress  string `json:"bitcoinAddress"`
	MiningAddress   string `json:"miningAddress"`
}

// Digest binds exact configuration bytes.
func Digest(value string) string { return fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(value))) }

// GenesisDigest binds the resolved public genesis snapshot used by external helpers.
func GenesisDigest(value *network.GenesisSpec) (string, error) {
	resolved, err := profiles.ResolveGenesis(value)
	if err != nil {
		return "", err
	}
	data, err := json.Marshal(resolved)
	if err != nil {
		return "", err
	}
	return Digest(string(data)), nil
}

// Stacks provisions a suspended network, rendering all nodes from one immutable genesis snapshot.
func Stacks(options StacksOptions) (Document, error) {
	if len(validation.IsDNS1123Label(options.Namespace)) != 0 || len(validation.IsDNS1123Label(options.Name)) != 0 || options.Image == "" || options.Interval < 1 || options.Interval > 86400 {
		return Document{}, fmt.Errorf("valid namespace, name, Stacks image and interval 1..86400 are required")
	}
	genesis, err := profiles.ResolveGenesis(options.Genesis)
	if err != nil {
		return Document{}, err
	}
	seeds := []string{token(), token(), token(), token(), token()}
	for name, seed := range options.AccountKeys {
		index, ok := map[string]int{"transfer": 0, "stacker": 2}[name]
		if !ok {
			return Document{}, fmt.Errorf("unsupported account key name %q", name)
		}
		seeds[index] = strings.TrimSuffix(seed, "01")
		if len(seed) == 64 {
			seeds[index] = seed
		}
	}
	keys, err := derive(options.SDKDirectory, seeds)
	if err != nil {
		return Document{}, err
	}
	// Reuse explicit profile allocations only when the supplied key derives their address.
	for _, role := range []struct {
		name   string
		index  int
		amount int64
	}{{"stacker", 2, 1000000000000000}, {"transfer", 0, 1000000000000}} {
		found := false
		for _, a := range genesis.Balances {
			if a.Name == role.name {
				found = true
				if options.AccountKeys[role.name] == "" || a.Address != keys[role.index].Address {
					return Document{}, fmt.Errorf("profile account %s requires its matching private key", role.name)
				}
			}
		}
		if !found {
			genesis.Balances = append(genesis.Balances, network.GenesisBalance{Name: role.name, Address: keys[role.index].Address, Amount: role.amount})
		}
	}

	genesis, err = profiles.ResolveGenesis(&genesis)
	if err != nil {
		return Document{}, err
	}
	actorPassword, authToken, bootstrapPassword := token(), token(), token()
	bitcoin, err := Bitcoin(BitcoinOptions{Namespace: options.Namespace, Name: options.Name, Image: DefaultBitcoinImage, Address: keys[3].MiningAddress, Interval: 5, Weights: []int32{1}, RPC: []RPCIdentity{
		{Username: "stacks", Password: actorPassword, Methods: "getblockchaininfo,getblockcount,getblockhash,getblock,getrawtransaction,getnetworkinfo,listunspent,listwallets,listwalletdir,loadwallet,importdescriptors,sendrawtransaction,estimatesmartfee"},
		{Username: "bootstrap", Password: bootstrapPassword, Methods: "getblockchaininfo,getblockcount,createwallet,getdescriptorinfo,importdescriptors,generatetoaddress"},
	}})
	if err != nil {
		return Document{}, err
	}
	items, parent := bitcoin.Resources, bitcoin.Network
	parent.Spec.BitcoinBlockProduction.Paused = true
	parent.Spec.Defaults.StacksNodeImage = options.Image
	parent.Spec.Defaults.StacksSignerImage = options.Image
	parent.Spec.Genesis = &genesis
	nodeConfig := func(name string, index int, role network.StacksNodeRole, peer int, peerName, signer string) (string, error) {
		return profiles.Stacks(profiles.StacksContext{Network: options.Name, Actor: name, Role: role, Genesis: &genesis, Generated: network.GeneratedConfig{Seed: seeds[index], BootstrapPeers: []string{keys[peer].PublicKey + "@${SERVICE:" + peerName + "}:20444"}}, BitcoinService: "${SERVICE:bitcoin}", BitcoinRPCPort: 18443, BitcoinP2PPort: 18444, SignerService: signer, RPCUser: "stacks", RPCPassword: actorPassword, AuthToken: authToken, MiningPublicKey: keys[index].MiningPublicKey})
	}
	miner, err := nodeConfig("miner", 3, network.StacksNodeMiner, 4, "signer-node", "")
	if err != nil {
		return Document{}, err
	}
	ingress, err := nodeConfig("signer-node", 4, network.StacksNodeSigner, 3, "miner", "${SERVICE:signer}")
	if err != nil {
		return Document{}, err
	}
	signer, err := profiles.Signer(profiles.SignerContext{PrivateKey: seeds[2], NodeHost: "${SERVICE:signer-node}:20443", AuthToken: authToken})
	if err != nil {
		return Document{}, err
	}
	config := func(name, key, text string) network.ConfigSource {
		return network.ConfigSource{SecretRef: &network.ConfigObjectRef{Name: name, Key: key, ExpectedDigest: Digest(text)}}
	}
	parent.Spec.StacksNodes = []network.StacksNodeTemplate{
		{Name: "miner", Role: network.StacksNodeMiner, BitcoinNodeRef: "bitcoin", Config: config("stacks-miner-config", "config.toml", miner), ServiceRefs: []string{"signer-node"}, Suspended: true},
		{Name: "signer-node", Role: network.StacksNodeSigner, BitcoinNodeRef: "bitcoin", Config: config("stacks-ingress-config", "config.toml", ingress), ServiceRefs: []string{"miner"}, Suspended: true},
	}
	parent.Spec.Signers = []network.StacksSignerTemplate{{Name: "signer", NodeRef: "signer-node", Index: 0, Weight: 1, PublicKey: keys[2].PublicKey, Config: config("stacks-signer-config", "signer.toml", signer), Suspended: true}}
	parent.Spec.StacksTransactionProduction = &stacks.TransferPolicy{Target: "signer-node", Sender: keys[0].Address, Recipient: keys[1].Address, AmountMicroSTX: 1, FeeMicroSTX: 1000, IntervalSeconds: options.Interval, Paused: true}
	account, err := json.Marshal(map[string]string{"networkName": options.Name, "sender": keys[0].Address, "privateKey": seeds[0] + "01", "configDigest": Digest(ingress)})
	if err != nil {
		return Document{}, err
	}
	for _, entry := range []struct{ name, key, value string }{{"stacks-miner-config", "config.toml", miner}, {"stacks-ingress-config", "config.toml", ingress}, {"stacks-signer-config", "signer.toml", signer}, {"stacks-transaction-account", "account.json", string(account)}} {
		immutable := true
		items = append(items, &core.Secret{TypeMeta: meta.TypeMeta{APIVersion: "v1", Kind: "Secret"}, ObjectMeta: meta.ObjectMeta{Name: entry.name, Namespace: options.Namespace}, Type: core.SecretTypeOpaque, Immutable: &immutable, StringData: map[string]string{entry.key: entry.value}})
	}
	digest, err := GenesisDigest(&genesis)
	if err != nil {
		return Document{}, err
	}
	return Document{APIVersion: "v1", Kind: "List", Items: items, Bootstrap: Bootstrap{Namespace: options.Namespace, NetworkName: options.Name, GenesisDigest: digest, SignerAccount: "stacker", Bitcoin: BitcoinBootstrap{Username: "bootstrap", Password: bootstrapPassword, Address: keys[3].MiningAddress}, Signer: SignerAccount{PrivateKey: seeds[2] + "01", Address: keys[2].Address, PublicKey: keys[2].PublicKey, PoXAddress: keys[2].BitcoinAddress}}}, nil
}

// derive sends private seeds over stdin to the offline SDK adapter, never argv or logs.
func derive(directory string, seeds []string) ([]keyEncoding, error) {
	data, err := json.Marshal(seeds)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "node", filepath.Join(directory, "keys.mjs"))
	cmd.Stdin = bytes.NewReader(data)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("SDK key encoding failed")
	}
	var keys []keyEncoding
	if err = json.Unmarshal(out, &keys); err != nil || len(keys) != len(seeds) {
		return nil, fmt.Errorf("invalid SDK key encoding response")
	}
	return keys, nil
}
