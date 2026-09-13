// Package stacksconfig renders bounded native Stacks actor configuration without RPC or Kubernetes clients.
package stacksconfig

import (
	"fmt"
	"net"
	"strconv"
	"strings"

	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/libs/stacks/identity"
	"github.com/pelletier/go-toml/v2"
)

// NodeParameters contains resolved public topology and frozen chain inputs.
type NodeParameters struct {
	// Name identifies this participant.
	Name string `json:"name"`
	// Chain is the exact frozen public genesis.
	Chain api.Chain `json:"chain"`
	// P2PAddress is the stable allocated Service IP required by Core's SocketAddr parser.
	P2PAddress string `json:"p2pAddress"`
	// RPCHost is the allocated RPC Service address advertised to native peers.
	RPCHost string `json:"rpcHost"`
	// BitcoinHost supplies the exact admitted burnchain Service DNS name.
	BitcoinHost string `json:"bitcoinHost"`
	// BootstrapPeers contains public-key@host:port startup hints.
	BootstrapPeers []string `json:"bootstrapPeers,omitempty"`
	// SignerHost selects the optional consensus event endpoint.
	SignerHost string `json:"signerHost,omitempty"`
	// Mining enables the declared node miner role.
	Mining bool `json:"mining"`
	// WalletName selects the public descriptor wallet loaded on the Bitcoin node.
	WalletName string `json:"walletName,omitempty"`
	// FeeRateSatsPerVByte supplies the declared Bitcoin commit fee rate.
	FeeRateSatsPerVByte int32 `json:"feeRateSatsPerVByte,omitempty"`
}

// NodeSecrets remains confined to the scoped renderer process.
type NodeSecrets struct {
	// PrivateKey is the node/account scalar.
	PrivateKey string
	// RPCUsername selects only the Bitcoin actor principal.
	RPCUsername string
	// RPCPassword authenticates the actor principal.
	RPCPassword string
	// EventToken authenticates the paired signer to the node.
	EventToken string
}

// nodeDocument is the typed native Core configuration model.
type nodeDocument struct {
	// Node contains peer identity and protocol configuration.
	Node nodeSection `toml:"node"`
	// Miner supplies declared mining timing.
	Miner *minerSection `toml:"miner,omitempty"`
	// Connection supplies supported regtest connectivity and signer authentication.
	Connection connectionSection `toml:"connection_options"`
	// Burnchain contains the Bitcoin ingress and frozen epoch schedule.
	Burnchain burnchainSection `toml:"burnchain"`
	// Balances contains exactly the frozen initial allocations.
	Balances []balanceSection `toml:"ustx_balance"`
	// Observers contains the managed consensus destination only.
	Observers []observerSection `toml:"events_observer,omitempty"`
}

