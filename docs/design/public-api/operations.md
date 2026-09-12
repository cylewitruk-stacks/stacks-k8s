# Operators, workloads and visible interfaces

Status: proposed. Names below define required products/artifact roles, not a claim
that the new versions or images have been built. No per-network Helm release is
required. Install shared controllers once, then apply namespaced declarations.

## Installed products

| Installation | Responsibility and scope |
| --- | --- |
| Network operator chart | Installs network/Bitcoin/Stacks v1alpha2 CRDs, shared controller Deployment, service account, RBAC and profile defaults. Watches permitted namespaces; all-namespace installation is the local development default. |
| Action operator chart, optional | Installs bounded action APIs and lifecycle controllers; observes executor records, never holds Bitcoin RPC mutation credentials. Installs independently and enables actions for selected namespaces. |
| Observability operator chart, optional | Installs NetworkTelemetry/EvidenceExport APIs and passive controllers. Store credentials/retention are administrator inputs. No baseline dependency. |
| Chaos Mesh plus stacks chaos profile, optional | Upstream fault controllers/daemon and validated actor selectors, durations and control-path exclusions. No scenario controller. |
| Local cluster addons | Existing Headlamp and Metrics Server installation helpers. Development cluster-admin dashboard access is a local installation choice, not a workload permission. |

CRD installation is cluster-scoped; resources are namespaced. Shared operator
Deployments may restart or roll. Scoped network workloads remain independent of
that Deployment's lifetime. Chart updates do not automatically replace live Stacks
management Pods or migrate their image profile. A new worker image/profile is selected for
new networks; incompatible network API upgrades require fresh execution state.

## Controller ownership and subscriptions

The network controller alone writes each participant's complete admission and
resolution/policy projections, as well as network lifecycle/status. Domain controllers
publish configuration validation reports and workload/runtime facts; scoped workers
write only their assigned execution status, as defined below. Runtime roots have their generated participant
as controller ownerRef; participants belong to the network; StatefulSet Pods,
Deployment ReplicaSets/Pods and Job Pods retain their standard workload-owner chain.
All carry network/participant labels identifying the runtime instance; reusable identity children
belong to their account/node/faucet. Retained PVCs have network labels but no cascading
ownerRef. Watches of
network-associated runtime use these labels to enqueue the appropriate domain controller.
Kubernetes Events are notifications, not reliable command queues.

