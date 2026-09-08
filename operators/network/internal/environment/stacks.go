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

	bitcoinapi "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha1"
	stacks "github.com/cylewitruk-stacks/stacks-k8s/apis/network/stacks/v1alpha1"
	network "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha1"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/naming"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/profiles"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/protocolcontracts"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/stackssdk"
	core "k8s.io/api/core/v1"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation"
)

// StacksOptions supplies the fresh network identity and optional public genesis recipe.
type StacksOptions struct {
	Namespace, Name, Image, SDKDirectory string
	Interval                             int32
	// SBTCContracts is the pinned checkout's contracts/contracts directory.
	SBTCContracts string
	Genesis       *network.GenesisSpec
	// AccountKeys optionally supplies private seeds for named stacker/transfer profile entries.
	AccountKeys map[string]string
}

// Document contains resources and private provisioning identities; its credential-bearing bytes are private.
type Document struct {
	APIVersion string    `json:"apiVersion"`
	Kind       string    `json:"kind"`
	Items      []any     `json:"items"`
	Bootstrap  Bootstrap `json:"bootstrap"`
}

// Bootstrap retains private provisioning identities independently of public managed declarations.
type Bootstrap struct {
	NetworkName   string `json:"networkName"`
	Namespace     string `json:"namespace"`
	GenesisDigest string `json:"genesisDigest"`
	// Participants binds each holder and administrator to its consensus actor.
	Participants []StackingParticipant `json:"participants"`
	// Bridge retains initialization identities; bridge signing keys are never mounted into actor Pods.
	Bridge  *BridgeBootstrap `json:"bridge,omitempty"`
	Bitcoin BitcoinBootstrap `json:"bitcoin"`
}

// StackingParticipant separates funded holder, manager administration and consensus signing.
type StackingParticipant struct {
	// Signer names the declared consensus actor whose key is authorized.
	Signer string `json:"signer"`
	// AccountName selects the immutable genesis allocation.
	AccountName string `json:"accountName"`
	// Stacker alone owns the STX and its transaction nonce.
	Stacker AccountKey `json:"stacker"`
	// Consensus supplies only the block-signing and PoX authorization key.
	Consensus AccountKey `json:"consensus"`
	// Administrator owns the minimal manager and its separate transaction nonce.
	Administrator AccountKey `json:"administrator"`
	// Manager is the non-custodial direct-staking contract principal.
	Manager string `json:"manager"`
}

// BridgeBootstrap explicitly initializes a disposable bridge registry without running bridge daemons.
type BridgeBootstrap struct {
	// Deployer publishes the pinned sBTC contracts and authorizes the initial registry rotation.
	Deployer AccountKey `json:"deployer"`
	// Signers are independent bridge identities retained for future supported rotation.
	Signers []AccountKey `json:"signers"`
	// Aggregate is an explicitly initialized test key, not a DKG result.
	Aggregate AccountKey `json:"aggregate"`
	// Threshold is the registry's multisig threshold.
	Threshold int `json:"threshold"`
}

// BitcoinBootstrap is the separately held wallet/bootstrap RPC capability.
type BitcoinBootstrap struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Address  string `json:"address"`
}

// AccountKey contains one role-specific private seed and its public SDK encodings.
type AccountKey = stackssdk.Key

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

// Stacks provisions an automatically managed network with verified standard sBTC artifacts.
func Stacks(options StacksOptions) (Document, error) {
	contracts, err := protocolcontracts.Load(options.SBTCContracts)
	if err != nil {
		return Document{}, err
	}
	return StacksWithContracts(options, contracts)
}