// nodeSection maps protected identity and shared genesis fields to Core TOML.
type nodeSection struct {
	// Name identifies the actor.
	Name string `toml:"name"`
	// RPCBind is the fixed native RPC listener.
	RPCBind string `toml:"rpc_bind"`
	// P2PBind is the fixed native peer listener.
	P2PBind string `toml:"p2p_bind"`
	// DataURL advertises this actor's RPC service.
	DataURL string `toml:"data_url"`
	// P2PAddress is a routable literal service address.
	P2PAddress string `toml:"p2p_address"`
	// WorkingDir retains chain data in the participant volume.
	WorkingDir string `toml:"working_dir"`
	// Seed supplies the miner keychain's scalar.
	Seed string `toml:"seed"`
	// PeerSeed supplies the stable P2P identity scalar.
	PeerSeed string `toml:"local_peer_seed"`
	// Miner enables the node miner role.
	Miner bool `toml:"miner"`
	// Stacker enables consensus-node participation independently of administration.
	Stacker bool `toml:"stacker"`
	// TestGenesis selects regtest chainstate construction.
	TestGenesis bool `toml:"use_test_genesis_chainstate"`
	// SBTCContract fixes the real token principal required by PoX-5.
	SBTCContract string `toml:"pox_5_sbtc_contract"`
	// RegistryContract fixes the real registry principal.
	RegistryContract string `toml:"pox_5_sbtc_registry_contract"`
	// BondAdmin fixes the supported profile's public bond authority.
	BondAdmin string `toml:"pox_5_bond_admin"`
	// PauseAdmin fixes the supported profile's public pause authority.
	PauseAdmin string `toml:"pox_5_pause_admin"`
	// TxIndex enables native transaction observations.
	TxIndex bool `toml:"txindex"`
	// Bootstrap contains frozen startup hints, not ongoing dependencies.
	Bootstrap string `toml:"bootstrap_node,omitempty"`
	// MineMicroblocks controls legacy microblock production.
	MineMicroblocks bool `toml:"mine_microblocks"`
	// EventBlocking keeps consensus events synchronous under this profile.
	EventBlocking bool `toml:"event_dispatcher_blocking"`
	// EventQueue bounds pending event delivery.
	EventQueue int64 `toml:"event_dispatcher_queue_size"`
	// WaitBlocks removes mainnet-oriented startup delays in regtest.
	WaitBlocks int64 `toml:"wait_time_for_blocks"`
	// WaitMicroblocks removes mainnet-oriented microblock delays.
	WaitMicroblocks int64 `toml:"wait_time_for_microblocks"`
	// InitiativeDelay sets the idle Nakamoto miner initiative threshold in milliseconds.
	InitiativeDelay int64 `toml:"next_initiative_delay"`
	// PoXSample removes sampling delays for disposable regtest networks.
	PoXSample int64 `toml:"pox_sync_sample_secs"`
}

// minerSection contains native regtest miner timing.
type minerSection struct {
	// FirstAttempt sets the first attempt delay in milliseconds.
	FirstAttempt int64 `toml:"first_attempt_time_ms"`
	// SubsequentAttempt sets later attempt delay in milliseconds.
	SubsequentAttempt int64 `toml:"subsequent_attempt_time_ms"`
	// CommitDelay controls block-commit delay in milliseconds.
	CommitDelay int64 `toml:"block_commit_delay_ms"`
	// VRFPath retains the activated VRF key with the node's data.
	VRFPath string `toml:"activated_vrf_key_path"`
}

// connectionSection contains native peer and signer authentication settings.
type connectionSection struct {
	// PublicAddress advertises the participant's stable P2P endpoint.
	PublicAddress string `toml:"public_ip_address"`
	// PrivateNeighbors allows regtest cluster addresses.
	PrivateNeighbors bool `toml:"private_neighbors"`
	// WalkInterval controls native peer discovery.
	WalkInterval int64 `toml:"walk_interval"`
	// InventoryInterval controls inventory synchronization.
	InventoryInterval int64 `toml:"inv_sync_interval"`
	// DownloadInterval controls block download polling.
	DownloadInterval int64 `toml:"download_interval"`
	// AuthToken is paired with the signer configuration.
	AuthToken string `toml:"auth_token"`
}

