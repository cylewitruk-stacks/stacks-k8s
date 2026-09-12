# Composable network runtime

The `v1alpha2` runtime implements the
[composable public API](../design/public-api/README.md). Install the
[network chart](../../charts/stacks-network-operator/README.md) independently
of individual networks. See [installation](operations.md) and
[compatibility](migration.md) before applying resources. The
[qualification record](public-api-qualification.md) states tested profiles and limits.

## Composition and identities

A canonical `StacksNetwork/network` selects reusable definitions or inline participants.
Compilation applies network defaults, definition values and typed entry overrides, then
fills absent profile values. Lists replace inherited lists. Each generated participant
belongs to the network; independently owned definitions and accounts survive its deletion.
Participant names are single-use within a network. Removing an entry destroys that
instance; deleting an instance directly does not authorize its recreation.

Accounts resolve independently. Scoped resolver Jobs generate or inspect keys and publish
public identities. The operator reads Secret metadata, never private key values. A replaced
public report gets a new inspection Job bound to the same immutable credential UID.
Shared signing accounts are legal experiment inputs; the operator does not lease their
nonces. Required baseline signing roles need initial funds. A faucet allocation of `"0"`
permits later funding without contributing a genesis balance.

The aggregate is the sole participant-admission writer. Domain controllers consume the
complete admitted policy and manage workloads. Bound workers publish execution facts.
These writers use separate status field managers and condition types. Candidate errors
block the affected participants and dependent inputs, while unrelated admissions, controls
and removals continue. Existing signer attachments take precedence over new contenders.

## Genesis and initialization

Successful creation of `StacksGenesis` freezes chain allocations, epoch/PoX settings,
contract sources and initial cohort requirements. The immutable artifact is bounded to
900 KiB. Its chain digest excludes provenance and bootstrap requirements; `inputDigest`
uses logical names, compiled configuration and public fingerprints without runtime UIDs.
Exact identities remain in provenance and admission bindings.

Initialization is continuously reconciled. Bitcoin production observes the frozen gates;
stackers enroll and renew, contract workers deploy and verify the pinned sBTC bundle, and
transaction production supplies demand. The [protocol timing contract](../design/public-api/protocol-timing.md)
defines release evidence. Crossing a Bitcoin height or observing Pod readiness alone does
not satisfy a gate. The final release also requires fresh canonical traffic after the
waterfall boundary before `Initialized=True`.

Before the first Stacks block, `AwaitingFirstAnchor` records a fresh, empty native
chain view. A captured miner may request bounded Bitcoin confirmation progress
without claiming PoX readiness. Any captured node's established-chain observation
withdraws that allowance; legacy enrollment transactions then supply the demand.
Clarity-3 contract deployment additionally requires a native canonical Nakamoto
header, since Bitcoin can cross epoch 3.0 while the Stacks tip remains epoch 2.5.

Bootstrap requirements capture values needed after a controller restart, including initial
mining-enabled roles. Compatible image, resource and seed changes may proceed during
bootstrap. Conflicting supported policies retain the previous whole admission and report
`PolicyDeferred=True/BootstrapPending` until their last affected gate completes:

| Captured requirement | Last affected gate |
| --- | --- |
| Bitcoin wallet attachment and preparation | `PrepareBitcoin` |
| sBTC registry initialization | `PreparePoX5` |
| Stacker amount, coverage, renewal and initial mining role | `PrepareWaterfall` |

Protected identity changes report `RequiresReplacement` instead. Lifecycle controls remain
independent of either rejection. A retained policy must still have its exact current
public dependencies and credential UIDs. Missing dependencies withhold authorization;
retaining an admission for evidence does not authorize stale inputs. Publication recovery
uses the originally captured policy, never a newly edited cohort or a second genesis.

## Workloads and controls

