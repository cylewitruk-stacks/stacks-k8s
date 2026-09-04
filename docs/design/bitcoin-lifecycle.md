# Bitcoin lifecycle design

## Status and scope

This document is the normative M0.3 design for the first custom action
controllers. No Bitcoin action CRD or controller is implemented yet.

`StacksNetwork` owns Bitcoin topology. The resources here perform one bounded
regtest action against one admitted `BitcoinNode`. An external agent creates,
sequences, overlaps, observes, and interprets them. Natural mining and
reorganizations remain observed network behavior; a controller-forced
reorganization is an action, not desired topology.

Both kinds follow the [atomic action contract](actions.md). The versioned
mechanism contract is
[`bitcoin-actions-v1.json`](../../contracts/bitcoin-actions-v1.json).
Its identifier is `actions.stacks.org/bitcoin-actions/v1alpha1`.

## Resource inventory

| Resource | Purpose |
| --- | --- |
| `BitcoinBlockGeneration` | Generate an immutable, finite number of blocks immediately or at a bounded cadence. |
| `BitcoinReorganization` | Replace one finite regtest suffix with a longer, higher-work branch. |

Working API group and version: `actions.stacks.org/v1alpha1`.

## Shared controller boundary

One action-operator binary may register both controllers. Each kind has its
own API type, controller registration, mechanism package, typed RPC interface,
RBAC tests, examples, and reference page. Shared packages may provide:

- action lifecycle and condition helpers;
- admitted network and leaf identity resolution;
- the target reservation protocol;
- bounded Bitcoin RPC transport; and
- chain, RPC-attempt, and attribution status types.

There is no generic RPC action, untyped mechanism registry, scenario
controller, or ordered execution plan. The network and observability operators
are not runtime dependencies of mutation beyond their published Kubernetes
APIs.

## Common admission

While the action remains `Pending`, either controller must:

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

### Example

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
  interval: 1m
  batchSize: 1
  destinationAddress: "<unique-regtest-address>"
  timeout: 3h
```

For an immediate bounded burst, omit `interval` and choose `batchSize` within
the hard limit:

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
  batchSize: 10
  destinationAddress: "<another-unique-regtest-address>"
  timeout: 2m
```

### Spec

| Field | Go representation | Contract |
| --- | --- | --- |
| `networkRef` | shared local reference | Exact same-namespace `StacksNetwork`. |
| `bitcoinNodeRef` | shared local reference | Exact compiled `BitcoinNode` resource name. |
| `blocks` | `int32` | Required, `1..288`. |
| `interval` | `*metav1.Duration` | Optional inter-block delay, `100ms..5m`; OpenAPI `format` remains unset. |
| `batchSize` | `int32` | Required, `1..16`, no larger than `blocks`; must equal `1` when `interval` is present. |
| `destinationAddress` | `string` | Required bounded regtest address, length `14..90`; runtime-validated by Bitcoin Core. |
| `timeout` | `metav1.Duration` | Required creation-relative bound, positive and no greater than `24h`. |

The entire spec is immutable from creation with `self == oldSelf`. All limits
are absolute until a separately reviewed administrator safety policy exists.
The API does not expose wallet names, RPC endpoints, credentials, raw methods,
or `maxtries`.

Omitting `interval` means the controller may request up to `batchSize` blocks
per RPC. Supplying `interval` means one block per RPC and the controller waits
the interval between acknowledged blocks. The action may expire with partial
progress; timeout does not imply that the requested cadence fits completely.

### Mechanism status

In addition to common action status, the kind owns:

| Field | Contract |
| --- | --- |
| `requestedBlocks` | Immutable admitted count. |
| `generatedBlocks` | Number of hashes attributed to this action. |
| `generatedBlockHashes` | Ordered, unique hashes, maximum 288. |
| `startingChain` | Height, best-block hash, and chainwork observed at admission. |
| `observedChain` | Latest height, best-block hash, and chainwork verified by the controller. |
| `reservation` | Retained Lease identity, deadlines, and proven release time. |
| `rpcAttempts` | Ordered bounded intent/receipt records, maximum 320. |
| `attribution` | `RPCResponse` or `Uncertain`. |

