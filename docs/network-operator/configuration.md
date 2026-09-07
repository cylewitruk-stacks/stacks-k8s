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
contributes to genesis identity; bootstrap records the observed genesis hash.

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
  --genesis-profile=/tmp/genesis-profile.yaml > /tmp/my-network.json
```

The output is a private Kubernetes List plus external bootstrap inputs. It
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

Network genesis contains public account identities and balances. Provisioning
selects the transfer sender from that same snapshot and binds the worker's
separate credential to the ingress configuration digest. Bootstrap selects the
named stacker and verifies the network's genesis digest before mutation. Actor
Pods never receive the transfer key or Bitcoin bootstrap capability.

## Bootstrap and maintenance

`cmd/stacks-environment`, `cmd/stacks-bootstrap` and `cmd/stacks-maintain-signers`
are external Go commands. They share provisioning/configuration code; Kubernetes
controllers do not run them. Follow the [production guide](stacks-production.md#provision-and-bootstrap)
for the deployment sequence. Node.js remains necessary only for the Stacks SDK's
offline key encoding and signing adapters in `transactions/`.

Go bootstrap selects an explicit kubeconfig/context; maintenance uses an
explicit existing loopback port-forward. Go owns reads, bounded waits, transaction submission and
public evidence. The SDK receives explicit account, nonce, fee and protocol
inputs over stdin, returns signed bytes, and performs no network discovery.
Go independently checks the transaction hash and requires an exact TxID receipt.
An uncertain submission exits without replay. Only one external helper may own
the bootstrap/stacking account at a time; evidence records authorization before
submission. Maintenance never reenrolls an expired lock.

Each invocation exclusively claims a new evidence file before any mutation,
including direct calls to the Go library. Defaults are
`<manifest>.bootstrap-evidence.json` and `<manifest>.renewal-evidence.json`.
Existing files are preserved and cause an error; use a fresh `--evidence` path
for each subsequent invocation. Records atomically replace that invocation's
snapshot with owner-only permissions. Evidence files do not provide account
locking or permission to retry a prior uncertain mutation.

A native HTTP 400 rejection must match the submitted TxID and rejection envelope
to report `FeeTooLow`, `BadNonce`, or `Other`. Raw server detail is discarded.
Other failed responses remain unconfirmed. Both outcomes stop the helper
without retry; an ingress rejection does not prove permanent non-execution.

This delivery retains the qualified PoX-4 schedule: Epoch 3.4 at burn height 227,
Epoch 4.0 at 1,000,005, 20-block reward cycles including a five-block prepare
phase. Configuration can represent other schedules; the external bootstrap
rejects schedules outside this qualified profile before mutation.

The next delivery must provision the required sBTC contracts and qualified
contract principals, activate Epoch 4.0 after 3.4, and verify PoX-5 activation
before qualifying faults. Changing the activation height alone is insufficient.

## Verification

Normal Go tests cover rendering, shared-value validation and bootstrap receipt
handling. `make -C operators/network test-sdk` installs the pinned npm dependencies,
runs offline signing tests, and exercises the Go provisioner against the SDK.
`make verify` includes this target and real API-server immutability tests.