Actor controllers provision participant-owned StatefulSets, Services and immutable
configuration through scoped resolver Jobs. Stacks configuration uses structured TOML.
Generated Stacks P2P and RPC advertisements use their allocated Service IPs; worker
and user endpoints retain Service DNS names. Both advertised Service UIDs are bound
into configuration resolution.
Bitcoin configuration uses native scalar/repeated-option overrides and one network
section level, or a complete immutable `bitcoin.conf` Secret. The operator reads only
Secret metadata; the resolver checks the captured UID, native syntax and required
managed settings. Bitcoin `ConfigVerified` attests that agreement, not Core option
semantics or startup compatibility. Unknown options and numeric/interaction errors
remain Core startup/readiness concerns. See [Bitcoin configuration](../design/public-api/resources.md#bitcoin-configuration)
for the mapping, protected options and Unverified exclusions.
Consensus signers receive their signing key and the node-owned event authentication
material; stacker administration runs in a separate worker. Event credentials survive
ordinary Pod rolls and are replaced only with their owning participant.

Bitcoin control runs separately from peer/protocol traffic. It owns scoped mutation
credentials and durable dispatch records. RPC ambiguity closes the affected target;
controller deadlines do not prove that Core stopped executing a request. Pausing blocks
new managed sends while read-only observations and outstanding receipt collection continue.

A reorganization refuses confirmed external tip movement with `ExternalChainMovement`.
Its reservation excludes mutations on its target only; experiments needing a stable
captured tip must control other producers. Transient preflight read failures remain
retryable within the action deadline.

Stacks management roles run as participant-owned Pods with `restartPolicy: Never`.
The aggregate records the exact Pod UID before activation. A surviving worker retains its
nonce and pending submission in memory across watch reconnects and operator restarts.
Inclusion receipts are retained independently of canonical rechecks. Traffic evidence
is omitted until its first successful canonical read; failed later reads preserve
the original observation time and mark availability false.
Loss of that bound worker fails the experiment; no replacement process reconstructs or
replays its pending mutations. Cooperative pause preserves the process and permits
observation of submissions already sent.

During a transient Kubernetes API outage, a surviving baseline worker may continue only
its previously acknowledged policy and cached public inputs. Native protocol checks still
apply. New faucet dispatch, policy changes and control acknowledgements require current
API reads. Retained outcomes are published before further work after API recovery.
A cached direct Pod IP avoids following Service changes, but IP reuse during an outage is
not a process-identity fence.

## Network status

| Phase | Meaning |
| --- | --- |
| `Resolving` / `ResolutionError` | Inputs are being resolved or require correction |
| `Uninitialized` | Resolution may be complete, but no running network is claimed |
| `Initializing` | Frozen protocol requirements are still converging |
| `Running` | Initialization completed and running operation is requested |
| `Pausing` / `Paused` | Cooperative pause is requested / acknowledged by current workers |
| `Stopping` / `Stopped` | Terminal shutdown is requested / confirmed |
| `Failed` | A terminal identity, worker or initialization failure is latched |
| `Destroying` | Network deletion and owned-runtime disposal are in progress |

`Resolved`, `Initialized`, `Running` and `Operational` answer different questions.
Initialization is historical and never cleared by a temporary read failure or later fault.
Operational requires fresh, correctly identified protocol observations and recent Bitcoin
and Stacks progress. A required paused baseline makes it False; unavailable evidence makes
it Unknown unless another known requirement is already false. Optional faucets, extra
actors and telemetry do not automatically gate global health.

The release observation policy uses 2 s polls, a 10 s RPC bound, 5 s heartbeats and 16 s
freshness. Progress windows are `max(120 s, 3 × cadence) + 10 s`, using the upper cadence
bound when timing varies. Status heartbeats do not refresh the original receipt or
inclusion time. Readiness and fault cleanup remain separate from protocol recovery.

Worker watches ignore execution heartbeats and native observation-only updates;
protocol polling remains timer-driven. Controls, admission and process identity
changes still trigger immediate reconciliation. Generated Stacks nodes use a
500 ms idle miner initiative threshold for the accelerated regtest profile;
Core's event loop does not guarantee a wakeup every 500 ms.

Shared genesis and Bitcoin records remain finalized until all participant consumers
finish disposal. Bitcoin control shutdown uses retained drain acknowledgement and
the existing Deployment, without requiring configuration to be rendered again.
During root destruction, confirmed control-Pod termination and the absence of
remaining workload controllers also permit disposal. This does not turn unresolved
RPCs into successful or drained executions. Missing process evidence stays unknown.

Actor shutdown persists exact-Pod termination before releasing the Pod that supplies
that evidence. Stopped networks do not resume. Participant removal remains destructive while stopped.
Actor Pods keep a read-only root filesystem and mount a writable `emptyDir` at
`/tmp` for native database scratch files. That directory is discarded with the Pod.
Storage survival follows the configured PVC retention policy; logs, memory and ephemeral
files are not automatically preserved. Collect desired evidence before removal.

## Verification boundary

Unit tests exercise policy, identity, dispatch, protocol encoding and state transitions.
Envtest exercises admission, watches, status ownership, scoped permissions and API lifecycle;
it has no kubelet or garbage collector and cannot establish native protocol behavior.
The pure-Go protocol library uses pinned Stacks.js vectors as a test oracle. New runtime
images contain no JavaScript.

The earlier [foundation review ledger](public-api-foundation-review.md) covers the committed
schema/resolution slice only. Runtime, storage, protocol and fault claims require evidence
from the replacement installation; earlier-runtime qualification does not establish them.

## Rejections and observation cost

A matching native transaction-rejection envelope settles an attempted send only
for `FeeTooLow`, `BadNonce`, `ConflictingNonceInMempool` or `NotEnoughFunds`.
The worker retains the TxID and rejection count, clears its pending slot and leaves
its next nonce unchanged. The send occurred: this is not `noSend` evidence.
Traffic and protocol roles can offer a fresh, authorized attempt after at least
5 seconds after the refusal returns; traffic also respects its configured cadence.
A faucet request ends `Rejected` and is never sent again. Unknown reasons, server failures, mismatched
TxIDs and transport failures retain uncertainty. Shared-key use can cause repeated
refusals or `NonceMismatch`; workers do not silently resynchronize nonce ownership.

Admission checks memoize dependency identities within one reconciliation only;
freeze uses a separate fresh pass. Runtime health summaries use cached observations
and coalesce status notifications to the 2-second observation interval. Timers still
evaluate expiry and recovery without new events. Durable gate transitions and
failures, exact lifecycle changes, selected Bitcoin targets and dispatch authorization
retain direct API checks. Heartbeat
filtering does not extend the observation freshness window.

Late actor creation and workload reconciliation are supported. Native catch-up and
repeat reliability remain [unqualified](public-api-qualification.md): missing-anchor
stalls have occurred in both late actors and an initial cohort. Successful runs do
not establish that every repetition or join at an arbitrary height will succeed.