An RPC attempt records sequence, method, expected height and tip, requested
count, destination, acquisition token, start time, deadline, outcome, and any
returned or candidate hashes. The Pending intent is persisted before the RPC
starts. Status never contains credentials or raw RPC payloads.

### Reconciliation

The controller performs these level-based steps:

1. establish admission and absolute expiry;
2. acquire the target reservation;
3. re-read reservation and target identity;
4. append and persist one bounded RPC intent;
5. invoke `generatetoaddress` with the admitted destination and batch count;
6. verify returned hashes, ancestry, height, and increasing chainwork;
7. persist the receipt and cumulative progress; and
8. requeue for the interval or next immediate batch.

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
old RPC call or serialize separate action kinds. Both controllers therefore
use one `coordination.k8s.io/v1` Lease per admitted Bitcoin leaf UID.

### Identity and constants

| Element | Frozen value |
| --- | --- |
| Namespace | Action and target namespace. |
| Lease name | `bitcoin-action-<first-32-hex-sha256(namespace-NUL-targetUID)>`. |
| Holder identity | `<action-uid>/<32-lowercase-hex-acquisition-token>`. |
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
| Annotations | Action kind/UID, target UID, acquisition token, and `rpc-not-after`. |

The keyed in-process mutex is only a contention optimization. Correctness
comes from optimistic API-server updates and uncached Lease reads. The mutex
may be held across a bounded RPC batch because it is keyed by target UID.
The action operator is the sole Lease writer. It refuses a pre-existing Lease
whose name, managed-by label, target owner UID, or target annotation differs;
it never adopts or deletes a foreign object.

### Reservation manager

A shared `ReservationManager` owns active handles for both Bitcoin action
controllers. It is registered with controller-runtime through `manager.Add`
as a leader-gated Runnable whose `NeedLeaderElection` result is `true`.
Manager-context cancellation on leader loss closes every handle and cancels
all RPC contexts derived from those handles.

The manager renews due handles independently of controller workqueues. A
reconciler's `RequeueAfter` controls action progress only; worker count and
queue latency are not reservation-safety properties. Each successful
acquisition returns one handle bound to Lease name, holder identity, action
UID, target UID, and acquisition token.

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
- Treat `actions.stacks.org/rpc-not-after` as a recorded holder deadline that
  may extend caution but can never permit acquisition earlier than the local
  unchanged-record interval.
- Persist holder identity in action status before any RPC intent.
- Register the acquired handle before entering `Admitted`; failure to register
  releases the Lease or leaves it to expire without issuing RPC.
- Renew through the handle every 10 seconds while the action may issue calls.
- Immediately before each mutation RPC, perform an uncached Lease read and
  require the exact holder and unexpired times.
- For each mutation, persist the RPC intent, update `rpc-not-after` to that
  intent's deadline through holder-checked compare-and-swap, re-read the Lease,
  and only then invoke the typed client.
- Close the handle on reservation loss, action deletion, or process shutdown;
  this cancels every derived in-flight RPC context.
- Release only after the RPC context has returned and no attempt remains
  `IntentRecorded`; an irreducibly uncertain attempt first becomes
  `Ambiguous`. Clear holder fields through compare-and-swap, persist
  `releasedAt`, and only then enter a terminal phase; do not delete the Lease
  as normal release.

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

- get/list/watch the two action kinds and update their status/finalizers;
- get `StacksNetwork` and `BitcoinNode` resources;
- get Pods, Services, and StatefulSets for uncached identity verification;
- get the exact `stacks-bitcoin-rpc` Secret name;
- get/create/update/patch Leases with names owned by this protocol; and
- emit namespaced Events.

It does not write topology, actor workloads, Chaos Mesh resources, or other
action specs. Exact rendered-RBAC allowlist tests are mandatory. If Kubernetes
RBAC cannot constrain Lease names by a stable static set, admission and code
ownership checks must refuse adoption of a foreign Lease; this limitation is
documented rather than represented as stronger RBAC.

## Observability

The action controller publishes only bounded mechanism facts and Kubernetes
Events. Passive observability independently records:

