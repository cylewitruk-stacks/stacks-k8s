# R1/R2 decision proposal

## Status and recommendation

**Recommended design; not a served API or completed runtime gate.** This
proposal supplies the R1/R2 decision record for M0.3/M0.4. The parent
[M0 plan](m0-remediation-plan.md) remains authoritative; production fields,
credential profiles, and implementation qualification still need review.

Recommend a health-independent compiled-declaration catalog for R1 and a
durable, single-dispatch execution record for R2. Investigate a minimal explicit
reset-and-readmission contract together with R3 before selecting the default
recovery profile; a full execution-aware adapter is not an M1 prerequisite.
Retain the aggregate/leaf controller structure. No in-cluster planner,
autonomous local miner, or generic RPC dispatcher is introduced.

The deliberate availability tradeoff is that one unresolved request can block
managed mutations on its target indefinitely. Other valid targets may continue
under the declared selection policy, without redistributing the blocked
target's weight. Automatic recovery after ambiguity is not a v1 promise.
Evaluate this cost against node-local IO/stress faults, Bitcoin restarts, and
producer-controller restarts. The qualified protocol-fault profile preserves
the management path; that does not prevent these other causes of response loss.

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
4. Authenticates the actual RPC server instance and verifies regtest capability.
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

The managed mutation profile must bind its transport to an enrolled server
instance: network/leaf/Pod identity plus process/credential epoch. A replacement
must not accept dispatch credentials or server identity from an earlier
instance. R3 must select and qualify the concrete transport/provisioning
mechanism; an instance-authenticated adapter is an option, not implemented
in this slice. Plain HTTP through a stable Service with shared credentials
does not satisfy this boundary.

This is an explicit dependency of R1, not proof supplied by uncached reads.
Actor-reported identity alone is insufficient. Kubernetes, the credential
provisioner, and the qualified network/endpoint boundary remain trusted;
arbitrary topology-author authority is addressed separately by R6.

### Candidate credential epoch

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
process-start nonce, immutable request identity/bounds, target/credential
instance, and execution state in the same optimistically updated object.
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
leaves the target blocked until qualified explicit recovery; ordinary slowness
can trigger this outcome even if the server eventually finishes successfully.

Receipt collection has a separate lifetime from action waiting and reconcile
calls. A profile may keep one receiver per outstanding target alive without a
read deadline while the connection survives. This needs bounded worker counts,
shutdown handling, and status visibility; it does not solve connection loss or
controller restart. Freeze these choices with R3. This direct-RPC profile does
not promise that irreversible effects cease at a wall-clock deadline.

Deletion, an unchanged tip, Lease expiry, and an Inconclusive phase likewise
do not prove quiescence or release a reservation.

### Recovery, record loss, and replacement

Without a terminal receipt, automatic recovery stays blocked. Reopening a
target requires evidence that the old caller can no longer cause accepted
execution and previous server work has ended, followed by fresh admission. Credential
revocation alone does not drain an already-authorized server request; Pod
deletion in the API alone does not prove a partitioned process has stopped.
No time-based administrator override is presented as safe recovery.

Investigate this minimal explicit recovery contract with R3:

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
Missing/recreated records fail closed; controllers must not treat lost state
as a fresh target. Garbage collection or action deletion must not erase an
unresolved record. Administrative removal explicitly abandons guarantees and
cannot produce a successful cleanup claim.

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
| Direct RPC plus sticky execution record | Conservative option; ordinary response loss can leave a target indefinitely blocked. |
| Direct RPC plus explicit reset/readmission | Preferred next investigation with R3; requires old-process termination, credential fencing, fresh admission, and cleanup accounting. |
| Execution-aware local adapter | Consider if automatic response recovery or stronger expiry is required; needs durable receipts, identity, fencing, crash recovery, and exclusive backend access. |

A transport-only proxy does not make mutation execution idempotent. An adapter
crash between backend execution and receipt persistence remains uncertain
unless its backend/recovery contract resolves that window.

## Implementation sequence and acceptance

1. Resolve the explicit reset candidate and R3 instance-authentication design
   together, against IO/stress faults and Bitcoin/controller restarts.
2. Freeze catalog/admission wire fields and compatibility vectors; implement
   aggregate publication and an independent consumer adapter.
3. Freeze the protected execution-record schema, enrollment/loss behavior,
   exact RBAC, RPC client behavior, receipt/status transfer, and recovery events.
4. Implement baseline and finite generation against that shared protocol.
5. Qualify with API-server and real-image tests before enabling either kind.

Acceptance must cover Stacks bootstrap, unrelated failure and rollout, stale
catalogs, catalog preservation through unrelated status failures/retirement,
publication conflicts, foreign/recreated objects, digest drift, Service/IP replacement,
same-Pod container restart, and authentication mismatch. Test every crash
boundary around reservation, Armed persistence, send, response, receipt
persistence, status accounting, and release. Include lost CAS responses,
duplicate reconciles, stale caller resumption, record deletion/recreation,
late receipts, admission races, and bounded-action cleanup. Verify that receipt
accounting and executor takeover never release an action's reservation between
dependent calls. Qualify cookie/static-credential configurations, old-epoch
rejection, prohibited credential refresh, lost termination evidence, partitioned
nodes, and interrupted reset handling before claiming reset-based recovery.

The spike tests cover only the stated premises. R1/R2 remain open until the
recommended design and its cross-dependencies are reviewed; M0.3/M0.4 are not
marked complete by this proposal.