// burnchainSection contains fixed native regtest ingress and chain identity.
type burnchainSection struct {
	// Chain selects Bitcoin.
	Chain string `toml:"chain"`
	// Mode selects the supported regtest protocol mode.
	Mode string `toml:"mode"`
	// PollTime sets native Bitcoin polling seconds.
	PollTime int64 `toml:"poll_time_secs"`
	// MagicBytes identifies the regtest burnchain protocol.
	MagicBytes string `toml:"magic_bytes"`
	// PrepareLength comes from frozen genesis.
	PrepareLength int32 `toml:"pox_prepare_length"`
	// RewardLength comes from frozen genesis.
	RewardLength int32 `toml:"pox_reward_length"`
	// FeeCap bounds burnchain expenditure per native policy.
	FeeCap int64 `toml:"burn_fee_cap"`
	// FeeRate supplies the typed commit fee rate.
	FeeRate int32 `toml:"satoshis_per_byte"`
	// PeerHost selects the Bitcoin participant service.
	PeerHost string `toml:"peer_host"`
	// PeerPort selects native Bitcoin P2P.
	PeerPort int32 `toml:"peer_port"`
	// RPCPort selects native Bitcoin RPC.
	RPCPort int32 `toml:"rpc_port"`
	// RPCSSL describes the cluster transport.
	RPCSSL bool `toml:"rpc_ssl"`
	// Username selects restricted protocol credentials.
	Username string `toml:"username"`
	// Password authenticates that principal.
	Password string `toml:"password"`
	// Timeout bounds native RPC waiting.
	Timeout int64 `toml:"timeout"`
	// WalletName selects the loaded descriptor wallet for enabled mining.
	WalletName string `toml:"wallet_name,omitempty"`
	// MiningPublicKey matches the descriptor's uncompressed public key.
	MiningPublicKey string `toml:"local_mining_public_key,omitempty"`
	// Epochs copies every frozen activation boundary.
	Epochs []epochSection `toml:"epochs"`
}

// epochSection preserves a quoted protocol epoch and its activation height.
type epochSection struct {
	// Name is the exact protocol epoch string.
	Name string `toml:"epoch_name"`
	// Height is the frozen burn height.
	Height int64 `toml:"start_height"`
}

// balanceSection contains one deduplicated frozen allocation.
type balanceSection struct {
	// Address is the public Stacks principal.
	Address string `toml:"address"`
	// Amount is its initial microSTX balance.
	Amount uint64 `toml:"amount"`
}

// observerSection defines the managed native consensus subscription.
type observerSection struct {
	// Endpoint selects the signer event Service.
	Endpoint string `toml:"endpoint"`
	// Keys limits delivery to required consensus inputs.
	Keys []string `toml:"events_keys"`
}

