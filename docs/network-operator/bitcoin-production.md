# Bitcoin baseline production

The initial implemented profile produces one regtest block on one declared
Bitcoin actor per interval. It works independently of aggregate readiness,
bounded actions, observability, Chaos Mesh, and an agent. It does not bootstrap
Stacks or supply Stacks transaction demand.

## Start a disposable environment

Build and load the network operator image using the
[chart guide](../../charts/stacks-network-operator/README.md). Select an isolated
cluster and a **new namespace**. Install the CRDs, then generate fresh static
credentials and an immutable Bitcoin configuration:

```bash
export STACKS_KUBECONFIG=/absolute/path/to/kubeconfig
export STACKS_CONTEXT=kind-YOUR_KIND_CLUSTER
export STACKS_NAMESPACE=bitcoin-baseline

kubectl --kubeconfig "$STACKS_KUBECONFIG" --context "$STACKS_CONTEXT" \
  apply -f charts/stacks-network-operator/crds

umask 077
GOWORK=off go -C operators/network run ./cmd/bitcoin-environment \
  --namespace "$STACKS_NAMESPACE" > /tmp/bitcoin-environment.json

kubectl --kubeconfig "$STACKS_KUBECONFIG" --context "$STACKS_CONTEXT" \
  create -f /tmp/bitcoin-environment.json

helm install bitcoin charts/stacks-network-operator \
  --kubeconfig "$STACKS_KUBECONFIG" --kube-context "$STACKS_CONTEXT" \
  --namespace "$STACKS_NAMESPACE" \
  --set image.repository=stacks-network-operator --set image.tag=local \
  --set bitcoinProduction.enabled=true

kubectl --kubeconfig "$STACKS_KUBECONFIG" --context "$STACKS_CONTEXT" \
  -n "$STACKS_NAMESPACE" get stacksnetworks,bitcoinblockproductions
```

The helper defaults to the pinned `bitcoin/bitcoin:31.1` image index. Its
`--image`, `--interval-seconds`, `--name`, and `--address` flags customize the
environment. The default coinbase destination is a test address with no
provided spending key; supply your own regtest address when testing spending.
The generated file contains disposable client credentials. Generate once per
environment; do not reapply newly generated credentials to an existing one.

## Desired policy and ownership

The supported entry point is `StacksNetwork.spec.bitcoinBlockProduction`:

```yaml
bitcoinBlockProduction:
  target: bitcoin
  intervalSeconds: 5
  address: mipcBbFg9gMiCh81Kj8tqqdgoZub1ZJRfn
  paused: false
```

`target` is a logical name in `spec.bitcoinNodes`. The aggregate compiles a
same-name `bitcoin.stacks.org/v1alpha1` `BitcoinBlockProduction`, owns its spec,
and permanently pins its UID in `status.bitcoinProductionUID`. A separate
controller owns production status and typed RPC calls. Change cadence,
destination, or pause through the parent. Interval bounds are 1–86400 seconds.
Changing the initial target requires a fresh environment. Removing the policy
pauses production and retains its ledger; re-adding it reuses that ledger.
Admission rejects direct target changes; removing and re-adding the policy
cannot bypass the retained ledger's target identity.

`StacksNetwork.status.conditions` includes `ProductionConfigured`. Its
`ControllerDisabled` reason explains a policy configured while chart production
is disabled; `PolicyUnavailable` reports compilation or ledger problems.
`PolicyReady` means the policy is compiled and the deployment option is enabled,
not that the producer is currently healthy. Inspect the child for execution
state. Production failures do not prevent actor updates, pruning, or readiness
observation; unrelated actor failures likewise do not prevent policy updates.

Each successful dispatch requests exactly one block with a bounded `maxtries`.
The next interval starts after receipt collection. Missed ticks do not queue
catch-up work. Policy updates affect future dispatches; an already authorized
request may finish after a pause, suspension, or update.

The legacy `BitcoinNode` miner/follower field remains compatible and does not
cause mining. Multi-target selection, jitter, weighted policies, standalone
production, bounded actions, and in-place recovery are not implemented.

## Admission and credentials

The aggregate publishes `status.targetDeclarations` before synchronizing
workloads. Its schema version, network UID, generation, leaf name, suspension,
and complete leaf-spec digest describe compiled intent. Publication survives
unrelated readiness, synchronization, and observation failures. Invalid
compilation withdraws it. It is separate from complete admitted inventory;
`inventoryReady`, inventory bytes, and inventory digests retain their meaning.

Production reads the parent, catalog, target, immutable configuration,
StatefulSet, Pod, and container identity directly from the API server. It
requires a current ready Bitcoin target and the approved configuration digest.
It pins the Pod IP for the authorized request and verifies regtest and the
destination before arming. Container command overrides and the old generated
`bitcoin-regtest/v1` shared-password profile are ineligible.

The helper provisions separate producer and observer principals using static
`rpcauth` verifiers, explicit method whitelists, and `rpcwhitelistdefault=1`.
Only the production Deployment mounts the producer password. Bitcoin receives
salted verifiers through its configuration; actor workloads receive no producer
password. Neither controller has Secret-read API permissions. The producer
has no workload mutation permissions. Adding Stacks clients requires a
separately qualified, method-restricted actor configuration; this helper
provisions a Bitcoin-only network.

This initial profile trusts disposable test services and excludes credential
compromise. Credentials remain fixed for the environment. It does not provide
server-process attestation or fence a caller whose process is paused after
authorization. Recorded runtime identity is admission evidence, not proof of
the exact process that eventually executed an ambiguous request.