| Controller / operator | Watches or polls | Managed outputs/effects |
| --- | --- | --- |
| Network / network | Root entries, referenced definitions/schedules, identity/configuration reports and instance status | Resolve selected topology; own participant specs, complete admission and resolution/policy conditions; publish StacksGenesis, lifecycle, frozen-gate progress and shared identity records. Preserve reusable declarations. |
| Participant domains / network | Generated instances filtered by kind, admitted policy, candidate inputs, direct dependencies, owned runtime | Provision non-mutating configuration resolvers; report validation, workload/runtime references, readiness and placement. Consume aggregate admission before activation. |
| Genesis / network | Selected validated initial inputs | Network controller creates one immutable StacksGenesis, reports digest; renderers consume it, no autonomous runtime. |
| Epoch schedule / network | Immutable schedule spec | Structural validation/digest only; consuming profile checks compatibility. |
| Account / network | Key spec, Secret metadata, resolver report | Optional reusable key Secret and public identity, resolved from direct inputs even without a network. No lease, funding or nonce state. |
| Bitcoin wallet / network | Key/descriptor inputs, resolver reports | Reusable descriptor/key identity resolved from direct inputs without a network; no node-global balance. |
| Bitcoin node / network | Frozen network context, admitted walletRefs/peers, owned-runtime labels, control-worker reports | Participant-owned StatefulSet/Services/config/PVCs and control-worker Deployment; projects node/wallet readiness. The scoped control worker alone loads wallets and executes mutation RPC. |
| Stacks node / network | Frozen network genesis, Bitcoin/account refs, peers, signer attachments and config Secret metadata | Reusable generated identity if omitted; participant-owned config, StatefulSet/Services/PVCs and signer event subscription. |
| Consensus signer / network | Node/account refs and run runtime | Participant-owned StatefulSet/event Service/config/PVC. No stacking administration. |
| Stacker / network | Admitted holder/admin/signer/ingress refs, policy generation, live worker reports | One participant-owned Go worker Pod and readiness/placement facts; worker performs registration/manager deployment and maintenance. Shared identities permitted. |
| Contract set / network | Admitted public deployer/registry inputs, context, worker report | Public configuration validation and one participant-owned Go worker Pod; worker performs exact real-contract initialization. |
| Faucet / network | Selected faucet/account, admitted ingress and worker report | Optional reusable account, one participant-owned Go worker Pod and runtime facts; request controller admits requests, worker executes them. |
| Faucet request / network | Explicit networkUID, faucet/destination, deadline | No additional worker. Publishes resolved admission for the surviving faucet worker to watch; no implicit replay into later runs. |
| Transaction production / network | Admitted account/recipient/ingress policy, context and worker report | One participant-owned Go worker Pod and runtime facts; worker offers STX transfers under aggregate-admitted live policy. |
| Bitcoin schedule / network | Immutable cadence | Validated reusable value; no workload. |
| Bitcoin production / network | Active network, schedule/override, selected targets/wallet views, records | Network-owned initialization/target execution records; scheduling policy/status on the participant. Per-node workers alone perform generation. |
| Schedule override / network | Explicit network and production refs, clocks | Activation/expiry/cancellation status; no Pod or baseline spec restoration. |
| Bitcoin actions / action | Explicit network/runtime target, execution records and deadlines | v1alpha2 lifecycle/status/cleanup; no RPC credentials. Existing Bitcoin executor mechanism semantics retained. |
| Network telemetry / observability | Explicit network, runtime labels/metadata, sources/backend | Independently owned read-only collectors/query Service. Ends that run's capture on deletion; does not follow its successor. |
| Evidence export / observability | Telemetry, time window, backend and timeout | Independently owned export Job and artifact summary; no action/replay. |
| NetworkChaos / Chaos Mesh | Exact network UID selectors, actor Pods/nodes, duration | Upstream injection/cleanup, using qualified protocol paths. |

Bitcoin execution records remain visible, controller-managed APIs. They belong to
the network and identify the source production/action and exact runtime target.
Preserve the [Bitcoin execution contract](../target-admission-and-rpc-execution.md)
for ambiguity, receipts and cleanup; adapt record ownership/identity to run scope.
Do not reuse a record or its counter for a later network. User declarations request
production/actions; users do not create or edit execution records directly.

Root watches route generation, UID, deletion, binding and relevant validation changes
to admission resolution through dependency indexes. High-frequency counters and
unchanged observation heartbeats do not enqueue the whole graph. Meaningful
health/control changes still drive the separate network lifecycle projection;
freshness expiry is scheduled from the release observation policy without rebuilding
admission. An admission-only predicate must not suppress those transitions. Keep
uncached identity reads at authorization/freeze boundaries; memoize only within that
validation pass. Controller resync repairs missed state, not a periodic full-topology
rebuild.

Bitcoin finite actions consume the existing per-node execution reservation. The
independent action lifecycle controller acknowledges exact admission and final receipts;
it never writes executor status or provisions a second mutation worker. Generation and
reorganization require separate installation opt-ins, both disabled on the network
operator by default. Unknown Armed work retains exclusion across worker replacement.
Change installation flags only after reservations settle or environment disposal;
disabling removes read permission and can strand cleanup. It is not cancellation.
Use action deletion or network pause while the installed capability retains access.

The scheduler checks signing-Secret metadata while admitting a short-lived baseline
offer. Bitcoin workers recheck public account/wallet identities and their own exact RPC
credentials/configuration at dispatch. They receive no Stacks signing-Secret permission;
deleting such a key affects its signing holder, not the immutable public payout address.

## Images and configuration artifacts

Every installed release must publish a profile manifest with immutable image
digests, configuration schema version, supported epoch windows, contract-source
hashes and Go protocol-library/profile revisions. Mutable tags in examples are local convenience
inputs;
resolved imageIDs belong in status/evidence. The following artifacts are required:

