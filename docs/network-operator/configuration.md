# Network configuration and genesis

`StacksNetwork.spec.genesis` is the immutable public initialization snapshot for
one network: named accounts and initial micro-STX balances, epoch activation
heights, test-genesis selection, and PoX cycle lengths. Changing or removing it requires a fresh network
and fresh chain data. It cannot be added after network creation. Balances are
initial allocations, not ongoing funding instructions.

Admission rejects duplicate addresses or nonempty account names. Omitted or
empty epochs select the built-in schedule; an explicit schedule must contain
all 14 supported epochs in order, with nondecreasing heights and epochs 1.0/2.0
starting at zero. These shared rules apply to profiles and embedded genesis.
The CRDs require Kubernetes 1.32+ for cost-bounded account-name uniqueness.

`useTestGenesisChainstate: true` includes the selected Stacks binary’s built-in
test allocations in addition to the declared balances. The image therefore also
contributes to genesis identity; retain its image identity with chain evidence.

## Reusable profiles

`StacksGenesisProfile` stores the same public values as a reusable, immutable
namespaced recipe. Provisioning copies the recipe into the new network and
fills omitted regtest defaults. Existing networks never look up the profile;
deleting or recreating it cannot change their genesis. There is no profile
controller, reconciliation loop, finalizer or additional operator RBAC.

Use a local [profile](../../examples/network/genesis-profile.yaml), or export an
installed resource with an explicitly selected kubeconfig/context:

```bash
kubectl --kubeconfig "$STACKS_KUBECONFIG" --context "$STACKS_CONTEXT" \
  --namespace profiles get stacksgenesisprofile regtest-pox4 -o yaml \
  > /tmp/genesis-profile.yaml
umask 077
go -C operators/network run ./cmd/stacks-environment \
  --namespace=my-network --stacks-image=YOUR_QUALIFIED_STACKS_IMAGE \
  --genesis-profile=/tmp/genesis-profile.yaml \
  --sbtc-contracts=/path/to/sbtc/contracts/contracts > /tmp/my-network.json
```

The output is a private Kubernetes List plus retained provisioning identities. It
contains credentials; keep it outside Git. Profile resources contain public
addresses and allocations only, never private keys.

The generator adds fresh, separate `stacker` and `transfer` accounts by default.
To reuse either named profile account, supply `--account-keys=/absolute/path/keys.json`:
a private JSON object mapping those names to their 32-byte hex seeds (or compressed
Stacks private-key encodings). The derived address must match its profile entry.
Other profile allocations are retained. Missing or mismatched keys are rejected;
provisioning never silently replaces a seeded account. Amounts remain integer
micro-STX throughout Go processing and cross the signing boundary as strings.

## Configuration ownership

The network operator's `internal/profiles/templates/` contains readable node,
signer and common genesis TOML templates. One Go renderer supplies typed
inputs, uses TOML 1.0-compatible string escaping, checks TOML syntax and computes
exact-byte configuration digests. Miner rendering requires a mining public key.
The node role, peers, endpoints, Bitcoin access and signer pairing supply
actor-specific inputs. Every provisioned Stacks node receives the same resolved
epochs, PoX lengths and genesis balances. Signer authentication is paired with
its node. Private actor configurations live in immutable Secrets.

Deploy matching network-operator and transfer-worker builds when shared API
fields change; an older worker can reject the new declaration digest.

The generated public follower profile uses the same renderer. Complete external
ConfigMap/Secret/inline configurations remain explicit expert inputs: the operator
does not read private files to verify their genesis semantics. Network genesis
immutability alone does not validate independently supplied raw TOML. The Go
provisioning path owns and verifies its generated configuration bytes.

Network genesis contains public account identities and balances. Management is
separately declared in `spec.operation`: funding an account does not enroll it.
Each managed account pins its designated key Secret, ingress configuration and
capability consumer. Holder, manager administrator, consensus signer, bridge
and transfer roles remain separate. Actor Pods receive only their own keys.

## Initialization and maintenance

`stacks-environment` renders resources; it does not monitor heights or submit
transactions. Install the separate Bitcoin, transfer and managed-operation
workers and apply the declaration as shown in the
[production guide](stacks-production.md#provision-and-bootstrap).
Capability controllers own initial contract deployment, direct stacking and
renewal. The aggregate controller only compiles their resources.

Go owns protocol observations, account nonce authorization, submission and
receipt accounting. JavaScript remains an offline Stacks SDK adapter for
explicit key/transaction inputs. Unknown submissions are observed by their
recorded TxID and never automatically resubmitted. The account remains reserved
until its consumer durably acknowledges execution evidence.

The standard provisioner always includes the five pinned sBTC sources and
separate deployer identities. Its default schedule activates Epoch 3.4 at height
227 and Epoch 4.0 at 244, with 20-block reward cycles and five prepare blocks.
Explicit epoch schedules are preserved, including schedules that defer 4.0.
The lower-level default genesis recipe still defers 4.0 to 1,000,005; it does
not itself install contract capabilities. There is no PoX-5 provisioning mode.

Full custom schedules and images require qualification: language activation,
initial enrollment windows and Nakamoto/PoX-5 transition cycles must permit the
declared prerequisites. A readiness hold reports the unmet prerequisite without
rewriting desired pause settings. See [managed operation](../design/managed-network-operation.md).

## Verification

Normal Go tests cover rendering, shared-value validation and managed receipt
handling. `make -C operators/network test-sdk` installs the pinned npm dependencies,
runs offline signing tests, and exercises the Go provisioner against the SDK.
`make verify` includes this target and real API-server immutability tests.

## PoX-5 bindings

Optional `spec.genesis.pox5` sets `sbtcContract`, `sbtcRegistryContract`,
`bondAdmin`, and `pauseAdmin` identically on generated nodes. These are immutable
protocol principals; `spec.operation` separately declares the contract and
participation capabilities. See [direct PoX-5 operation](pox5.md). Manually supplied
complete node configurations must use the same
network-level genesis and PoX-5 bindings.