// Node renders a complete native configuration using the exact frozen chain.
func Node(p NodeParameters, secrets NodeSecrets) ([]byte, error) {
	public, err := identity.FromPrivate(secrets.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf("invalid node key")
	}
	key := strings.TrimSuffix(secrets.PrivateKey, "01")
	if len(secrets.PrivateKey) == 64 {
		key = secrets.PrivateKey
	}
	if net.ParseIP(p.P2PAddress) == nil || p.RPCHost == "" || p.BitcoinHost == "" || secrets.RPCUsername != common.BitcoinActorRPCUsername || secrets.RPCPassword == "" || secrets.EventToken == "" {
		return nil, fmt.Errorf("incomplete managed node endpoints or credentials")
	}
	if len(p.Chain.Epochs) != 14 || p.Chain.PoX.PrepareLength < 1 || p.Chain.PoX.PrepareLength >= p.Chain.PoX.RewardCycleLength || p.Chain.Contracts.Deployer == "" {
		return nil, fmt.Errorf("incomplete frozen genesis")
	}
	d := nodeDocument{Node: nodeSection{Name: p.Name, RPCBind: "0.0.0.0:20443", P2PBind: "0.0.0.0:20444", DataURL: "http://" + net.JoinHostPort(p.RPCHost, "20443"), P2PAddress: net.JoinHostPort(p.P2PAddress, "20444"), WorkingDir: "/data/node", Seed: key, PeerSeed: key, Miner: p.Mining, Stacker: p.SignerHost != "", TestGenesis: true, SBTCContract: p.Chain.Contracts.Deployer + ".sbtc-token", RegistryContract: p.Chain.Contracts.Deployer + ".sbtc-registry", BondAdmin: p.Chain.Contracts.Deployer, PauseAdmin: p.Chain.Contracts.Deployer, TxIndex: true, Bootstrap: strings.Join(p.BootstrapPeers, ","), EventBlocking: true, EventQueue: 1000, InitiativeDelay: 500}, Connection: connectionSection{PublicAddress: net.JoinHostPort(p.P2PAddress, "20444"), PrivateNeighbors: true, WalkInterval: 5, InventoryInterval: 5, DownloadInterval: 1, AuthToken: secrets.EventToken}, Burnchain: burnchainSection{Chain: "bitcoin", Mode: "nakamoto-neon", PollTime: 1, MagicBytes: "T3", PrepareLength: p.Chain.PoX.PrepareLength, RewardLength: p.Chain.PoX.RewardCycleLength, FeeCap: 20000, FeeRate: p.FeeRateSatsPerVByte, PeerHost: p.BitcoinHost, PeerPort: 18444, RPCPort: 18443, Username: secrets.RPCUsername, Password: secrets.RPCPassword, Timeout: 30}}
	if d.Burnchain.FeeRate == 0 {
		d.Burnchain.FeeRate = 2
	}
	for _, epoch := range p.Chain.Epochs {
		d.Burnchain.Epochs = append(d.Burnchain.Epochs, epochSection{Name: epoch.Name, Height: epoch.StartHeight})
	}
	for _, allocation := range p.Chain.Allocations {
		amount, err := strconv.ParseUint(string(allocation.AmountMicroSTX), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid frozen allocation")
		}
		d.Balances = append(d.Balances, balanceSection{Address: allocation.Address, Amount: amount})
	}
	if p.Mining {
		if p.WalletName == "" {
			return nil, fmt.Errorf("miner wallet name is required")
		}
		d.Miner = &minerSection{FirstAttempt: 1000, SubsequentAttempt: 2000, CommitDelay: 1000, VRFPath: "/data/node/activated-vrf-key.json"}
		d.Burnchain.WalletName = p.WalletName
		d.Burnchain.MiningPublicKey = public.MiningPublicKey
	}
	if p.SignerHost != "" {
		d.Observers = []observerSection{{Endpoint: net.JoinHostPort(p.SignerHost, "30000"), Keys: []string{"stackerdb", "block_proposal", "burn_blocks"}}}
	}
	data, err := toml.Marshal(d)
	if err != nil {
		return nil, fmt.Errorf("render native node configuration")
	}
	return data, nil
}

// signerDocument is the typed native consensus signer configuration.
type signerDocument struct {
	// PrivateKey signs consensus messages only.
	PrivateKey string `toml:"stacks_private_key"`
	// NodeHost selects the admitted Stacks node RPC endpoint.
	NodeHost string `toml:"node_host"`
	// Endpoint binds the consensus event listener.
	Endpoint string `toml:"endpoint"`
	// Network selects testnet signing domains.
	Network string `toml:"network"`
	// AuthPassword authenticates against the paired node.
	AuthPassword string `toml:"auth_password"`
	// DBPath retains consensus state on participant storage.
	DBPath string `toml:"db_path"`
	// MetricsEndpoint binds optional native metrics.
	MetricsEndpoint string `toml:"metrics_endpoint"`
	// EventTimeout controls native event polling milliseconds.
	EventTimeout int64 `toml:"event_timeout_ms"`
}

// Signer renders a consensus actor independently of PoX registration.
func Signer(nodeHost, privateKey, eventToken string) ([]byte, error) {
	if _, err := identity.FromPrivate(privateKey); err != nil {
		return nil, fmt.Errorf("invalid consensus key")
	}
	if nodeHost == "" || eventToken == "" {
		return nil, fmt.Errorf("incomplete signer binding")
	}
	if len(privateKey) == 64 {
		privateKey += "01"
	}
	data, err := toml.Marshal(signerDocument{PrivateKey: privateKey, NodeHost: net.JoinHostPort(nodeHost, "20443"), Endpoint: "0.0.0.0:30000", Network: "testnet", AuthPassword: eventToken, DBPath: "/data/signer.sqlite", MetricsEndpoint: "0.0.0.0:31000", EventTimeout: 250})
	if err != nil {
		return nil, fmt.Errorf("render native signer configuration")
	}
	return data, nil
}
