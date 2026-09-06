# Bitcoin baseline production

The implemented profile offers one regtest block per fixed or jittered policy interval,
selecting among 1–8 declared Bitcoin targets using integer weights. Target
execution is independent of aggregate readiness and of other targets' receipts
or action reservations. It does not bootstrap Stacks or supply transaction demand.

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
  -n "$STACKS_NAMESPACE" get stacksnetworks,bitcoinblockproductions,bitcoinproductiontargets
```

The helper defaults to the pinned `bitcoin/bitcoin:31.1` image index. Its
`--image`, `--interval-seconds`, `--name`, `--address`, and `--target-weights` flags customize the
environment. The default coinbase destination is a test address with no
provided spending key; supply your own regtest address when testing spending.
The generated file contains disposable client credentials. Generate once per
environment; do not reapply newly generated credentials to an existing one.

## Desired policy and ownership

The supported entry point is `StacksNetwork.spec.bitcoinBlockProduction`:

```yaml
bitcoinBlockProduction:
  intervalSeconds: 5
  targets:
    - name: bitcoin
      weight: 1
      address: mipcBbFg9gMiCh81Kj8tqqdgoZub1ZJRfn
    - name: bitcoin-2
      weight: 3
      address: mipcBbFg9gMiCh81Kj8tqqdgoZub1ZJRfn
  paused: false