- action creation, spec identity, phase, conditions, and deletion;
- target reservation acquisition, turnover, expiry, and release;
- admitted topology and credential-input identity;
- RPC intents and receipts without credentials;
- Bitcoin heights, tips, chainwork, branches, and peer views; and
- gaps or source unavailability.

Observability never grants admission, holds the reservation, invokes RPC
mutations, or turns action completion into a protocol verdict.

## Tests

### Contract and schema

- Pin exact kinds, fields, hard limits, RPC allowlists, credential names, and
  reservation constants through `bitcoin-actions-v1.json`.
- Generate structural schemas with no preserved unknown fields or OpenAPI
  `format` on duration strings.
- Compile CEL for whole-spec immutability, positive/bounded durations,
  `batchSize == 1` with an interval, `replacementBlocks > depth`, and all
  three v1 boundary opt-ins.
- Prove forbidden orchestration, endpoint, credential, wallet, and raw-RPC
  fields are absent.

### Unit and envtest

- Exercise admission, count/cadence/batch bounds, timeout, deletion, partial
  progress, identity drift, and every ambiguous RPC boundary.
- Use a fake clock for local observed-record expiry, handle renewal, release,
  stale holder, competing kinds, same action/new token, and leader
  cancellation.
- Prove the leader-gated reservation manager starts and stops with election,
  owns every Lease write, and renews independently of a saturated reconcile
  queue.
- Prove only one contender reaches a mutation RPC under concurrent reconcile.
- Prove status intent precedes every fake mutation and crash recovery never
  blindly repeats it.
- Prove a cancelled or lost mutation response cannot reach `Completed`, and
  that `ProvenAbsent` examines every known tip.
- Bound the maximum serialized action status at schema limits before deciding
  whether acknowledged attempts need compaction.
- Verify generated CRD validation and immutable-spec updates through envtest.
- Verify `spec.bitcoinRPCAuth`, both compiled leaf fields, v2 profile rollout,
  digest mismatch, and managed-RPC readiness through envtest.
- Verify finalizer and status ownership without requiring the observability
  operator.

### Live qualification

- Use real supported Bitcoin Core images, both v2 profiles, and the fixed
  credential Secret.
- Generate immediate and cadenced blocks, including a multi-batch request.
- Run two independent Bitcoin miners concurrently and contend both kinds on
  one target.
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
| Separate mining-window and block-request kinds | Rejected; count, interval, and batch size are one mechanism. |
| Mining policy inside `StacksNetwork` | Rejected; bounded side effects are not desired topology. |
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

- The two generated CRDs match this document and both versioned contracts.
- The required topology credential field, v2 profiles, and updated leaf and
  inventory vectors land before the action controllers are enabled.
- Each controller is independently registered, permissioned, testable, and
  disableable.
- Hard limits, typed RPC methods, identity checks, persisted intent, and Lease
  semantics pass unit, envtest, race, and negative-control tests.
- Live qualification proves immediate/cadenced generation, contention,
  restart ambiguity, forced reorganization, and separate partition
  composition on declared images and platforms.
- Status reports exact observed action facts without claiming Stacks recovery,
  global Bitcoin convergence, deterministic execution, or exactly-once RPC.
- API, examples, operations, security, compatibility, and release docs are
  current before either kind is published.

## Resolved M0.3 decisions

1. Generation is one `BitcoinBlockGeneration` kind, not two overlapping kinds.
2. `BitcoinReorganization` is a separate irreversible action.
3. Destinations are explicit immutable regtest addresses; wallet names are
   neither action input nor required by the mechanism.
4. Only hashes returned by a successful mutation response can support
   completion; lost responses are either `ProvenAbsent` across every known tip
   or `Ambiguous` and `Inconclusive`.
5. API-server Leases plus a leader-gated reservation manager serialize by
   admitted Bitcoin leaf UID across all Bitcoin action kinds.
6. `spec.bitcoinRPCAuth`, the fixed immutable credential Secret, and its
   expected digest define configuration-input identity.
7. The first implementation uses absolute schema bounds; policy elevation and
   cross-kind aggregate budgets remain future work.
8. V1 has no trusted protocol-schedule source and requires every boundary
   opt-in for reorganization admission.

No unresolved decision blocks the `BitcoinBlockGeneration` vertical slice.