// StacksWithContracts renders explicit contract artifacts for programmatic callers.
// The CLI uses Stacks to enforce the reviewed upstream pin; custom sources require separate qualification.
func StacksWithContracts(options StacksOptions, contracts []protocolcontracts.Source) (Document, error) {
	if err := validateContractSources(contracts); err != nil {
		return Document{}, err
	}

	if len(validation.IsDNS1123Label(options.Namespace)) != 0 || len(validation.IsDNS1123Label(options.Name)) != 0 || options.Image == "" || options.Interval < 1 || options.Interval > 86400 {
		return Document{}, fmt.Errorf("valid namespace, name, Stacks image and interval 1..86400 are required")
	}
	genesis, err := profiles.ResolveGenesis(options.Genesis)
	if err != nil {
		return Document{}, err
	}
	seeds := make([]string, 11)
	for i := range seeds {
		seeds[i] = token()
	}
	for name, seed := range options.AccountKeys {
		index, ok := map[string]int{"transfer": 0, "stacker": 2, "manager-admin": 6, "sbtc-deployer": 7}[name]
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
	roles := []struct {
		name   string
		index  int
		amount int64
	}{{"stacker", 2, 1000000000000000}, {"transfer", 0, 1000000000000}, {"manager-admin", 6, 1000000000000}}
	{
		roles = append(roles, struct {
			name   string
			index  int
			amount int64
		}{"sbtc-deployer", 7, 1000000000000})
	}
	for _, role := range roles {
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

	{
		bindings := network.GenesisPoX5{SBTCContract: keys[7].Address + ".sbtc-token", SBTCRegistryContract: keys[7].Address + ".sbtc-registry", BondAdmin: keys[7].Address, PauseAdmin: keys[7].Address}
		if genesis.PoX5 != nil && *genesis.PoX5 != bindings {
			return Document{}, fmt.Errorf("profile PoX-5 bindings require their matching deployer key")
		}
		genesis.PoX5 = &bindings
		if options.Genesis == nil || len(options.Genesis.Epochs) == 0 {
			genesis.Epochs[13].StartHeight = profiles.PoX5ActivationHeight
		}
	}
	genesis, err = profiles.ResolveGenesis(&genesis)
	if err != nil {
		return Document{}, err
	}
	actorPassword, authToken := token(), token()
	bitcoin, err := Bitcoin(BitcoinOptions{Namespace: options.Namespace, Name: options.Name, Image: DefaultBitcoinImage, Address: keys[3].MiningAddress, Interval: 5, InitializeWallet: true, Weights: []int32{1}, RPC: []RPCIdentity{
		{Username: "stacks", Password: actorPassword, Methods: "getblockchaininfo,getblockcount,getblockhash,getblock,getrawtransaction,getnetworkinfo,listunspent,listwallets,listwalletdir,loadwallet,importdescriptors,sendrawtransaction,estimatesmartfee"},
	}})
	if err != nil {
		return Document{}, err
	}
	items, parent := bitcoin.Resources, bitcoin.Network
	parent.Spec.BitcoinBlockProduction.Initialization = &bitcoinapi.RegtestInitialization{Target: "bitcoin", Wallet: "stacks-miner", InitialHeight: 201}
	parent.Spec.Defaults.StacksNodeImage = options.Image
	parent.Spec.Defaults.StacksSignerImage = options.Image
	parent.Spec.Genesis = &genesis
	nodeConfig := func(name string, index int, role network.StacksNodeRole, peer int, peerName, signer string) (string, error) {
		receiptService := ""
		if name == "signer-node" {
			receiptService = naming.Child(options.Name, "receipts") + ":8082"
		}
		return profiles.Stacks(profiles.StacksContext{Network: options.Name, Actor: name, Role: role, Genesis: &genesis, Generated: network.GeneratedConfig{Seed: seeds[index], BootstrapPeers: []string{keys[peer].PublicKey + "@${SERVICE:" + peerName + "}:20444"}}, BitcoinService: "${SERVICE:bitcoin}", BitcoinRPCPort: 18443, BitcoinP2PPort: 18444, SignerService: signer, ReceiptService: receiptService, RPCUser: "stacks", RPCPassword: actorPassword, AuthToken: authToken, MiningPublicKey: keys[index].MiningPublicKey})
	}
	miner, err := nodeConfig("miner", 3, network.StacksNodeMiner, 4, "signer-node", "")
	if err != nil {
		return Document{}, err
	}
	ingress, err := nodeConfig("signer-node", 4, network.StacksNodeSigner, 3, "miner", "${SERVICE:signer}")
	if err != nil {
		return Document{}, err
	}
	signer, err := profiles.Signer(profiles.SignerContext{PrivateKey: seeds[5], NodeHost: "${SERVICE:signer-node}:20443", AuthToken: authToken})
	if err != nil {
		return Document{}, err
	}
	config := func(name, key, text string) network.ConfigSource {
		return network.ConfigSource{SecretRef: &network.ConfigObjectRef{Name: name, Key: key, ExpectedDigest: Digest(text)}}
	}
	parent.Spec.StacksNodes = []network.StacksNodeTemplate{
		{Name: "miner", Role: network.StacksNodeMiner, BitcoinNodeRef: "bitcoin", Config: config("stacks-miner-config", "config.toml", miner), ServiceRefs: []string{"signer-node"}, Suspended: false},
		{Name: "signer-node", Role: network.StacksNodeSigner, BitcoinNodeRef: "bitcoin", Config: config("stacks-ingress-config", "config.toml", ingress), ServiceRefs: []string{"miner"}, Suspended: false},
	}
	parent.Spec.Signers = []network.StacksSignerTemplate{{Name: "signer", NodeRef: "signer-node", Index: 0, Weight: 1, PublicKey: keys[5].PublicKey, Config: config("stacks-signer-config", "signer.toml", signer), Suspended: false}}
	parent.Spec.StacksTransactionProduction = &stacks.TransferPolicy{CredentialsSecret: "stacks-transaction-account", Target: "signer-node", Sender: keys[0].Address, Recipient: keys[1].Address, AmountMicroSTX: 1, FeeMicroSTX: 1000, IntervalSeconds: options.Interval, MinimumBurnHeight: genesis.Epochs[8].StartHeight + 1, Paused: false}
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
	accountKey := func(i int) AccountKey {
		return AccountKey{PrivateKey: seeds[i] + "01", Address: keys[i].Address, PublicKey: keys[i].PublicKey, PoXAddress: keys[i].BitcoinAddress}
	}
	setup := Bootstrap{Namespace: options.Namespace, NetworkName: options.Name, GenesisDigest: digest, Bitcoin: BitcoinBootstrap{Address: keys[3].MiningAddress}, Participants: []StackingParticipant{{Signer: "signer", AccountName: "stacker", Stacker: accountKey(2), Consensus: accountKey(5), Administrator: accountKey(6), Manager: keys[6].Address + ".direct-signer"}}}
	{
		setup.Bridge = &BridgeBootstrap{Deployer: accountKey(7), Signers: []AccountKey{accountKey(8), accountKey(9)}, Aggregate: accountKey(10), Threshold: 2}
	}
	items, err = managedResources(items, parent, setup, contracts, Digest(ingress))
	if err != nil {
		return Document{}, err
	}
	return Document{APIVersion: "v1", Kind: "List", Items: items, Bootstrap: setup}, nil
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

// ValidateBootstrapKeys checks every external signing key against its recorded public identity.
func ValidateBootstrapKeys(setup Bootstrap, directory string) error {
	accounts := []AccountKey{}
	for _, p := range setup.Participants {
		accounts = append(accounts, p.Stacker, p.Consensus, p.Administrator)
	}
	if setup.Bridge != nil {
		accounts = append(accounts, setup.Bridge.Deployer, setup.Bridge.Aggregate)
		accounts = append(accounts, setup.Bridge.Signers...)
	}
	seeds := make([]string, len(accounts))
	seen := map[string]bool{}
	for i, a := range accounts {
		if len(a.PrivateKey) != 66 || !strings.HasSuffix(a.PrivateKey, "01") || seen[a.Address] {
			return fmt.Errorf("bootstrap roles require distinct valid signing identities")
		}
		seeds[i] = a.PrivateKey[:64]
		seen[a.Address] = true
	}
	keys, err := derive(directory, seeds)
	if err != nil {
		return err
	}
	for i, a := range accounts {
		if keys[i].Address != a.Address || keys[i].PublicKey != a.PublicKey || keys[i].BitcoinAddress != a.PoXAddress {
			return fmt.Errorf("bootstrap signing key does not match its recorded identity")
		}
	}
	return nil
}