```

Each name references a logical actor in `spec.bitcoinNodes`. Weights are 1–1000;
intervals are 1–86400 seconds. This example offers one opportunity every five
seconds, with each target receiving a 1/4 or 3/4 share in expectation. Availability
does not change these shares. There is no round-robin or exact-ratio guarantee.
The helper's `--target-weights 1,3` creates this pair with common static RPC
configuration and P2P peer addresses; the default `1` creates one target.

Set optional `jitterSeconds` to sample an inclusive integer-second interval
uniformly from `intervalSeconds − jitterSeconds` through
`intervalSeconds + jitterSeconds`. Omission or zero preserves fixed cadence.
Every possible interval must stay within 1–86400 seconds: jitter must be smaller
than the central interval, and their sum cannot exceed 86400. For example,
`intervalSeconds: 5` with `jitterSeconds: 2` offers intervals of 3–7 seconds.

Timing sampling is independent of weighted target selection. The scheduler
uses policy UID, policy generation, and opportunity ordinal as a stable
internal sampling key, and persists the next due time. Retries cannot redraw
that decision; restart retains committed deadlines. Sampled deadlines use
microsecond precision rounded upward. An opportunity expires at the next
sampled deadline. Pause clears the pending timer; resume and policy edits
start a fresh interval. If parent suspension clears the timer without changing
the policy generation or opportunity ordinal, resume reuses that pending
opportunity's delay sample with a fresh time anchor. It does not promise an
independent draw on every resume. Retries retain the committed anchor.
No user-facing seed or deterministic execution is promised.

The aggregate compiles a same-name `BitcoinBlockProduction` and pins its UID
in `status.bitcoinProductionUID`. A scheduler owns that policy's status and
creates a `BitcoinProductionTarget` named after each compiled Bitcoin leaf.
The policy's `status.targets` permanently pins each execution ledger's UID.
Target workers own execution status and typed RPCs. Change destinations,
weights, membership, cadence, and pause through the parent.

Removed targets retain their ledgers, outstanding RPCs, and admitted action
cleanup obligations. Re-adding the same target reuses its ledger. Removing and
recreating a pinned ledger never grants fresh authorization. Up to sixteen
distinct target identities are retained per policy lifetime; an update exceeding
that limit blocks policy scheduling until the new targets are removed or a
fresh environment is provisioned. Individual missing, replaced, or unavailable
ledgers otherwise affect only their own execution opportunities.

`StacksNetwork.status.conditions` includes `ProductionConfigured`. Its
`ControllerDisabled` reason explains a policy configured while chart production
is disabled; `PolicyUnavailable` reports compilation or ledger problems.
`PolicyReady` means the policy is compiled and the deployment option is enabled,
not that the producer is currently healthy. Inspect the child for execution
state. Production failures do not prevent actor updates, pruning, or readiness
observation; unrelated actor failures likewise do not prevent policy updates.

Each successful dispatch requests exactly one block with bounded `maxtries`.
The scheduler persists a weighted selection and the next due time in one
optimistic-lock status write. The first opportunity, policy updates, and resume
start a fresh interval. Late scheduling publishes at most one opportunity from
the current time, without catch-up. A target consumes its opportunity in the
same status write that arms its RPC. Expired or superseded selections are
skipped, never replayed or reassigned. Preflight failure, action reservation,
unresolved work, or collector exhaustion also skips that target's opportunity.

`status.opportunities` on the policy counts selections. Each retained target
entry counts `offered` selections; its execution ledger records
`opportunitiesConsumed`, `opportunitiesSkipped`, `lastSkipReason`, and
acknowledged `blocksProduced`. Selections made before a ledger can be pinned
are skipped into the root’s `unassignedOpportunities` counter, with the latest
affected name in `lastUnassignedTarget`. Thus root `opportunities` equals the
sum of retained target `offered` counts plus `unassignedOpportunities`. Later
registration does not transfer or replay those skipped selections.
Earlier selections missed by the worker count
as skipped. These are bounded current facts, not a retained scheduling journal.
An already authorized RPC may finish after pause, suspension, or a policy edit.

Admission and preflight get one attempt per opportunity. A transient failure
forfeits that selection; successful preflight must still finish before its
expiry. Short intervals on a slow or resource-constrained host can therefore
mostly skip. Increase the interval or provision adequate resources when sustained
production matters. Cumulative skips can include startup failures and policy
interventions; `lastSkipReason` describes only the latest skip, not a breakdown
or a measured steady-state failure rate. `PolicyChanged` and `Expired` take
precedence over reservation/outstanding reasons when both apply.

`BitcoinNode` configuration does not cause mining. Standalone
production, per-target credential profiles, and in-place recovery remain
unimplemented. Optional [finite generation](bitcoin-generation.md) and
[reorganization](bitcoin-reorganization.md) reserve only their target's executor.

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
| `Running` | The target waits for its next weighted policy opportunity. |
| `Paused` | Current parent intent pauses production or no longer selects this retained target. |
| `Reserved` | A finite action holds this target’s executor between calls or pending receipt acknowledgement. |
| `Blocked` | The ledger cannot safely authorize another request. Inspect `message` and `dispatchState`. |
| `Abandoned` | The owning environment was removed; this is not an execution-recovery claim. |

An optimistic-lock status write records `dispatchState: Armed`, a unique
dispatch ID, and admitted leaf/Pod/container identities before sending.
Receiving one matching block hash atomically records that hash, increments
`blocksProduced`, timestamps completion, and returns the ledger to `Idle`.
The count measures acknowledged production, not canonical height or retained
chain membership. Each target retains bounded dispatch facts and counters.

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
retained receipts; exhausted capacity skips baseline opportunities and stops new arming. Read-only preflight
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

Delete the `StacksNetwork` and wait for both its production policy and every
retained target ledger to disappear **before** uninstalling the chart or deleting
the namespace. Root deletion alone is insufficient: garbage collection can
start target deletion afterwards, and their finalizers still need the worker.
The controllers must remain available to remove their finalizers. As with other Kubernetes
finalizers, removing the controller first can leave deletion pending; an
administrator must then record abandonment and remove that finalizer during
environment disposal. This manual removal must never be used to resume an old
environment. Uninstall only after every deletion wait succeeds.

```bash
# Capture every pinned ledger, including targets removed from the current spec.
read -r -a STACKS_BITCOIN_TARGETS <<< "$(
  kubectl --kubeconfig "$STACKS_KUBECONFIG" --context "$STACKS_CONTEXT" \
    -n "$STACKS_NAMESPACE" get bitcoinblockproduction bitcoin \
    -o jsonpath='{.status.targets[*].resourceName}'
)"
kubectl --kubeconfig "$STACKS_KUBECONFIG" --context "$STACKS_CONTEXT" \
  -n "$STACKS_NAMESPACE" delete stacksnetwork bitcoin
kubectl --kubeconfig "$STACKS_KUBECONFIG" --context "$STACKS_CONTEXT" \
  -n "$STACKS_NAMESPACE" wait --for=delete bitcoinblockproduction/bitcoin \
  --timeout=60s
for target in "${STACKS_BITCOIN_TARGETS[@]}"; do
  kubectl --kubeconfig "$STACKS_KUBECONFIG" --context "$STACKS_CONTEXT" \
    -n "$STACKS_NAMESPACE" wait --for=delete "bitcoinproductiontarget/$target" \
    --timeout=60s
done
```

## Qualification

The [multi-target review ledger](../reviews/bitcoin-multi-target-review.md#live-qualification)
records the weighted profile’s two-node runtime qualification.
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
[`bitcoin-receipt-delay`
fixture](../../operators/network/internal/integration/testdata/bitcoin-receipt-delay/)
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

Cadence validation and qualification limits are recorded in the
[cadence review ledger](../reviews/bitcoin-cadence-review.md).