| Artifact | Runs where / contents |
| --- | --- |
| Network operator OCI image | Shared Go controller Deployment. Kubernetes resolution, workload/config compilation, status and scheduling. No private signing keys or JavaScript dependency in this process. |
| Network resolver OCI image | Short-lived scoped Jobs. Go TOML rendering/validation and Go key/address encoding; receives only relevant immutable inputs/Secrets. No chain mutation. Publishes public identity/config reports and private rendered config Secrets. |
| Stacks protocol worker OCI image | One Pod per stacker, faucet, transaction producer or contract set; one Go binary with explicit role entrypoints. Watches admitted CR state, queries/signs/submits through libs/stacks, and owns in-memory nonce/protocol state. Pinned real sBTC/direct-manager sources included. No topology or workload management. |
| Bitcoin control worker OCI image | One Deployment per BitcoinNode. Go wallet/RPC execution, scoped Kubernetes execution records and retained Bitcoin uncertainty semantics. May share a release/build with the network image but remains an explicit runtime entrypoint. |
| Bitcoin Core | `bitcoin/bitcoin:31.1`, qualified platform digest in release profile. Ten Core Pods in the example. |
| Stacks node and consensus signer | Independently selectable OCI images exposing `stacks-node start --config` and `stacks-signer run --config`. Example uses the locally built `stacks-core:4.0.1-pox5`; this tag is not a public registry promise. |
| Action operator OCI image | Independently versioned Go lifecycle controller Deployment, optional. |
| Observability controller/collector OCI images | Independently versioned Go controller and collector/query/export entrypoints, optional. No signing material. |
| Chaos Mesh/Headlamp/Metrics Server | Existing independently pinned upstream installation artifacts; not built into the network image. |

The Stacks profile must pin tested Core, Go library and contract revisions supporting
both PoX-4 and PoX-5; pin Stacks.js separately as a development-only test oracle.
The current real sBTC source
baseline is revision `d31b0780cf1ae91b6ef4ed7ee89400c796aea913`; retain exact source
hashes in the profile, and qualify any replacement. Hacknet's small mock contracts
are not interchangeable with this bundle. No sBTC signer daemon, Stacks API indexer,
PostgreSQL, legacy receipt receiver, source checkout or image build service is
required by this direct-stacking profile.

Rendered artifacts are network-owned immutable StacksGenesis, per-participant public
binding records/configuration ConfigMaps, public resolver reports, and immutable
private config/key Secrets.
Actor config changes create a new digest-named config artifact and roll that actor;
previous config remains available until no owned Pod references it. Mutable worker
policies are admitted through CR status without changing the Pod template. ConfigMaps
carry configuration artifacts, never faucet requests or pause commands.

## Go protocol library and runtime boundary