## Dispatch state and recovery

| State | Meaning |
| --- | --- |
| `Waiting` | Current target, policy, or RPC preflight is unavailable; no new mutation is authorized. |
| `Collecting` | The live process is waiting for its authorized RPC response. |
| `Accounting` | A matching receipt is retained locally pending durable accounting. |
| `Running` | The receipt is accounted; production waits for its next interval. |
| `Paused` | Current parent intent stops new dispatches. |
| `Blocked` | The ledger cannot safely authorize another request. Inspect `message` and `dispatchState`. |
| `Abandoned` | The owning environment was removed; this is not an execution-recovery claim. |

An optimistic-lock status write records `dispatchState: Armed`, a unique
dispatch ID, and admitted leaf/Pod/container identities before sending.
Receiving one matching block hash atomically records that hash, increments
`blocksProduced`, timestamps completion, and returns the ledger to `Idle`.
The count measures acknowledged production, not canonical height or retained
chain membership. Status is bounded to the latest dispatch and a counter.

A lost arm-write acknowledgement sends nothing. Once a request might have
been sent, timeout, response loss, or restart never causes a retry. The live
process retains a received receipt through transient API failures, retries
accounting with five-second attempts, and preserves its original receipt time.
It sends no further mutation until accounting succeeds. An `Armed` record
without durable accounting or surviving local receipt knowledge stays closed
across producer, actor, and Pod replacement. Removing/recreating the production
resource also cannot reopen the pinned ledger. Direct policy deletion retains
a finalizer until the owning network is removed.

Receipt collectors run independently of reconcile workers, without a normal
response deadline. A process has 32 slots shared by outstanding requests and
retained receipts; exhausted capacity stops new arming. Read-only preflight
has a ten-second timeout and connections have a three-second dial timeout.
A surviving slow response therefore remains collectible; a closed connection
does not. Pause stops future dispatches and does not cancel a collector.

SIGTERM stops new arming and allows 25 seconds to drain transport and accounting
together. The manager allows 30 seconds to shut down and its Pod allows 45
seconds. These bounds limit the client, not Bitcoin execution. A crash,
exhausted grace period, or lost response can still leave production blocked.
Process-local receipts cannot be reconstructed after a restart.

For a blocked environment, preserve its status and recreate in a **new
namespace with new UIDs, fresh credentials, and no reused data/configuration**.
Deleting the owning network makes a bounded best-effort abandonment status
write and removes the producer's finalizer. Teardown does not wait for proof
that an old RPC stopped. A new namespace alone does not fence IP reuse; fresh
credentials are part of environment provisioning.

Delete the `StacksNetwork` and wait for its production resource to disappear
**before** uninstalling the chart or deleting the namespace. Its controller
must remain available to remove the ledger finalizer. As with other Kubernetes
finalizers, removing the controller first can leave deletion pending; an
administrator must then record abandonment and remove that finalizer during
environment disposal. This manual removal must never be used to resume an old
environment.

```bash
kubectl --kubeconfig "$STACKS_KUBECONFIG" --context "$STACKS_CONTEXT" \
  -n "$STACKS_NAMESPACE" delete stacksnetwork bitcoin
kubectl --kubeconfig "$STACKS_KUBECONFIG" --context "$STACKS_CONTEXT" \
  -n "$STACKS_NAMESPACE" wait --for=delete bitcoinblockproduction/bitcoin \
  --timeout=60s
```

## Qualification

The [initial qualification record](bitcoin-qualification.md) records the tested
Core/platform combination, observed results, and remaining limits.

The live baseline suite checks advancement, pause, cadence update, unrelated
Stacks failure, and graceful producer rollout against real Bitcoin:

```bash
STACKS_NETWORK_LIVE_KUBECONFIG="$STACKS_KUBECONFIG" \
STACKS_NETWORK_LIVE_CONTEXT="$STACKS_CONTEXT" \
STACKS_BITCOIN_LIVE_NAMESPACE="$STACKS_NAMESPACE" \
STACKS_BITCOIN_LIVE_NETWORK=bitcoin \
GOWORK=off go -C operators/network test -tags=live,bitcoinproduction -count=1 -v \
  ./internal/integration -run '^TestLiveBitcoinBaseline$'
```

The suite temporarily changes the dedicated network and returns it to a
Bitcoin-only five-second policy. The separate
[`bitcoin-receipt-delay` fixture](../../operators/network/internal/integration/testdata/bitcoin-receipt-delay/)
wraps real Core 31.1 and delays the first generation receipt for 15 seconds.
Build it with its directory as Docker context and provision a fresh namespace
using the helper's `--image` flag. Set the generated parent's policy to
`paused: true` before creating it. `TestLiveBitcoinDelayedReceipt` unpauses it
and checks eventual accounting. In another fresh, initially paused environment,
`TestLiveBitcoinInFlightShutdown` waits for the fixture's actual server receipt,
deletes the producer Pod, observes a pending collector in the manager drain,
and checks that the same dispatch is accounted once.

Build a separate fixture image with `--build-arg RECEIPT_FAULT=drop` to close the
first generation response connection after Core returns. In another fresh
namespace, `TestLiveBitcoinAmbiguousReceipt` verifies that this request remains
blocked across producer replacement. `TestLiveBitcoinAbandonment` then deletes
that dedicated network and verifies ledger cleanup. These test-only images
are not production adapters or Chaos profiles. Select tests individually; the
existing generic `live` suite remains separate.
