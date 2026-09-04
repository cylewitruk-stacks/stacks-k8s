// Package profiles renders bounded public regtest configuration profiles.
package profiles

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	networkv1alpha1 "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha1"
)

// StacksContext supplies topology values needed by a generated Stacks profile.
type StacksContext struct {
	Network        string
	Actor          string
	Role           networkv1alpha1.StacksNodeRole
	BitcoinService string
	BitcoinRPCPort int32
	BitcoinP2PPort int32
	SignerService  string
	SignerIndex    int32
	Genesis        *networkv1alpha1.GenesisSpec
	Generated      networkv1alpha1.GeneratedConfig
}

// Bitcoin renders a deterministic Bitcoin Core regtest configuration.
func Bitcoin(rpcPort int32) string {
	return fmt.Sprintf("regtest=1\nprinttoconsole=1\nserver=1\ntxindex=1\ndiscover=0\ndnsseed=0\nlistenonion=0\nfallbackfee=0.00001\n\n[regtest]\nrpcbind=0.0.0.0:%d\nrpcallowip=0.0.0.0/0\nrpcuser=devnet\nrpcpassword=devnet\n", rpcPort)
}

// Stacks renders a deterministic non-secret Nakamoto regtest node configuration.
func Stacks(context StacksContext) string {
	seed := context.Generated.Seed
	if seed == "" {
		digest := sha256.Sum256([]byte(context.Network + ":" + context.Actor))
		seed = hex.EncodeToString(digest[:])
	}
	dispatcher := context.Generated.EventDispatcher
	if dispatcher == "" {
		dispatcher = "queued"
	}
	bootstrap := append([]string(nil), context.Generated.BootstrapPeers...)
	sort.Strings(bootstrap)
	bootstrapLine := ""
	if len(bootstrap) > 0 {
		bootstrapLine = fmt.Sprintf("bootstrap_node = %q\n", strings.Join(bootstrap, ","))
	}
	observer := ""
	if context.SignerService != "" {
		observer = fmt.Sprintf("\n[[events_observer]]\nendpoint = %q\nevents_keys = [\"stackerdb\", \"block_proposal\", \"burn_blocks\"]\n", context.SignerService+":30000")
	}
	return fmt.Sprintf(`[node]
name = %q
rpc_bind = "0.0.0.0:20443"
p2p_bind = "0.0.0.0:20444"
data_url = "http://__NODE_IP__:20443"
p2p_address = "__NODE_IP__:20444"
prometheus_bind = "0.0.0.0:20446"
working_dir = "/data/node"
seed = %q
local_peer_seed = %q
miner = %t
stacker = %t
event_dispatcher_blocking = %t
event_dispatcher_queue_size = 1000
use_test_genesis_chainstate = true
pox_sync_sample_secs = 0
wait_time_for_blocks = 0
wait_time_for_microblocks = 0
mine_microblocks = false
%s
[connection_options]
public_ip_address = "__NODE_IP__:20444"
private_neighbors = true
walk_interval = 5
inv_sync_interval = 5
download_interval = 1
auth_token = "12345"
%s
[burnchain]
chain = "bitcoin"
mode = "nakamoto-neon"
poll_time_secs = 1
magic_bytes = "T3"
pox_prepare_length = 5
pox_reward_length = 20
burn_fee_cap = 20000
peer_host = %q
peer_port = %d
rpc_port = %d
rpc_ssl = false
username = "devnet"
password = "devnet"
timeout = 30
%s%s`, "regtest-"+context.Actor, seed, seed, context.Role == networkv1alpha1.StacksNodeMiner,
		context.Role == networkv1alpha1.StacksNodeSigner, dispatcher == "blocking", bootstrapLine, observer,
		context.BitcoinService, context.BitcoinP2PPort, context.BitcoinRPCPort, epochs, genesis(context.Genesis))
}

func genesis(value *networkv1alpha1.GenesisSpec) string {
	if value == nil {
		return ""
	}
	balances := append([]networkv1alpha1.GenesisBalance(nil), value.Balances...)
	sort.Slice(balances, func(i, j int) bool { return balances[i].Address < balances[j].Address })
	var result strings.Builder
	for _, balance := range balances {
		fmt.Fprintf(&result, "\n[[ustx_balance]]\naddress = %q\namount = %d\n", balance.Address, balance.Amount)
	}
	return result.String()
}

const epochs = `
[[burnchain.epochs]]
epoch_name = "1.0"
start_height = 0
[[burnchain.epochs]]
epoch_name = "2.0"
start_height = 0
[[burnchain.epochs]]
epoch_name = "2.05"
start_height = 203
[[burnchain.epochs]]
epoch_name = "2.1"
start_height = 204
[[burnchain.epochs]]
epoch_name = "2.2"
start_height = 206
[[burnchain.epochs]]
epoch_name = "2.3"
start_height = 207
[[burnchain.epochs]]
epoch_name = "2.4"
start_height = 208
[[burnchain.epochs]]
epoch_name = "2.5"
start_height = 209
[[burnchain.epochs]]
epoch_name = "3.0"
start_height = 223
[[burnchain.epochs]]
epoch_name = "3.1"
start_height = 224
[[burnchain.epochs]]
epoch_name = "3.2"
start_height = 225
[[burnchain.epochs]]
epoch_name = "3.3"
start_height = 226
[[burnchain.epochs]]
epoch_name = "3.4"
start_height = 227
[[burnchain.epochs]]
epoch_name = "4.0"
start_height = 1000005
`