Controllers, resolvers and workers run Go. The independent libs/stacks module owns
protocol RPC/encoding/signing; workers execute admitted policy and own nonce
coordination and submission.
Signing keys stay in scoped worker/resolver Pods. Stacks.js 7.6.0 is a development-only
oracle, not a runtime dependency. [Implementation notes](implementation-notes.md#go-protocol-library-and-runtime-boundary)
define module isolation, source attribution and qualification requirements.

## Worker-control transport decision

**Selected:** Go workers consume the existing custom resources using client-go /
controller-runtime list/watch and publish execution status through the Kubernetes
API. No custom HTTP command client/server, command-bus CRD, broker or mounted
ConfigMap command/acknowledgement document is required. Streaming lists may optimize
initial synchronization when supported by the selected client; ordinary list/watch
is sufficient. Watches provide prompt change notification, not a latency SLA or
exactly-once execution. Use standard reconnect/relist handling, including expired
resource versions; do not interpret resourceVersion as an application sequence.
See [Kubernetes change detection](https://kubernetes.io/docs/reference/using-api/api-concepts/#efficient-detection-of-changes)
and [streaming lists](https://kubernetes.io/docs/reference/using-api/api-concepts/#streaming-lists).

A notification queues reconciliation of current admitted state, never an unconditional
transaction send. Watch reconnection is normal transport recovery; the surviving
process retains request/nonce state. A crashed worker still fails the network.
Implementation tests must cover relist/duplicate events, stale cached Pending state,
lost status acknowledgements, deletion/expiry, capacity and operator restart. Test
the candidate → durable Bound → activation handshake separately; watches do not
replace root-owned session identity or authorize worker crash recovery.

## Services and worker interface

| Endpoint | Clients / purpose |
| --- | --- |
| BitcoinNode P2P 18444 | Bitcoin peers, actor protocol traffic. |
| BitcoinNode RPC 18443 | Bound Stacks nodes/miners with restricted credentials; node control worker with separate mutation credentials. Wallet RPC uses the bound named wallet. |
| StacksNode P2P 20444 | Stacks peers. |
| StacksNode RPC 20443 | Consensus signer and assigned Stacks workers; native state, contract, transaction and submission APIs. |
| StacksSigner events 30000 | Its configured Stacks node's required native event observer for consensus input. |
| Kubernetes API | Scoped Go workers watch admitted CRs/root lifecycle and apply only their assigned execution fields through the status subresource. No worker status Service or application command endpoint. Standard local process probes are not a control protocol. |
| Optional telemetry query 8080 | User/agent via port-forward; `/healthz`, `/v1/sources`, `/v1/events?from=...&to=...&cursor=...`. Read-only paginated facts/gaps, maximum 1000 entries per page. |

Status publishes actual endpoints and workload refs; clients must not invent DNS or
follow a stable definition-name alias into a replacement network. Names are UID-derived,
with bounded suffix space for Kubernetes descendants. See the
[naming contract](implementation-notes.md#generated-names) for exact formulas and limits.

## Admission, execution status and faucet dispatch

Before genesis freeze, the network controller persists each initial participant's complete
public policy and resolved input identities in status.admission, using domain validation
reports and reusable identity reports. Candidate configuration resolvers may run before
admission; configuration validation must not depend on an activated actor. This policy admission
does not activate runtime: genesis and exact runtime bindings are added only when
available. Freeze captures the admitted policy digests and bootstrap requirements;
conflicting updates cannot replace those policies while their gates remain unfinished.

The network controller alone publishes complete `status.admission` bindings on
Stacks participant instances: network/participant/Pod UIDs, pinned resolved dependencies, selected
policy generation/digest and complete public policy inputs. Workers watch their own
participant, root lifecycle and, for a faucet, its requests. They never reconstruct a
partially resolved binding from unrelated informer snapshots. Activation requires
the exact durable root Pod binding. Policy adoption occurs at safe operation
boundaries; source generation changes alone do not authorize execution. Workers also require
current root membership and exact participant UID. Entry omission, instance deletion
or root stop/delete withdraws new activity even if admission status is stale. Definition
existence is not membership. Rejected candidate updates retain only the prior complete
admitted policy while its original dependencies remain valid.

Status ownership is explicit:

| Resource / fields | Writer |
| --- | --- |
| StacksNetwork status, including allocated participant and bound worker identities | Network controller only. |
| Participant complete status.admission and resolution/policy projections, including Resolved and PolicyDeferred conditions | Network controller only; domain reports supply facts, never partial admission writes. |
| Participant configuration validation reports, status.runtime, workload readiness and placement conditions | Corresponding domain controller; ConfigVerified, WorkloadReady and placement conditions remain disjoint from aggregate resolution/policy conditions. |
| Participant status.execution | Bound worker: applied policy digest/generation, acknowledged control generation/root lifecycle, observation time and bounded protocol summaries. |
| FaucetRequest status.admission | Request controller: decision/reason, exact network/faucet/worker binding, resolved immutable transfer inputs and expiresAt. |
| FaucetRequest status.phase / conditions | Request controller projects admission/execution and worker availability; never asserts inclusion or no-send without evidence. |
| FaucetRequest status.execution | Bound faucet worker: phase, TxID, outcome/inclusion, observation time and worker/network UID. |

Existing public policy/control summaries are controller projections of those worker
acknowledgements, not independently inferred execution. The root reports Paused only
once current acknowledgements agree. All shared-status writers use server-side apply
through the status subresource with fixed managers and minimal assigned-field payloads.
Conditions are map-lists keyed by type; no condition entry has concurrent writers.
See the [status apply contract](implementation-notes.md#shared-status-apply-contract)
for schema, payload, migration and handoff requirements. Instance deletion follows
process termination; no ownership transfer permits reuse of an old instance.
Status writes carry object UID/conflict
preconditions and cannot create or adopt a replacement CR.
A worker may update only its assigned execution subtree; RBAC grants status access
at resource/object granularity, not per field. Field ownership is a trusted-worker
contract, not an RBAC isolation guarantee.

Until a participant kind has a domain runtime controller, the network controller
owns only that kind's fallback WorkloadReady=False, reason RuntimeNotImplemented.
Admission may still succeed for a complete valid cohort; it does not assert actor or
worker existence or permit an unsupported initialization gate to pass. Transfer this
condition to the domain controller in the same delivery that enables its runtime,
disable the fallback for that kind and test the handoff. The network controller
retains complete admission ownership throughout.

For requests, absent execution means Pending while admission is unresolved. A
negative admission decision exposes Rejected (or Expired only when execution was
never authorized). Once admitted, only execution evidence establishes the outcome;
loss of worker/control access exposes Unknown/Failed readiness without inventing a
transaction result. At expiry, admitted work with no conclusive outcome projects
Inconclusive, not Expired. This projection does not overwrite worker evidence or
prevent later exact observations from refining the result. Execution phases and
receipt meaning are defined in
[the faucet contract](resources.md#stacksfaucet-and-stacksfaucetrequest).

The surviving worker serializes request handling with its account state. Before a
new dispatch, read the request and required admission/root state directly from the
API; verify current UID, binding, eligibility, no deletion, no terminal outcome,
network pause (and capability pause where supported), admitted membership with the
current-entry check above, and deadline.
Record the request UID locally before sending. Cached
Pending events and initial-list ADDED events are not dispatch permission. The final
read and chain submission are not atomic: a concurrent pause/delete takes effect
when processed before the send, and cannot cancel an already-started operation.

Retain each outcome until its status write is confirmed, including readback after
an uncertain acknowledgement. A settled terminal request then frees its worker slot
without requiring deletion/export. Before handling any replayed event, a fresh GET
must establish eligibility; a terminal, absent or different-UID object cannot send.
Keep unresolved account coordination even if the request is deleted; do not recreate
that CR to publish an outcome. Its bounded public summary remains on the participant until
destruction.
The bound is 1000 active requests/unpersisted outcomes per worker; reject additional
admission with CapacityExceeded without truncating unresolved state. Unresolved
Inconclusive submissions continue to consume capacity. No command-document byte
limit, acknowledgement revision sequence or lifetime faucet-request limit is needed.

Pause prevents new baseline and faucet submissions when observed; admitted but
unsent faucet requests wait, and their original deadlines continue. Workers keep
observing in-flight operations. During API/watch outages, workers continue already
applied baseline policy, but admit no new faucet sends without fresh API reads;
new policy/pause acknowledgements remain unavailable. This availability choice is
not immediate remote revocation. Known worker exit/Pod loss invokes terminal failure;
read errors alone do not prove exit. Protocol freshness follows the
[release observation policy](#observation-policy). Stale reports cannot
establish Operational or a newly acknowledged pause.

### Observation policy

The immutable release-profile manifest owns runtime.observationPolicy for network
capability workers and controller projections. It is not a user-configurable CR field.
The regtest-pox4-pox5-v1 values are pollInterval: 2s, rpcAllowance: 10s,
heartbeatInterval: 5s and progressGrace: 120s. The resolved values/profile revision
are published as StacksNetwork.status.observationPolicy and passed to workers as
immutable configuration. Optional telemetry has its own collection configuration.

Freshness expires at `observationTime + 3 * pollInterval + rpcAllowance` (16s for
this profile). Bitcoin and traffic progress each use a window of
`max(progressGrace, 3 * effectiveCadenceUpperBound) + rpcAllowance`. For Bitcoin,
use the current admitted Fixed interval or Uniform maximum, including an active
schedule override; for traffic, use its admitted transfer interval. Pauses do not
refresh progress. Both progress windows must have evidence before Operational=True;
slower cadences increase the window, not the age of the underlying read observation.
After a timing update, re-evaluate the window using the applied policy. Publish the
effective window and last successful generation/inclusion time with that capability's
observations. Fresh reads of an unchanged old transaction do not refresh its progress
time; canonical inclusion must still be verified at the reported observationTime.

Publish execution-state transitions and policy/control acknowledgements promptly.
Coalesce routine counters and unchanged successful observations into a bounded
heartbeat, rather than patching on every protocol poll. The
heartbeat must remain shorter than the observation freshness window. Judge freshness
from observationTime, the time of the actual successful protocol observation, not
the status write or heartbeat time. Republishing cached data cannot refresh it.
Failed reads and API outages must therefore age observations out even if other
status fields continue changing. Controller projections follow the same rule.

## Worker placement

Root defaults.workerPlacement and per-participant workerPlacement use
nodeSelector and tolerations. They affect support Pods only. With no selectors,
normal scheduling may colocate actors and workers. To test node-wide faults, put
workers on explicitly selected nodes outside the fault target set and ensure those
nodes have capacity; placement alone neither isolates packets nor prevents host-wide
failure in kind. A worker node drain/eviction can end the experiment. The examples
place support on the control-plane node and actors on workers; faults must respect
that selection. Bitcoin RPC control connectivity remains independently required.
These are explicitly stacks-k8s three-node kind templates; change selectors before
start on another cluster. PlacementError surfaces scheduler rejection. Live
Bitcoin control-worker placement overrides follow drain/retained-record semantics;
bound Stacks-worker placement requires recreation. Redundant spreading preferences are
omitted when a single-host selector already fixes placement.

## Permissions and admission surface

Users need namespaced CRUD for declared CRs and imported configuration Secrets.
Controllers own generated workloads/status, not user key contents. Resolver Jobs
receive narrowly scoped input/output access; Stacks worker Secrets are mounted only
for the declared role (holder/admin/consensus authorization as required, never all
network accounts). Operator Secret metadata inspection uses metadata-only clients;
no general Secret data cache. Bitcoin control workers receive their own target
credentials and record scope, not unrelated network accounts.

The participant workload controller creates and owns per-worker ServiceAccount, Role
and RoleBinding resources beneath that participant. Its installation permissions must
cover this namespaced provisioning and the permissions being delegated; do not infer
blanket bind/escalate grants. Implementation must update exact rendered operator/worker
RBAC allowlists and test both creation and cleanup of these objects.

Stacks workers receive a dedicated namespaced ServiceAccount with short-lived
projected API credentials: get/list/watch for required public CR kinds and patch on
assigned status subresources. Restrict named participant/root access where supported;
faucet request collection watches are namespace-scoped. Labels/field selectors route
work, not RBAC authorization. No worker Secret API reads, workload writes, exec or
cluster-wide permissions; keys remain scoped mounts. Rendered RBAC checks verify
resource/subresource, name and verb scope. Disjoint admission/execution field ownership
is a trusted-writer protocol checked in implementation/tests; RBAC cannot enforce
status-field boundaries. No field-authorization webhook is added.

Static schema/CEL validates kind/ref/inline unions, the name-keyed participant list, immutable root
inputs and generated genesis spec, bounds and shape. Live
cross-resource rules are resolution conditions before side effects: membership,
UID/Secret identity, required dependency agreement and genesis agreement. Shared accounts are
permitted; no writer/role exclusivity
or duplicate-key admission check. No webhook is needed for the initial design.
An admin changing a worker Pod or managed credential can invalidate the experiment;
the trust model does not promise to protect against cluster-admin interference.

Runtime objects carry network.stacks.org/network (root name), network-uid,
participant (entry name), participant-uid and participant-kind labels under the same
prefix. Preserve network.stacks.org/actor as the logical actor name on actor Pods;
use the separate network.stacks.org/role=actor or support label for classification.
Referenced source kind/UID are provenance labels only. The full source name is in
the network.stacks.org/source-name annotation and source status reference, never a
label value; source UID supports label-based lookup. Inline entries have no reusable
source UID/name metadata. Runtime/fault identity always uses participant UID. Two entries
using one definition must never collapse into the same selector or workload identity.
Observability reads public CRs/Pods/Events/logs/metrics; no signing Secret reads, exec,
workload mutations or actions. Loki/OTel integration is optional collection/backend
configuration, not an operator lifecycle dependency or complete forensic archive.

## Chaos Mesh interaction

Chaos Mesh is optional and independently installed. Users create native NetworkChaos
against instantiated actor Pods, never reusable descriptors. The network operator
keeps reconciling declared topology during faults; degraded protocol observations do
not themselves authorize deleting/reinitializing actors or resetting genesis. Normal
StatefulSet process replacement remains part of actor behavior. Removing a participant
withdraws its workload; a fault does not retain membership or recreate the actor.

The proposed profile requires spec.target and same-namespace selectors on both ends.
Both the primary selector and target.selector must contain exact network-uid,
participant-uid and role=actor labels under network.stacks.org/. participant-kind may
further constrain selection; it never substitutes for participant-uid. Correlation
metadata remains required. Names are display/search hints.

Creation validation rejects an omitted target or missing/wrong identity/role label
on either endpoint, including partitions. A kind-only selector with mode: one is not
admitted: random selection is not exact participant identity. Rendered CEL/API-server
tests cover missing target, both endpoints independently and selector bypass alternatives. A native
label selector may affect a replacement
Pod for the same participant UID; it is not immutable Pod-UID fencing. Observe selected
Pod UIDs and replacement gaps. Participant/root replacement must not match an old fault.
A selector matching no current Pod is a no-op, not proof of complete network health;
static admission does not validate live membership. See [upstream selector semantics](https://chaos-mesh.org/docs/define-chaos-experiment-scope/).

The profile's selector admission, delay/partition bounds and control-path exclusions
require joint qualification. [Implementation notes](implementation-notes.md#chaos-profile-transition)
cover the selector transition and legacy-object cleanup.

Protocol partitions must preserve worker→actor RPC, worker→Kubernetes API and consensus
support paths that are outside the intended fault. Separate Services do not isolate
traffic to one Pod; qualify actual CNI/runtime paths. Broad Pod/node/IO faults may
remove that access; worker Pod loss still ends the experiment. Exclude worker-hosting
nodes for actor-only host faults, while remembering kind nodes share a physical host.

Native fault objects remain user-owned, with Chaos Mesh owning injection/recovery and
its finalizers. Network paused does not pause a fault or its duration/cleanup clocks.
For qualified teardown, delete faults and observe native recovery, collect evidence,
then delete the network. Root deletion is not blocked on the presence of optional
Chaos CRDs, nor does it automatically delete user-owned faults. Surviving faults keep
the old UID selectors and may consume namespace quota until explicitly removed.
Network removal or native fault recovery is not evidence of protocol recovery;
verify chain/transaction/signer progress separately. No fault wrapper or scenario CR
is added to StacksNetwork. Observability records facts; the agent sequences experiments.

## Kubernetes grounding

The ownership rules use Kubernetes' [owner/dependent
model](https://kubernetes.io/docs/concepts/overview/working-with-objects/owners-dependents/):
references do not automatically confer garbage-collection ownership. Defaults and
immutability belong in the [CRD schema and validation
surface](https://kubernetes.io/docs/tasks/extend-kubernetes/custom-resources/custom-resource-definitions/).
Short-lived resolver Jobs tolerate retries because their work is identity/config
resolution; [Job retry semantics](https://kubernetes.io/docs/concepts/workloads/controllers/job/)
are deliberately not used as transaction-recovery semantics. Non-restarting owned
management Pods are the explicit consequence of this test profile's crash contract,
not a general recommendation for resilient production services.
