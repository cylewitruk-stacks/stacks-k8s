# Historical M0.4 Bitcoin lifecycle baseline

This archived design records the reviewed M0.4 baseline. It is superseded by
the [current Bitcoin design](../bitcoin-lifecycle.md) and
[steady-state operation amendment](../steady-state-operation.md). Its
single-target schema, whole-network admission, shared observation credentials,
and deadline-based recovery rules are not implementation authority. The
associated fixtures retain this baseline for review history only.

The source snapshot is
[`6bf26f6`](https://github.com/cylewitruk-stacks/stacks-k8s/blob/6bf26f614a828d8d30a62b615150eece2a658bc5/docs/design/bitcoin-lifecycle.md).
Historical action semantics are pinned to that commit below; links to current
designs in this banner are navigation, not part of the reviewed baseline.

## Status and scope

This document is the normative M0.4 design for continuous Bitcoin block
production and the first custom Bitcoin action controllers. None of these CRDs
or controllers is implemented yet.

`StacksNetwork` owns Bitcoin topology. `BitcoinBlockProduction` maintains
mutable, long-lived baseline block production for one admitted `BitcoinNode`.
The two action resources perform one immutable, bounded regtest operation.
An external agent creates, changes, sequences, overlaps, observes, and
interprets these resources. Natural reorganizations remain observed network
behavior; a controller-forced reorganization is an action, not desired
topology.

Only the two bounded kinds follow the
[atomic action contract](https://github.com/cylewitruk-stacks/stacks-k8s/blob/6bf26f614a828d8d30a62b615150eece2a658bc5/docs/design/actions.md).
Their versioned mechanism contract is
[`bitcoin-actions-v1.json`](../../../contracts/bitcoin-actions-v1.json).
Its identifier is `actions.stacks.org/bitcoin-actions/v1alpha1`.
Continuous production instead follows
[`bitcoin-block-production-v1.json`](../../../contracts/bitcoin-block-production-v1.json),
identified by `bitcoin.stacks.org/block-production/v1alpha1`. All three share
[`bitcoin-reservation-v1.json`](../../../contracts/bitcoin-reservation-v1.json),
identified by `bitcoin.stacks.org/reservation/v1alpha1`.

## Resource inventory

| Resource | Purpose |
| --- | --- |
| `BitcoinBlockProduction` | Maintain mutable fixed or uniform-random baseline block production until paused or deleted. |
| `BitcoinBlockGeneration` | Generate an immutable, finite number of blocks immediately or at a bounded cadence. |
| `BitcoinReorganization` | Replace one finite regtest suffix with a longer, higher-work branch. |

Working API groups and versions are `bitcoin.stacks.org/v1alpha1` for
continuous production and `actions.stacks.org/v1alpha1` for finite actions.

## Shared controller boundary

One action-operator binary may initially register all three controllers, even
though continuous production has a separate API group and lifecycle. Each kind
has its own API type, controller registration, mechanism package, tests,
examples, and reference page. Shared packages may provide:

- action lifecycle and condition helpers;
- admitted network and leaf identity resolution;
- the target reservation protocol;
- bounded Bitcoin RPC transport; and
- chain, RPC-attempt, and attribution status types.

There is no generic RPC action, untyped mechanism registry, scenario
controller, or ordered execution plan. Continuous production is desired
operation, not an action, administrator safety policy, or scheduler for other
resources. The network and observability operators are not runtime
dependencies of mutation beyond their published Kubernetes APIs.

## Common admission

While a bounded action remains `Pending`, either action controller must:

1. resolve `networkRef` and `bitcoinNodeRef` using the identity join in the
   atomic action contract;
2. require a complete, current admitted inventory and a Ready `BitcoinNode`;
3. require the leaf role to be `miner`;
4. resolve the Service name and RPC port from admitted topology rather than
   from the action spec;
5. require the aggregate and compiled leaf `spec.bitcoinRPCAuth` identities to
   agree, then read only the fixed credential Secret described below;
6. call `getblockchaininfo` and require `chain == "regtest"`;
7. call `validateaddress` for `destinationAddress` and require it to be valid
   on the target node;
8. validate every hard kind bound; and
9. prepare the network, target, policy, request, chain, and credential input
   identity that admission will bind.

It then attempts the target reservation. A busy target remains
`Pending/TargetBusy`. After acquisition, the controller repeats the uncached
identity and chain checks, records both the admitted identity and reservation,
and only then enters `Admitted`. No RPC intent may exist before that status
write succeeds.

The controller repeats uncached Kubernetes identity reads immediately before
every material RPC batch. Divergence before the first side effect is
`Failed/IdentityDiverged`; divergence afterward is
`Inconclusive/IdentityDiverged`.

### Shared mechanism status types

A chain identity has exactly `height`, `bestBlockHash`, and `chainwork`.
A reservation status has `leaseName`, `holderIdentity`,
`acquisitionToken`, `acquiredAt`, `renewedAt`, and `rpcNotAfter`.
`releasedAt` is set after the controller proves its compare-and-swap release;
the terminal action retains the reservation record.

Each `rpcAttempts` entry has `sequence`, `method`, `expectedHeight`,
`expectedTip`, `requestedBlocks`, `destinationAddress`,
`acquisitionToken`, `startedAt`, `deadline`, `outcome`, and
`blockHashes`. Outcome is one of:

| Outcome | Meaning |
| --- | --- |
| `IntentRecorded` | Durable intent exists; no RPC result has been reconciled. |
| `Acknowledged` | RPC returned successfully and its result was verified. |
| `ProvenAbsent` | Every known tip proves the intended effect did not occur. |
| `Ambiguous` | Effect or attribution cannot be established safely. |

`rpcAttempts` records mutation methods only. Admission and verification reads
are represented by their resulting admitted or observed facts rather than as
an unbounded request transcript.

## `BitcoinBlockGeneration`

### Examples

Fixed cadence delays before every block, including the first:

```yaml
apiVersion: actions.stacks.org/v1alpha1
kind: BitcoinBlockGeneration
metadata:
  name: bitcoin-a-cadence
  labels:
    actions.stacks.org/correlation-id: investigation-42
spec:
  networkRef:
    name: mixed-network
  bitcoinNodeRef:
    name: mixed-network-bitcoin-a
  blocks: 120
  cadence:
    fixed:
      interval: 1m
  destinationAddress: "<unique-regtest-address>"
  timeout: 3h
```

An immediate burst may batch several blocks into one RPC:

```yaml
apiVersion: actions.stacks.org/v1alpha1
kind: BitcoinBlockGeneration
metadata:
  name: bitcoin-b-flash
spec:
  networkRef:
    name: mixed-network
  bitcoinNodeRef:
    name: mixed-network-bitcoin-b
  blocks: 20
  cadence:
    immediate:
      batchSize: 10
  destinationAddress: "<another-unique-regtest-address>"
  timeout: 2m
```

An explicit sequence temporarily replaces baseline production with one delay
per requested block. After the action releases its reservation, continuous
production resumes independently:

```yaml
apiVersion: actions.stacks.org/v1alpha1
kind: BitcoinBlockGeneration
metadata:
  name: bitcoin-a-sequence
spec:
  networkRef:
    name: mixed-network
  bitcoinNodeRef:
    name: mixed-network-bitcoin-a
  blocks: 3
  cadence:
    sequence:
      delays:
        - 15s
        - 20s
        - 3s
  destinationAddress: "<unique-regtest-address>"
  timeout: 1m
```

### Spec

| Field | Go representation | Contract |
| --- | --- | --- |
| `networkRef` | shared local reference | Exact same-namespace `StacksNetwork`. |
| `bitcoinNodeRef` | shared local reference | Exact compiled `BitcoinNode` resource name. |
| `blocks` | `int32` | Required, `1..288`. |
| `cadence` | typed union | Required; exactly one of `immediate`, `fixed`, `uniform`, or `sequence`. |
| `destinationAddress` | `string` | Required bounded regtest address, length `14..90`; runtime-validated by Bitcoin Core. |
| `timeout` | `metav1.Duration` | Required creation-relative bound, positive and no greater than `24h`. |

The entire spec is immutable from creation with `self == oldSelf`. All limits
are absolute until a separately reviewed administrator safety policy exists.
The API does not expose wallet names, RPC endpoints, credentials, raw methods,
or `maxtries`.

The cadence union has this exact shape:

| Branch | Fields and bounds | Semantics |
| --- | --- | --- |
| `immediate` | `batchSize`, required `1..16` and no greater than `blocks` | Generate immediately in batches of at most `batchSize`. |
| `fixed` | `interval`, required `100ms..5m` | Select the same delay before each block. |
| `uniform` | `minimumInterval`, `maximumInterval`, each `100ms..5m`, minimum no greater than maximum | Select an independent duration uniformly over the inclusive integer-nanosecond range before each block. |
| `sequence` | `delays`, exactly `blocks` entries, each `100ms..5m` | Apply the listed delay before each corresponding block. |

CEL requires exactly one branch, constrains an immediate batch to the requested
block count, and requires the sequence length to equal `blocks`. Scheduled
branches always issue one block per RPC. Duration schemas remain strings with
no OpenAPI `format`; CEL compares them through `duration(...)`.

Uniform selection uses an injected entropy interface backed by `crypto/rand`
and unbiased integer rejection sampling. The selected duration is durable
action control state, but neither its selection nor the distributed result is
claimed reproducible. The action may expire with partial progress; timeout
does not imply that the requested cadence fits completely.

### Mechanism status

In addition to common action status, the kind owns:

| Field | Contract |
| --- | --- |
| `requestedBlocks` | Immutable admitted count. |
| `generatedBlocks` | Number of hashes attributed to this action. |
| `generatedBlockHashes` | Ordered, unique hashes, maximum 288. |
| `cadence` | Current `mode`, `selectedInterval`, `scheduledForBlock`, and `dueAt`. |
| `startingChain` | Height, best-block hash, and chainwork observed at admission. |
| `observedChain` | Latest height, best-block hash, and chainwork verified by the controller. |
| `reservation` | Retained Lease identity, deadlines, and proven release time. |
| `rpcAttempts` | Ordered bounded intent/receipt records, maximum 320. |
| `attribution` | `RPCResponse` or `Uncertain`. |

An RPC attempt records sequence, method, expected height and tip, requested
count, destination, acquisition token, start time, deadline, outcome, and any
returned or candidate hashes. For scheduled modes, `cadence` is persisted
before waiting, so restart resumes the accepted due time instead of drawing or
starting the same delay again. The Pending RPC intent is persisted immediately
before mutation. Status never contains credentials or raw RPC payloads.

### Reconciliation

The controller performs these level-based steps:

1. establish admission and absolute expiry;
2. acquire the target reservation;
3. re-read reservation and target identity;
4. append and persist one bounded RPC intent;
5. invoke `generatetoaddress` with the admitted destination and batch count;
6. verify returned hashes, ancestry, height, and increasing chainwork;
7. persist the receipt and cumulative progress; and
8. persist or clear cadence control state and requeue for the next block or
   immediate batch.

The typed read surface is `getblockchaininfo`, `validateaddress`,
`getblockhash`, `getblockheader`, bounded `getblock` with verbosity `2`, and
`getchaintips`. The sole mutation method is `generatetoaddress`.

The [Bitcoin Core RPC](https://bitcoincore.org/en/doc/26.0.0/rpc/generating/generatetoaddress/)
returns one hash for every generated block. Only hashes returned in that
successful response support `RPCResponse` attribution and `Completed`.

A transport timeout or cancelled client context does not cancel server-side
Bitcoin Core execution. After any lost mutation response, the controller
waits until `rpc-not-after`, reads every known branch through `getchaintips`,
and inspects only the bounded descendant range through verbosity-2 `getblock`.
It records candidate hashes but never converts them into acknowledged hashes.
If every known tip proves the intended effect absent, the attempt becomes
`ProvenAbsent` and may be retried within the original action bounds. Otherwise
it becomes `Ambiguous`, attribution becomes `Uncertain`, and the action ends
`Inconclusive/EffectUncertain`.

A dedicated destination remains recommended because it improves operator and
observer diagnosis. It is not an admission invariant or a correctness basis.
Suspected direct-RPC mutation remains apparatus interference.

`Completed` requires exactly `blocks` ordered hashes and a verified final
chain observation. Partial attributable progress followed by a definite
failure is `Failed`; an ambiguous call or attribution is `Inconclusive`.
Deletion stops new calls and waits only for the current bounded RPC context.
Generated blocks are irreversible and are never removed as cleanup.

## `BitcoinBlockProduction`

### Purpose and API boundary

`BitcoinBlockProduction` keeps a regtest network progressing without requiring
an agent to create one action per block. It is mutable desired operation, not a
bounded action, workflow, schedule of actions, or administrator safety policy.
It therefore uses `bitcoin.stacks.org/v1alpha1`, does not implement the atomic
action phases, and has no timeout or completion outcome.

The resource name must equal `spec.bitcoinNodeRef.name`. Because compiled leaf
names are unique within a namespace, this CEL rule permits at most one
production resource for a Bitcoin node without a dynamic admission webhook.

### Examples

Fixed ten-second baseline production:

```yaml
apiVersion: bitcoin.stacks.org/v1alpha1
kind: BitcoinBlockProduction
metadata:
  name: mixed-network-bitcoin-a
spec:
  networkRef:
    name: mixed-network
  bitcoinNodeRef:
    name: mixed-network-bitcoin-a
  destinationAddress: "<baseline-regtest-address>"
  cadence:
    fixed:
      interval: 10s
  paused: false
```

Continuously varying production between three and thirty seconds:

```yaml
apiVersion: bitcoin.stacks.org/v1alpha1
kind: BitcoinBlockProduction
metadata:
  name: mixed-network-bitcoin-b
spec:
  networkRef:
    name: mixed-network
  bitcoinNodeRef:
    name: mixed-network-bitcoin-b
  destinationAddress: "<another-baseline-regtest-address>"
  cadence:
    uniform:
      minimumInterval: 3s
      maximumInterval: 30s
  paused: false
```

### Spec and mutability

| Field | Mutability | Contract |
| --- | --- | --- |
| `networkRef` | Immutable | Exact same-namespace `StacksNetwork`. |
| `bitcoinNodeRef` | Immutable | Exact compiled `BitcoinNode`; must equal the resource name. |
| `destinationAddress` | Immutable | Required regtest address, length `14..90`, runtime-validated. |
| `cadence` | Mutable | Exactly one fixed or uniform branch. |
| `paused` | Mutable | Defaults to `false`; stops future production after the current bounded call settles. |

Fixed cadence has one `interval`. Uniform cadence has
`minimumInterval` and `maximumInterval`, with minimum no greater than maximum.
Every duration is `1s..1h`; the lower bound limits indefinite control-plane and
chain growth, while the upper bound permits a mainnet-like ten-minute baseline.
The selected delay applies before every block, including the first block after
creation, resume, or cadence change.

Only `cadence` and `paused` are mutable. CEL transition rules keep both
references and the destination unchanged. A cadence update cancels an idle
timer and selects a new delay from the update time. An update during an RPC is
admitted after that call returns or reaches its recorded deadline. The
controller never catches up missed intervals with a burst.

The due time is process memory, not CRD status. It is keyed by namespace,
name, object UID, and `activeCadenceGeneration`, and is driven with
`RequeueAfter` rather than a goroutine. Every reconcile evaluates the current
key and due time, so an old queued request can cause only an early check. A
missing in-memory entry after process restart selects a fresh full delay.

Uniform selection uses the same injected `crypto/rand`-backed interface as the
finite action. The selected interval and resulting block are observations, not
replay promises.

### Status

Production phases are `Pending`, `Running`, `Paused`, `Degraded`, and
`Terminating`. Conditions are `TargetResolved`, `CredentialsReady`,
`CadenceAccepted`, and `Ready`; every condition carries
`observedGeneration`.

`Pending` applies before the first successful network and target identity
bind. After that bind, a target that is not currently safe to mutate is
`Degraded`. A normal rollout may return from `Degraded` after Ready identity is
admitted again.

Status contains only:

- `observedGeneration`, `phase`, and `conditions`;
- initially bound `admittedNetwork` and `admittedTarget` object UIDs plus the
  current admitted runtime identity;
- `activeCadenceGeneration`;
- cumulative `acknowledgedBlocks` and `ambiguousAttempts` counters;
- `lastSelectedInterval`, `lastAcknowledgedAt`, and `lastAmbiguousAt`; and
- `recentBlockHashes`, an ordered ring of at most eight acknowledged hashes.

Counters describe only responses observed and flushed by this controller.
They are not a chain-height total and may undercount after ambiguity or a
process failure before the next summary flush. The controller flushes
accumulated summaries at least every 30 seconds or 16 acknowledged blocks and
immediately on phase, condition, generation, pause, ambiguity, or deletion
transitions. It never appends unbounded history to status.

The initial network and leaf UIDs remain bound for the resource lifetime, so
same-name replacement requires a new production object. Image, Pod, config,
and inventory changes under those objects stop mutation until the topology is
Ready, then refresh admitted runtime identity and resume. This supports normal
upgrades without silently following replacement API objects.

Replacing the bound `StacksNetwork` UID reports
`Degraded/NetworkIdentityChanged`; replacing the bound `BitcoinNode` UID
reports `Degraded/TargetIdentityChanged`. Both reasons instruct the agent to
delete and recreate `BitcoinBlockProduction`, including when the network and
producer were originally applied from one multi-document manifest.

### Reconciliation and ambiguity

The controller:

1. resolves and binds current topology and credential identity;
2. validates regtest, miner role, destination, and cadence;
3. waits the selected interval without holding the target reservation;
4. performs an uncached priority lookup for bounded actions on the target;
5. acquires the shared reservation for exactly one one-block RPC, recording
   `rpc-not-after` in the acquisition write;
6. revalidates the uncached holder and target identity;
7. calls `generatetoaddress` for one block; and
8. releases the reservation, updates bounded summaries, and selects the next
   interval from release time.

A successful response contributes its returned hash to acknowledged status.
There is no per-block intent journal and no completion claim. On a lost
response, the handle remains reserved until `rpc-not-after`; the controller
then increments ambiguity, releases, and schedules a later new production
call. It never retries the uncertain call or infers acknowledgement from a tip.
Client cancellation still does not imply server-side cancellation.

Structured logs record each selected interval, acquisition token, response
classification, and returned hash without credentials. Kubernetes Events
cover lifecycle and ambiguity transitions rather than every block. Passive
observability may retain the structured stream and independently observe chain
facts according to its declared source coverage; neither status nor Events are
treated as a complete history.

### Pause, deletion, and bounded-action priority

When `paused` becomes true, no new timer or acquisition starts. An existing
call is allowed to return or reach `rpc-not-after`, after which the controller
releases and reports `Paused`. Resuming starts a fresh full cadence delay.

Deletion sets `Terminating`, prevents new work, cancels the handle-derived
client context, waits through the recorded deadline because Bitcoin Core may
still execute, releases or safely abandons the Lease, and then removes
`bitcoin.stacks.org/block-production`. Deletion does not claim to remove a
generated block.

Immediately before every acquisition, the producer performs an uncached list
for `BitcoinBlockGeneration` and `BitcoinReorganization` objects in
`Admitted`, `Active`, or `Recovering` that reference the same Bitcoin node. A
`Pending` action yields only when its admission condition has reason
`TargetBusy`, meaning all pre-reservation checks passed and it is waiting only
for the target reservation. Invalid, not-Ready, or otherwise stuck `Pending`
actions do not stop baseline production.

If an eligible action exists, production skips acquisition and requeues. If an
action is created after that lookup, Lease exclusion still limits the producer
to the single in-flight call; after release it waits a newly selected interval
before competing again. The action controller watches managed Leases and maps
their target-UID annotation to waiting actions through a field index, so a
release triggers prompt reconciliation. This is priority for bounded work,
not action sequencing by the producer.

## `BitcoinReorganization`

### Example

```yaml
apiVersion: actions.stacks.org/v1alpha1
kind: BitcoinReorganization
metadata:
  name: replace-two-blocks
  labels:
    actions.stacks.org/correlation-id: investigation-42
spec:
  networkRef:
    name: mixed-network
  bitcoinNodeRef:
    name: mixed-network-bitcoin-b
  depth: 2
  replacementBlocks: 3
  replacementInterval: 250ms
  destinationAddress: "<unique-regtest-address>"
  boundaryPolicy:
    allowEpochBoundaryCrossing: true
    allowRewardCycleBoundaryCrossing: true
    allowPreparePhaseBoundaryCrossing: true
  timeout: 2m
```

### Spec

| Field | Go representation | Contract |
| --- | --- | --- |
| `networkRef` | shared local reference | Exact same-namespace `StacksNetwork`. |
| `bitcoinNodeRef` | shared local reference | Exact compiled `BitcoinNode` resource name. |
| `depth` | `int32` | Required, `1..144`, and no greater than the admitted tip height. |
| `replacementBlocks` | `int32` | Required, `2..288`, and strictly greater than `depth`. |
| `replacementInterval` | `*metav1.Duration` | Optional delay between replacement blocks, `100ms..5m`; OpenAPI `format` remains unset. |
| `destinationAddress` | `string` | Required bounded regtest address, length `14..90`; runtime-validated. |
| `boundaryPolicy` | typed object | Three required explicit protocol-boundary opt-ins. |
| `timeout` | `metav1.Duration` | Required creation-relative bound, positive and no greater than `24h`. |

The spec is immutable from creation. With ordinary regtest blocks,
`replacementBlocks > depth` gives the replacement branch more blocks than the
replaced suffix. The final chainwork comparison, rather than block count, is
the authoritative higher-work proof after the original branch is
reconsidered. The API exposes no generic RPC or cleanup switch.

The affected interval is inclusive from `originalHeight - depth + 1` through
`originalHeight - depth + replacementBlocks`. It therefore covers the
replaced suffix and any new height reached by the longer branch. Epoch,
reward-cycle, and PoX prepare-phase crossings each require their corresponding
opt-in.

V1 has no trusted protocol-schedule source and always records the schedule as
unknown. Admission therefore requires all three opt-ins to be `true` for every
`BitcoinReorganization`; uncertainty is not treated as a safe interval. A
future trusted schedule may make the individual opt-ins selective without
changing their meaning. The admitted assessment is retained in status.

| Boundary policy field | Meaning when `true` |
| --- | --- |
| `allowEpochBoundaryCrossing` | Permit the affected interval to include an epoch boundary; required in v1. |
| `allowRewardCycleBoundaryCrossing` | Permit the affected interval to include a PoX reward-cycle boundary; required in v1. |
| `allowPreparePhaseBoundaryCrossing` | Permit the affected interval to include a PoX prepare-phase boundary; required in v1. |

### Mechanism status

| Field | Contract |
| --- | --- |
| `originalChain` | Height, best-block hash, and chainwork before mutation. |
| `forkParent` | Height, hash, previous hash, and chainwork of the retained parent. |
| `originalBlockHashes` | Ordered replaced suffix, maximum 144. |
| `replacementBlockHashes` | Ordered replacement branch, maximum 288. |
| `finalChain` | Final observed canonical height, tip, and chainwork. |
| `boundaryAssessment` | Known/unknown schedule and every detected crossing class. |
| `reservation` | Retained Lease identity, deadlines, and proven release time. |
| `rpcAttempts` | Ordered bounded intent/receipt records, maximum 320. |
| `attribution` | `RPCResponse` or `Uncertain`. |

### Reconciliation

After shared admission and reservation, the controller:

1. captures the exact canonical suffix and validates its ancestry and
   increasing chainwork;
2. persists the prepared branch and boundary assessment;
3. revalidates that height, tip, and chainwork are unchanged;
4. persists intent and calls `invalidateblock` on the first replaced block;
5. proves the active tip is the retained fork parent;
6. generates replacement blocks one at a time, persisting intent and receipt
   around every call;
7. persists intent and calls `reconsiderblock` for the original branch; and
8. proves the replacement branch is canonical, has increasing chainwork, and
   exceeds the original chainwork.

`getblockchaininfo`, `validateaddress`, `getblockhash`, `getblockheader`,
bounded verbosity-2 `getblock`, and `getchaintips` are the read surface.
`invalidateblock`, `generatetoaddress`, and `reconsiderblock` are the only
mutation methods. The action does not create a partition or observe downstream
Stacks recovery. An agent composes a separate native `NetworkChaos` resource
when it wants split views.

Every mutation has a persisted intent. A lost response is always
`Ambiguous`; cancellation alone never proves absence and a reconstructed
effect never becomes acknowledged success. The controller reads every known
tip and the exact prepared branch after `rpc-not-after`, records candidate
state, and may perform only a state-proven compensating `reconsiderblock`. It
never repeats an unproven invalidation, generation, or reconsideration call.
The action ends `Inconclusive` and retains all known hashes and receipts.

`Completed` means every mutation returned successfully, the replacement is
the target node's canonical higher-work branch, and the original invalidity
marker was removed. If `reconsiderblock` succeeds but an externally extended
original branch wins, the definite outcome is `Failed/MechanismFailed`.
Completion does not claim that other Bitcoin or Stacks nodes adopted the
branch. Deletion stops further work but cannot restore consensus history;
terminal handling follows the irreversible-action rules in the atomic
contract.

## Per-target reservation protocol

Leader election prevents two active operator leaders, but it does not fence an
old RPC call or serialize continuous production with separate action kinds.
All three controllers therefore use one `coordination.k8s.io/v1` Lease per
admitted Bitcoin leaf UID. The neutral protocol is pinned by
`bitcoin.stacks.org/reservation/v1alpha1`.

### Identity and constants

| Element | Frozen value |
| --- | --- |
| Namespace | Action and target namespace. |
| Lease name | `bitcoin-mutation-<first-32-hex-sha256(namespace-NUL-targetUID)>`. |
| Holder identity | `<holder-uid>/<32-lowercase-hex-acquisition-token>`. |
| Acquisition token | 16 cryptographically random bytes, new for every successful acquisition. |
| Lease duration | 30 seconds. |
| Renewal period | 10 seconds while the action may issue RPC calls. |
| Maximum RPC context | 10 seconds. |
| Manager lifecycle | `leader-gated-manager-runnable`. |
| Expiry observation | `local-observation-of-unchanged-record`. |
| RPC context parent | `reservation-handle`. |
| Lease write owner | `reservation-handle`. |
| Owner reference | Admitted `BitcoinNode` UID, `controller=false`, `blockOwnerDeletion=false`. |
| Managed-by label | `app.kubernetes.io/managed-by=stacks-action-operator`. |
| Annotations | Holder kind/UID, target UID, acquisition token, and `rpc-not-after`, all under `bitcoin.stacks.org`. |

The exact annotations are `bitcoin.stacks.org/holder-kind`,
`bitcoin.stacks.org/holder-uid`, `bitcoin.stacks.org/acquisition-token`,
`bitcoin.stacks.org/rpc-not-after`, and
`bitcoin.stacks.org/target-uid`. Allowed holder kinds are
`BitcoinBlockProduction`, `BitcoinBlockGeneration`, and
`BitcoinReorganization`.

The keyed in-process mutex is only a contention optimization. Correctness
comes from optimistic API-server updates and uncached Lease reads. The mutex
may be held across a bounded RPC batch because it is keyed by target UID.
The action operator process is the sole Lease writer. It refuses a
pre-existing Lease whose name, managed-by label, target owner UID, or target
annotation differs; it never adopts or deletes a foreign object.

### Reservation manager

A shared `ReservationManager` owns active handles for continuous production
and both Bitcoin action controllers. It is registered with controller-runtime
through `manager.Add` as a leader-gated Runnable whose `NeedLeaderElection`
result is `true`.
Manager-context cancellation on leader loss closes every handle and cancels
all RPC contexts derived from those handles.

The manager renews due handles independently of controller workqueues. A
reconciler's `RequeueAfter` controls action progress only; worker count and
queue latency are not reservation-safety properties. Each successful
acquisition returns one handle bound to Lease name, holder kind, holder UID,
target UID, and acquisition token.

Acquisition itself begins with a pending handle; that handle performs the
create or compare-and-swap and is returned to the reconciler only after it
owns the reservation. This keeps acquisition inside the same single-writer
boundary as later updates.

Every Lease write goes through that handle. The handle serializes renewal,
`rpc-not-after` updates, and release, and every write performs the same-holder
resource-version check. Reconcilers never write the Lease directly. Mutation
RPC contexts derive from the handle context rather than the reconcile context,
so reservation or leader loss cancels the client call directly. Cancellation
does not imply that Bitcoin Core stopped server-side execution; the persisted
intent must still be reconciled.

### Acquisition and renewal

- Acquire an absent, explicitly released, or expired Lease with a
  resource-version compare-and-swap.
- On first observing another holder, record local observation time. Reset that
  time whenever the relevant Lease record changes, and consider acquisition
  only after the same record remains unchanged locally for the full 30-second
  Lease duration. A controller restart therefore waits a fresh full duration.
- Treat `bitcoin.stacks.org/rpc-not-after` as a recorded holder deadline that
  may extend caution but can never permit acquisition earlier than the local
  unchanged-record interval.
- Bounded actions persist holder identity in action status before any RPC
  intent. Production records its RPC deadline in the acquisition write.
- Register the acquired handle before entering `Admitted`; failure to register
  releases the Lease or leaves it to expire without issuing RPC.
- Renew through the handle every 10 seconds while the action may issue calls.
- Immediately before each mutation RPC, perform an uncached Lease read and
  require the exact holder and unexpired times.
- For each bounded-action mutation, persist the RPC intent, update
  `rpc-not-after` to that intent's deadline through holder-checked
  compare-and-swap, re-read the Lease, and only then invoke the typed client.
  Production acquires separately for each block with its deadline already
  present, then performs the same uncached holder read.
- Close the handle on reservation loss, action deletion, or process shutdown;
  this cancels every derived in-flight RPC context.
- Release only after the RPC context has returned or its recorded deadline has
  elapsed. A bounded action must also have no attempt left `IntentRecorded`;
  an irreducibly uncertain attempt first becomes `Ambiguous`. Clear holder
  fields through compare-and-swap and do not delete the Lease as normal
  release. Actions persist `releasedAt` before entering a terminal phase;
  production returns to cadence waiting.

The 20-second gap between maximum RPC duration and Lease duration is a
takeover safety margin. Expiry uses elapsed time since local observation of an
unchanged record rather than comparing clocks written by different hosts.

There is no force-steal path in v1. Administrative recovery removes or edits a
Lease only after the operator is stopped, the record has remained unchanged
for a full Lease duration, and the recorded `rpc-not-after` has elapsed. The
next controller still revalidates chain state before mutation.

### Restart and loss outcomes

| Observed state | Required behavior |
| --- | --- |
| Another token holds an unexpired Lease | Make no RPC; remain `Pending/TargetBusy` if no prior intent exists. |
| Same action, old token, no intent | Wait for safe expiry, acquire a new token, and re-run admission within the original timeout. |
| Same action, old token, persisted intent | Inspect every known tip first; proven absence may resume, any observed or uncertain effect is `Inconclusive`. |
| Different holder after this action produced an effect | Never resume mutation; record attributable state and end `Inconclusive`. |
| Expired, unclaimed holder | Reacquire only after the unchanged-record interval, holder deadline, and complete live identity/chain revalidation. |
| Renewal or leader context lost during RPC | Cancel the client context; after `rpc-not-after`, reconcile every known tip before any further call. |

Bitcoin Core accepts no fencing token or idempotency key. The protocol limits
concurrency and records uncertainty; it does not claim exactly-once execution.
Bounded actions hold the reservation across their complete active lifecycle.
Continuous production holds it for one block RPC and its ambiguity deadline,
never while waiting for cadence.

## Credentials and configuration-input identity

### Fixed Secret contract

Each enrolled network namespace has one administrator-created Secret:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: stacks-bitcoin-rpc
immutable: true
type: Opaque
stringData:
  bitcoin-rpc.conf: |
    rpcuser=<high-entropy-base64url-value>
    rpcpassword=<high-entropy-base64url-value>
    rpcauth=<username>:<salt>$<hmac-sha256>
```

The provisioning helper generates both values with a cryptographically secure
random source: 24 random bytes for the base64url username, 32 for the
base64url password, and 16 for the hexadecimal `rpcauth` salt. It derives the
salted HMAC using Bitcoin Core's `share/rpcauth` contract, writes the immutable
Secret, and prints only the SHA-256 digest of the exact newline-terminated
`bitcoin-rpc.conf` bytes. It never prints or accepts credentials through
command-line arguments, logs, or Helm values.

Cookie authentication is Bitcoin Core's preferred same-host mechanism, but its
credential is generated at daemon start and access is controlled by the
daemon's filesystem. A centralized controller in another Pod would need
shared Bitcoin data-volume access, broad `pods/exec`, or a separately
authenticated in-Pod gateway. V1 therefore uses the supported static
`rpcauth` mechanism for cross-Pod RPC. Actor init renders only the `rpcauth`
verifier into `bitcoin.conf`; it never renders plaintext `rpcuser` or
`rpcpassword`. Controllers use the matching client values for HTTP Basic
authentication. See Bitcoin Core's
[JSON-RPC security guidance](https://github.com/bitcoin/bitcoin/blob/master/doc/JSON-RPC-interface.md#security).

HTTP Basic authentication does not encrypt RPC traffic. The Bitcoin RPC
Service remains cluster-internal and NetworkPolicy restricts ingress to the
declared Stacks actors plus enrolled action and observation controllers. V1
assumes a private cluster network; exposing this Service or crossing an
untrusted network requires a separately reviewed encrypted proxy or same-Pod
helper.

### Required topology changes

The first action-controller slice depends on a topology API update; the
current `bitcoin-regtest/v1` and `nakamoto-regtest-node/v1` profiles still
render development credentials and are not action-eligible.

`StacksNetwork.spec.bitcoinRPCAuth` is an optional `ConfigObjectRef` at the
general topology layer, with this action-compatible form:

```yaml
spec:
  bitcoinRPCAuth:
    name: stacks-bitcoin-rpc
    key: bitcoin-rpc.conf
    expectedDigest: sha256:<64-lowercase-hex>
```

For managed RPC, CEL fixes `name` and `key`, requires `expectedDigest`, and
forbids `mountPath`. The aggregate compiler copies the same field to
`BitcoinNode.spec.bitcoinRPCAuth` and `StacksNode.spec.bitcoinRPCAuth`.
`bitcoin-regtest/v2` and `nakamoto-regtest-node/v2` consume it through actor
init; the `/v1` profiles remain available only for compatibility and do not
qualify for managed action RPC.

The Bitcoin v2 renderer places only `rpcauth` in the effective Bitcoin
configuration. The Stacks v2 renderer places the matching username and
password in an ephemeral rendered TOML file. Custom configuration remains
supported, but its author must opt into the same reserved renderer inputs
before the leaf can report managed-RPC readiness.

This topology prerequisite changes the generated-profile enum, aggregate and
leaf APIs, compiled leaf specification, workload renderer, and action
eligibility status. It requires new vectors in `leaf-spec-v1.json` and
`inventory-v1.json`; a changed leaf `specDigest` carries the credential-input
identity without adding secret material to inventory. The topology update and
credential helper land before `BitcoinBlockGeneration` is enabled.

Actor init verifies the mounted key before using it or rendering Stacks TOML.
The network operator references the object but has no Secret-read RBAC.

Action and observation controllers receive `get` on only that Secret name via
a RoleBinding in every enrolled namespace. Reading credential bytes is an
explicit trust-boundary exception. Action specs cannot choose a Secret,
wallet, RPC profile, or endpoint.

Inventory attests configuration-input identity through the compiled leaf
`specDigest`: fixed reference, expected digest, template digest, and renderer
contract. It does not attest credential contents or final rendered TOML bytes.
Rotation replaces the immutable Secret, updates the topology expected digest,
withdraws admitted identity, and rolls affected actors. Controllers fail
closed until the new inventory is Ready.

## RBAC

The action operator needs namespaced access to:

- get/list/watch `BitcoinBlockProduction` and the two action kinds and update
  only their respective status/finalizers;
- get `StacksNetwork` and `BitcoinNode` resources;
- get Pods, Services, and StatefulSets for uncached identity verification;
- get the exact `stacks-bitcoin-rpc` Secret name;
- get/list/watch/create/update/patch Leases with names owned by this protocol;
  configure the Lease informer with the managed-by label selector and map
  target UIDs to waiting actions through an index; and
- emit namespaced Events.

It does not write topology, actor workloads, Chaos Mesh resources, or action
specs. The agent-facing production role may update `cadence` and `paused`; the
bounded-action role has no update or patch verb. Exact rendered-RBAC allowlist
tests are mandatory. If Kubernetes RBAC cannot constrain Lease names by a
stable static set, admission and code ownership checks must refuse adoption of
a foreign Lease; this limitation is documented rather than represented as
stronger RBAC.

Kubernetes RBAC cannot constrain `list` or `watch` by label. The managed-by
selector bounds the informer cache; namespace-scoped authorization, strict
ownership checks, and refusal to adopt foreign Leases provide the security
boundary.

## Observability

The controllers publish only bounded status, structured logs, and Kubernetes
Events. Passive observability independently records:

- production and action creation, spec generations, phase, conditions, pause,
  and deletion;
- target reservation acquisition, turnover, expiry, and release;
- admitted topology and credential-input identity;
- action RPC intents and receipts plus captured production summaries without
  credentials;
- Bitcoin heights, tips, chainwork, branches, and peer views; and
- gaps or source unavailability.

Observability never grants admission, holds the reservation, invokes RPC
mutations, or turns action completion into a protocol verdict.

## Tests

### Contract and schema

- Pin exact kinds, fields, hard limits, RPC allowlists, credential names, and
  cadence rules through `bitcoin-actions-v1.json` and
  `bitcoin-block-production-v1.json`; pin shared exclusion separately through
  `bitcoin-reservation-v1.json`.
- Generate structural schemas with no preserved unknown fields or OpenAPI
  `format` on duration strings.
- Compile CEL for action immutability, producer target-field immutability,
  producer name/target equality, both cadence unions, positive/bounded
  durations, sequence/count equality, `replacementBlocks > depth`, and all
  three v1 boundary opt-ins.
- Prove forbidden orchestration, endpoint, credential, wallet, and raw-RPC
  fields are absent.

### Unit and envtest

- Exercise every finite cadence branch, count/batch/sequence bounds, persisted
  uniform selection, timeout, deletion, partial progress, identity drift, and
  every ambiguous RPC boundary.
- Exercise producer create, fixed/uniform cadence updates, pause/resume,
  deletion, missed timers without catch-up, target rollout, object-UID
  replacement refusal, status-rate bounds, hash-ring truncation, and ambiguous
  responses.
- Use a fake clock for local observed-record expiry, handle renewal, release,
  stale holder, competing kinds, same action/new token, and leader
  cancellation.
- Prove the leader-gated reservation manager starts and stops with election,
  owns every Lease write, and renews independently of a saturated reconcile
  queue.
- Prove only one contender reaches a mutation RPC under concurrent reconcile.
- Prove the producer's uncached action lookup is non-vacuous, that a waiting
  bounded action prevents producer acquisition, that a malformed or otherwise
  stuck `Pending` action does not stall production, and that an action created
  after the lookup waits for at most the one held production RPC/deadline.
- Prove a managed Lease release maps by target UID and promptly reconciles the
  waiting action while unrelated or foreign Lease events enqueue nothing.
- Prove producer timers use current object UID and cadence generation, ignore
  stale requeues, start a fresh full delay after restart, and require no
  goroutine or per-block status write.
- Prove status intent precedes every fake mutation and crash recovery never
  blindly repeats it.
- Prove a cancelled or lost mutation response cannot reach `Completed`, and
  that `ProvenAbsent` examines every known tip.
- Bound the maximum serialized action status at schema limits before deciding
  whether acknowledged attempts need compaction. Prove producer status cannot
  exceed eight hashes or grow with runtime duration.
- Verify generated CRD validation and immutable-spec updates through envtest.
- Verify `spec.bitcoinRPCAuth`, both compiled leaf fields, v2 profile rollout,
  digest mismatch, and managed-RPC readiness through envtest.
- Verify finalizer and status ownership without requiring the observability
  operator.

### Live qualification

- Use real supported Bitcoin Core images, both v2 profiles, and the fixed
  credential Secret.
- Maintain fixed and uniform-random baseline production, update cadence, pause,
  resume, and verify no catch-up burst after downtime.
- Generate blocks in all four finite cadence modes, including a multi-batch
  immediate request and the explicit `15s`, `20s`, `3s` sequence.
- Run two independent Bitcoin miners concurrently and contend production plus
  both action kinds on one target, proving bounded-action priority and baseline
  resumption.
- Interrupt the leader before, during, and after an RPC deadline.
- Replace a suffix, remove the invalidity marker, and prove the higher-work
  branch from returned hashes and headers.
- Apply a separate native partition to demonstrate composability and observe
  split views without teaching either controller the scenario.
- Exercise invalid credentials, non-regtest nodes, unsupported Bitcoin RPC
  versions, and out-of-band chain activity as negative controls.
- Prove v1 reorganization admission rejects any missing boundary opt-in while
  its protocol schedule is unknown.

## Alternatives

| Alternative | Disposition |
| --- | --- |
| Endless or controller-recreated generation action | Rejected; it breaks the bounded, immutable, terminal action contract. |
| Continuous production inside `StacksNetwork` | Rejected; topology and operational cadence have different mutability, privilege, failure, and controller lifecycles. |
| Kubernetes CronJob for baseline mining | Rejected; it cannot share target exclusion, identity admission, RPC ambiguity, or bounded status consistently. |
| Producer holds the Lease while waiting for cadence | Rejected; it would block independently submitted bounded actions without protecting an active RPC. |
| Per-block producer intent/receipt in CRD status | Rejected; production makes no completion claim and status must remain bounded. |
| Seeded production timing | Rejected; actual selections are observed facts and distributed replay is not a product guarantee. |
| Generic Bitcoin RPC action | Rejected; it defeats schema, RBAC, review, and safety boundaries. |
| In-memory target mutex only | Rejected; it does not survive leader turnover or multiple processes. |
| Tip-height delta as attribution | Rejected; concurrent miners can advance the same observed chain. |
| Destination-based completion after a lost response | Rejected for v1; cross-action uniqueness cannot be established atomically without another coordination domain. |
| Blind retry after transport ambiguity | Rejected; Bitcoin RPC has no idempotency or fencing token. |
| Action-selected credentials or endpoints | Rejected; topology and administrator policy own connectivity. |
| Reorganization as a reversible fault | Rejected; deleting the resource cannot undo propagated history. |

## Deferred decisions

- Publish a trusted protocol schedule that can make boundary opt-ins selective.
- Add administrator policy elevation only after concrete override needs exist.
- Add a Lease ValidatingAdmissionPolicy only if code ownership checks and
  exact RBAC prove insufficient in qualification.
- Compact acknowledged mutation attempts only if the serialized-size guard or
  measured API-server write cost requires it.

## Definition of done

- The three generated CRDs match this document and the action, production,
  reservation, and shared lifecycle contracts.
- The required topology credential field, v2 profiles, and updated leaf and
  inventory vectors land before the action controllers are enabled.
- Each controller is independently registered, permissioned, testable, and
  disableable.
- Hard limits, typed RPC methods, identity checks, action intent, bounded
  production status, yield behavior, and Lease semantics pass unit, envtest,
  race, and negative-control tests.
- Live qualification proves continuous fixed/random production, cadence
  updates, pause/resume, all finite cadence modes, contention, restart
  ambiguity, forced reorganization, and separate partition composition on
  declared images and platforms.
- Status reports exact observed controller facts without claiming complete
  production history, Stacks recovery, global Bitcoin convergence,
  deterministic execution, or exactly-once RPC.
- API, examples, operations, security, compatibility, and release docs are
  current before any kind is published.

## Resolved M0.4 decisions

1. `BitcoinBlockProduction` is mutable desired operation in
   `bitcoin.stacks.org`, not an action or `StacksNetwork` field.
2. Finite generation is one `BitcoinBlockGeneration` with immediate, fixed,
   uniform-random, and explicit-sequence cadence branches.
3. `BitcoinReorganization` is a separate irreversible action.
4. Destinations are explicit immutable regtest addresses; wallet names are
   neither action input nor required by the mechanism.
5. Only hashes returned by a successful mutation response can support action
   completion; lost responses are either `ProvenAbsent` across every known tip
   or `Ambiguous` and `Inconclusive`.
6. Production has no completion claim or per-block intent journal; its status
   is bounded and ambiguity remains explicit.
7. A neutral API-server Lease plus a leader-gated reservation manager
   serializes by admitted Bitcoin leaf UID across production and action kinds;
   production yields to waiting bounded actions.
8. `spec.bitcoinRPCAuth`, the fixed immutable credential Secret, and its
   expected digest define configuration-input identity.
9. The first implementation uses absolute schema bounds; policy elevation and
   cross-kind aggregate budgets remain future work.
10. V1 has no trusted protocol-schedule source and requires every boundary
   opt-in for reorganization admission.

No unresolved decision blocks the initial `BitcoinBlockProduction`,
`BitcoinBlockGeneration`, or `BitcoinReorganization` vertical slices.
