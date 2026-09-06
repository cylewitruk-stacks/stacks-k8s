# R1/R2 decision proposal

## Status and recommendation

**Decision record with a constrained initial implementation.** This
proposal supplies the R1/R2 decision record for M0.3/M0.4. The parent
[M0 plan](m0-remediation-plan.md) remains authoritative; production fields,
credential profiles, and broader implementation qualification still need review.
The [implemented Bitcoin baseline](../network-operator/bitcoin-production.md)
defines the served catalog and single-target production subset. The richer
reservation/action/reset model below remains a proposal for later capabilities.

Recommend a health-independent compiled-declaration catalog for R1 and a
durable, single-dispatch execution record for R2. Apply the
[initial delivery scope](m0-remediation-plan.md#initial-delivery-scope): implement
R1 publication first, then the smallest baseline-production profile.
Explicit reset/readmission, credential rotation, cryptographic process
attestation, and execution-aware adapters are deferred.
Retain the aggregate/leaf controller structure. No in-cluster planner,
autonomous local miner, or generic RPC dispatcher is introduced.

The deliberate availability tradeoff is that one unresolved request can block
managed mutations on its target indefinitely. Other valid targets may continue
under the declared selection policy, without redistributing the blocked
target's weight. Automatic recovery after ambiguity is not a v1 promise.
Evaluate this cost against node-local IO/stress faults, Bitcoin restarts, and
producer-controller restarts during vertical-slice use. The qualified protocol-fault profile preserves
the management path; that does not prevent these other causes of response loss.

The initial profile trusts test-Pod services and excludes compromise of their
credentials. Static per-environment credentials are acceptable. Keep target
checks and observation/mutation separation, but do not require defenses against
credential theft. Unresolved targets stay closed; preserve evidence and use a
fresh independently isolated environment. Reusing resource names alone does
not isolate a replacement environment from old callers.
For the initial profile, use a new namespace, new network/resource UIDs, and
fresh RPC credentials generated once for that environment. Requests use
namespace-qualified endpoints and never retarget old Armed work. A namespace
alone cannot fence an already-resolved IP or connection; distinct credentials
make a new server reject an old request even if an address is reused. Do not
reuse old credentials, data volumes, or mutable configuration references.
This is environment provisioning, not ongoing credential rotation.

Static credentials remain separate by principal. Actor clients receive only
their own method-restricted credentials; producer/action mutation passwords
are never mounted into actor workloads. Bitcoin servers receive the required
salted authentication verifiers, not those client passwords. See the
[RPC permission profile](bitcoin-lifecycle.md#credential-and-renderer-gate).

## Evidence

The [reproducible checks](../../tools/spikes/r1-r2/README.md) establish:

- Leaf identity already exists independently of complete aggregate inventory.
  An unrelated Stacks image change leaves the compiled Bitcoin spec digest
  unchanged; changing the Bitcoin image changes that digest.
- Isolated Bitcoin Core 25.2 and 31.1 nodes accepted a queued one-block request.
  The client timed out while height was zero; the request subsequently advanced
  height to one. The node had zero peers.
- A small transition model separates reservation ownership, process identity,
  dispatch, receipt accounting, cleanup, and release. It exercises selected
  stale-state interleavings, not real concurrent or durable execution.

The model is not a controller, API-server, or distributed-system proof. The
Bitcoin check covers two images and one queued-request case, not every RPC,
version, platform, or failure mode.

## R1: current declaration without global readiness

The current leaf status proves convergence to the leaf's own spec. Ownership
alone does not prove that spec still matches the latest parent declaration.
Moving the aggregate compiler into action consumers would couple runtimes and
duplicate authority.

Recommend an additive, bounded StacksNetwork.status.targetDeclarations
catalog. The aggregate publishes it immediately after successful compilation,
before calling synchronize or observe. Proposed entries contain leaf
kind/name, logical actor name, effective suspended state, and compiled leaf
spec digest. The catalog separately records parent UID/generation and its
contract version. Maximum size follows the existing 232-actor topology bound.
Exact Go types, schema, and digest vectors belong to the API implementation
proposal; no existing inventory-v1 bytes change here.

Only the aggregate controller writes this catalog. Publish with a resource-version
precondition, such as MergeFromWithOptimisticLock, and refresh the patch base
after success. Current updateStatus uses plain MergeFrom and needs an explicit
change. All subsequent status paths must preserve the published catalog for
the same parent UID/generation, including unrelated synchronization failures,
degraded observations, and retiring actors. Patch only owned fields or carry
the catalog forward with conflict protection; fresh status structs must not
erase it. Compilation failure invalidates it for that generation. Never relabel
an older catalog with a newer generation. Preservation establishes declaration
freshness, not continuing admission of any target.

The actors, inventoryReady, and inventoryDigest status fields retain their
existing complete-snapshot semantics. Old consumers continue using them.

Before admission and each newly authorized mutation, a consumer:

1. Directly reads the parent and a catalog for its current UID/generation;
   rejects deletion, suspension, absent/duplicate targets, or stale publication.
2. Directly reads the selected leaf; verifies namespace, network reference,
   controller owner UID, current leaf generation/status, and live spec digest
   against the catalog and leaf identity.
3. Verifies the selected Service, StatefulSet, Pod, revision, configuration
   identity, image identity, and running actor instance through direct reads.
   Other actors' readiness is irrelevant.
4. Verifies the intended RPC endpoint and regtest capability under the trusted
   environment profile; does not claim cryptographic process authentication.
5. Rechecks the parent/catalog and target before granting dispatch authority.
   A concurrent change causes fresh validation, not silent retargeting.

The parent generation establishes freshness, not a global execution lock.
An unrelated parent edit may briefly require catalog republication; unchanged
target inputs and runtime identity permit revalidation without actor rollout.
Target changes require fresh admission. Already-authorized RPC effects may
outlive a topology or policy edit; Kubernetes and Bitcoin have no shared
transaction.

The several uncached reads per dispatch, plus final rechecks, are a deliberate
regtest cost. Measure API load at supported cadences before optimizing them;
cached snapshots must not silently weaken the admission contract.

### Endpoint binding and R3 dependency

A Service name, Pod IP, or successful RPC read is not authenticated Pod
identity. IP reuse, Service retargeting, and container restart must be covered.
The current identity record lacks a running-container instance identifier.
Retain the existing Pod UID and add container name and containerID, with
restartCount as supporting evidence. A restart counter alone is not unique.
Require bitcoind to be the container's lifetime-defining process; an image that
respawns it inside the same container needs separate process enrollment.

For initial production, qualify ownership, routing, and runtime checks under
the trusted Kubernetes/network boundary. Bind Armed work to its selected
endpoint and request; do not retarget or retry it after uncertainty. Replacement
does not clear outstanding work. Static credentials do not fence a stale caller,
so this profile makes no promise that an old request cannot affect a replacement
process; exclusion prevents authorizing competing managed work.
Record the admitted identity separately from evidence about which process
served the RPC. A response alone does not prove process identity across a
replacement race. If that identity is uncertain, preserve the uncertainty
and exclusion rather than attributing the result to the replacement.

The stronger reset profile below requires authenticated process enrollment
and server-side fencing of old credentials. Those mechanisms are deferred,
not a dependency of catalog publication. Public mutation APIs still need their
own reviewed endpoint/admission contract before enablement. Arbitrary topology
authority and cluster/workload permissions remain separately addressed by R6.

### Candidate credential epoch

**Deferred reset-profile investigation; not an initial production requirement.**

[Bitcoin Core 31.1 cookie generation](https://github.com/bitcoin/bitcoin/blob/v31.1/src/rpc/request.cpp)
uses fresh random material.
[RPC startup](https://github.com/bitcoin/bitcoin/blob/v31.1/src/httprpc.cpp)
generates it when cookie authentication is enabled; static rpcauth entries
can coexist. Cookie rotation is therefore a candidate process fence, not
an unconditional property of every Bitcoin configuration.

Bind each Armed request to immutable endpoint and credential epochs before
dispatch. It must never reload a replaced cookie/Secret, refresh authentication
after rejection, or retarget to the new instance. Enrollment must associate
the credential with the actual process, without exposing its bytes in status.
The replacement must reject every previous managed mutation credential;
retained static rpcauth cannot provide that fence.

Cookie authentication alone neither authenticates the server to the client
nor separates observation from mutation authority. R3 must qualify protected
provisioning, endpoint authentication, distinct restricted principals, and
server-enforced method permissions. Do not distribute one shared cookie to
observers and producers. Keep immutable inputs and salted rendering for
rpcauth profiles; any mutation profile used for reset must rotate per instance.

## R2: one durable dispatch authority

Use one protected, target-scoped execution record shared by every managed
Bitcoin mutation kind. Persist reservation owner UID, executor epoch, fresh
process-start nonce, immutable request identity/bounds, admitted target and
credential profile, and execution state in the same optimistically updated object.
Per-process server credential epochs apply only to a later reset profile;
executor epochs still distinguish controller attempts in the initial profile.
A Lease plus a separately updated intent is not an atomic dispatch decision.

This record is controller infrastructure, not an agent-created action or a
history journal. Its storage shape and exact permissions must be frozen with
the revised Bitcoin schema. It retains at most one outstanding bounded RPC
and its bounded receipt; action status retains necessary result attribution.
Reservation ownership and individual RPC authority are distinct. A bounded
action keeps its reservation across dependent mutations and cleanup. An
eligible executor replacement serves that same reservation, has current
admission, and uses CAS with a fresh process nonce while no RPC or unaccounted
receipt is outstanding. A different action or baseline producer must wait
for explicit release; Lease expiry does not change the reservation owner.

| State | Allowed transition |
| --- | --- |
| Unreserved | An admitted eligible owner may reserve by CAS. |
| Reserved, no outstanding RPC or unaccounted receipt | The same reservation may appoint an eligible executor with a fresh epoch by CAS. Other owners remain excluded. |
| Armed, possible dispatch | The exact acquiring process may attempt the request once. No replacement may resend or supersede it. |
| Terminal receipt persisted | Durably account for the result once; retain the reservation. |
| Receipt accounted, reservation retained | Authorize the reservation's next admitted RPC, or explicitly release after cleanup obligations are satisfied. |
| Outcome or executor state unknown | Retain exclusion; observe and report uncertainty. Elapsed time does not clear it. |

Armed is written before sending. Only the process that received confirmation
of its successful CAS may send; a controller reconstructing state never treats
an existing Armed record as permission to resend. Pod name or leader identity
must not substitute for a process-start nonce. If the CAS response is lost,
the still-live process may confirm its exact record by a direct read only
while retaining local knowledge that no send began. Any dispatch must also
meet current admission/deadline rules. An exact CAS may instead withdraw this
proven-unsent authorization while retaining the reservation. An absent read
does not prove the original write cannot still commit. Restart or lost local
knowledge leaves the record blocked; the nonce alone cannot establish whether
a send began.

Prohibit redirects, authentication refresh, and retries after possible delivery.
Specify and test the supported HTTP transport, including any proxy behavior.
[Go's transport](https://go.dev/src/net/http/transport.go) can retry a replayable
body when zero bytes were written on a reused connection. That is distinct
from replay after ambiguous delivery. Fresh HTTP/1 connections are a possible
conservative profile, not fencing; receipt and epoch rules still apply.

A caller paused before Armed loses its ability to arm when another caller
changes the epoch. A caller paused after Armed may still send later, which is
why Armed cannot expire into another dispatch. Leader election still reduces
contention; it is not the source of execution fencing.

The original caller may persist a late terminal response for the exact
request/instance even after leadership loss, while its response path survives.
This is receipt recording, not authority for another RPC. A lost status write
does not authorize resending.
A server error can prove that the handler returned without proving zero
effects; classification and cleanup remain method-specific.

Receipt-to-status accounting must be idempotent by execution identity.
Do not overwrite the receipt before durable accounting or reviewed orphan
handling; a restart must neither lose acknowledged progress nor count it twice.

### Deadlines and receipt collection

An action deadline prevents new dispatch authorization; it cannot recall an
already-Armed request. A transport deadline/cancellation that closes the
connection abandons its response path. No late response can subsequently be
collected through that closed connection. Once delivery is possible, this
leaves the target blocked; in-place recovery is deferred. Ordinary slowness
can trigger this outcome even if the server eventually finishes successfully.

Receipt collection has a separate lifetime from action waiting and reconcile
calls. A profile may keep one receiver per outstanding target alive without a
read deadline while the connection survives. This needs bounded worker counts,
shutdown handling, and status visibility; it does not solve connection loss or
controller restart. The implemented single-target baseline selects this model:
32 process-wide slots include retained receipts, accounting retries preserve
the original receipt time, and SIGTERM allows a 25-second drain within the
manager's 30 seconds and Pod's 45 seconds. These limits do not qualify a future
bounded-action profile. This direct-RPC profile does not promise that
irreversible effects cease at a wall-clock deadline.

Supported controller recovery initially means graceful drain: on SIGTERM,
stop new dispatch authorization, retain receipt collectors, and persist
receipts/accounting within terminationGracePeriodSeconds before exiting.
Do not cancel collectors with the ordinary reconcile shutdown context. Drain
includes local dispatch workers; a proven-unsent Armed request must be safely
withdrawn or remain unresolved. A clean drain leaves no outstanding Armed work
and preserves any action reservation for its next executor. Crashes, lost
responses, API-write failures, and exhausted shutdown grace can still strand
a target; an ordinary rollout is not guaranteed to drain successfully.

Deletion, an unchanged tip, Lease expiry, and an Inconclusive phase likewise
do not prove quiescence or release a reservation.

### Recovery, record loss, and replacement

**Initial profile: no in-place reset after ambiguous execution.** Retain the
record and target exclusion across controller/Bitcoin restarts and Pod
replacement. Preserve evidence and use a fresh independently isolated network.
Do not treat object recreation, record deletion, or a restarted server as
permission to retry. The contract below is retained as deferred design input;
its qualification does not gate initial production.

Without a terminal receipt, automatic recovery stays blocked. Reopening a
target requires evidence that the old caller can no longer cause accepted
execution and previous server work has ended, followed by fresh admission. Credential
revocation alone does not drain an already-authorized server request; Pod
deletion in the API alone does not prove a partitioned process has stopped.
No time-based administrator override is presented as safe recovery.

If operational experience justifies in-place recovery, investigate this
candidate with R3:

1. Record reset intent against the exact unresolved execution, reservation,
   old instance, and credential epoch; keep the target closed. Preserve action
   evidence and identify R4 cleanup obligations before changing the experiment.
2. Establish actual termination of the old Bitcoin process and rejection of
   its mutation credentials by any replacement. A reachable, trusted
   kubelet/runtime providing current termination evidence for the exact old container, correlated
   with a new containerID and process enrollment, is the proposed evidence
   boundary. Retain that evidence before status/history disappears.
3. Admit the replacement through R1/R3 with a fresh credential epoch. Prevent
   stale callers from obtaining replacement credentials for old Armed work.
   Inspect chain and action-specific state; preserve unknown outcomes instead
   of inventing a receipt or retrying the old operation.
4. Complete required accounting and safe cleanup under the retained reservation;
   record the reset outcome and explicitly release before baseline can resume.
   If cleanup cannot safely run until after termination/readmission, reset may
   establish that prerequisite but must not waive the obligation. R4 must
   define that ordering and unresolved-cleanup behavior before enabling reorgs.

Normal takeover cannot supersede Armed work. Qualified reset is the explicit
exception: retire its execution identity with the recovery evidence, preserving
its uncertain result. A late receipt for the retired epoch may add evidence
but must not overwrite the replacement's state or release its reservation.

For a same-Pod container restart, lastState.terminated may supply the old
container's termination evidence if its identity matches. Pod replacement has
a new UID and does not retain the old Pod's status. Covering drain/eviction
therefore requires durably capturing trusted termination evidence for the old
Pod UID/containerID before deletion, then joining it to fresh readmission.
Without that captured evidence, replacement remains fail-closed.
[StatefulSet replacement ordering](https://kubernetes.io/docs/tasks/run-application/force-delete-stateful-set-pod/)
can corroborate a qualified graceful-termination path; the new same-name Pod
alone is not proof, particularly after force deletion or node unreachability.
Both paths still require qualification before support claims.

New container identity alone, Node Ready, cached termination status, Pod
deletion, or elapsed time does not establish the required evidence. Under
node partition, kubelet loss, missing termination history, or uncertain process
identity, fail closed. A replacement on another node does not prove the old
process stopped. This follows the distinction between API deletion and actual
termination in [Kubernetes Pod lifecycle](https://kubernetes.io/docs/concepts/workloads/pods/pod-lifecycle/#forced-pod-termination).

[getrpcinfo](https://bitcoincore.org/en/doc/31.0.0/rpc/control/getrpcinfo/)
lists active methods and durations, not queued work or durable request receipts.
Use it diagnostically. Per-dispatch destinations can help attribute generation;
neither technique fences a caller or proves absence of effects from the
canonical chain alone.

The reset is an explicit, bounded experiment event, not an automatic scenario
step or restart loop. Exact API ownership, termination evidence collection,
credential provisioning, and R4 ordering remain design/qualification gates.
If this contract cannot be established, retain exclusion or create a fresh
isolated environment without making claims about the old one. Evaluate an
execution-aware adapter only if the constrained contract cannot meet the
required availability; its crash windows need qualification too.

An execution-record UID must be pinned during managed-instance enrollment.
While the owning network remains active, missing/recreated records fail closed;
controllers must not treat lost state as a fresh target. Action deletion must
not erase an unresolved record. Tie its lifetime to the network, not the action.

Network or namespace deletion explicitly abandons that environment. Stop new
authorization, attempt bounded best-effort recording of an abandonment outcome
in available status/Events or a configured evidence sink, and remove owned
cleanup/exclusion finalizers without waiting for RPC quiescence. Do not add a
record finalizer solely to retain unresolved RPCs after environment deletion.
Optional evidence sinks must not block teardown; export evidence first when
retention matters. If controllers or the API are unavailable, recording/removal
may need administrative intervention and evidence may be lost. Abandonment
never claims successful cleanup or authorizes resumption of the old target.

When a receipt is durable, release additionally respects the bounded action's
reservation and cleanup obligations. This proposal does not resolve R4 or
allow baseline production between a reorganization's dependent mutations.

## Alternatives

| Alternative | Assessment |
| --- | --- |
| Complete Ready inventory | Reject for mutation admission: it prevents bootstrap and couples unrelated health. |
| Leaf status and ownership alone | Insufficient: they can describe an earlier parent declaration. |
| Copy the aggregate compiler into every consumer | Avoid: runtime coupling and duplicated compilation authority. |
| Lease expiry plus chain-tip inspection | Reject as dispatch authority: neither fences a stale caller or queued server work. |
| Direct RPC plus sticky execution record | Initial direction; ordinary response loss can leave a target indefinitely blocked, requiring a fresh isolated environment. |
| Direct RPC plus explicit reset/readmission | Deferred; requires old-process termination, credential fencing, fresh admission, and cleanup accounting. |
| Execution-aware local adapter | Deferred unless operational need justifies it; requires durable receipts, identity, fencing, crash recovery, and exclusive backend access. |

A transport-only proxy does not make mutation execution idempotent. An adapter
crash between backend execution and receipt persistence remains uncertain
unless its backend/recovery contract resolves that window.

## Implementation sequence and acceptance

1. Freeze the additive catalog fields and compatibility vectors; implement
   aggregate publication with focused unit/API-server coverage.
2. Review the smallest baseline-production profile: target admission, static
   credential permissions, durable execution records, and response-loss behavior.
3. Implement and qualify that baseline vertical slice, including record-loss
   handling, exact RBAC, and receipt/status transfer. Add finite generation
   against the shared protocol when its lifecycle contract is ready.
4. Measure behavior under relevant faults before deciding whether deferred
   reset/readmission or an adapter is needed.

Acceptance must cover Stacks bootstrap, unrelated failure and rollout, stale
catalogs, catalog preservation through unrelated status failures/retirement,
publication conflicts, foreign/recreated objects, digest drift, Service/IP replacement,
same-Pod container restart, and authentication mismatch. Test every crash
boundary around reservation, Armed persistence, send, response, receipt
persistence, status accounting, and release. Include lost CAS responses,
duplicate reconciles, stale caller resumption, record deletion/recreation,
late receipts, admission races, and bounded-action cleanup. Verify that receipt
accounting and executor takeover never release an action's reservation between
dependent calls when finite actions are enabled. For deferred reset support,
qualify cookie/static-credential configurations, old-epoch
rejection, prohibited credential refresh, lost termination evidence, partitioned
nodes, and interrupted reset handling before claiming reset-based recovery.

The spike tests cover only the stated premises. R1/R2 remain open until the
recommended design and its cross-dependencies are reviewed; M0.3/M0.4 are not
marked complete by this proposal.
