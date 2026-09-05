# Security and safety model

## Trust boundaries

| Principal/component | Trusted for | Not trusted or permitted for |
| --- | --- | --- |
| External agent | Choosing and interpreting an investigation within granted policy | Cluster administration, status writes, safety-policy changes |
| Network aggregate/actor controllers | Compiling topology/baseline declarations and reporting admitted identity | Mining RPC, traffic generation, faults, protocol conclusions |
| Action controllers | One typed mechanism and its lifecycle | Cross-action sequencing, arbitrary RPC/shell, diagnosis |
| Bitcoin production controller | Maintaining bounded-rate policy across admitted Bitcoin targets | Action sequencing, topology mutation, completion claims |
| Transaction producer controller/workers | Offering bounded traffic under declared account and ingress policy | Bootstrap sequencing, protocol correctness or throughput claims |
| Chaos Mesh | Its upstream injection/recovery contract | Stacks protocol correctness |
| Observability operator | Collecting and labeling configured facts | Mutating observed resources, declaring root cause |
| Actor | Its process behavior and self-reported telemetry | Proving its own identity or correctness |
| Kubernetes API/audit | Admitted object state and recorded API events within configured guarantees | Complete application behavior or globally ordered time |

## Identity and confused-deputy prevention

- Same-namespace typed references by default.
- Record network, leaf, workload, Pod, image, and applicable policy identity
  before mutation.
- Use uncached reads immediately before security-sensitive effects.
- Refuse replacement, deletion/recreation, stale generation, and ambiguous
  target identity rather than silently retargeting. Independent target
  admission under R1 must not require unrelated actors to be healthy.
- Use UID-preconditioned deletion and owner checks; never adopt foreign
  same-named objects.
- Treat actor-reported identity or completion as untrusted until corroborated
  by control-plane or external observation.

Content digests protect immutable requests, configurations, images, and
evidence objects. Timestamps and mutable resource versions do not enter stable
identity digests. None of these controls promises repeatable distributed
behavior.

## Admission layers

1. OpenAPI and CEL enforce static type, enum, branch, numeric, and immutability
   invariants.
2. Kubernetes RBAC limits principals to intended resources and subresources.
3. ValidatingAdmissionPolicy enforces namespace and selector profiles where
   expressible.
4. A small fail-closed webhook is justified only for live cross-object or
   aggregate constraints that cannot be expressed statically.
5. Controllers re-evaluate identity, policy, and dynamic preconditions before
   every material side effect.

Admission success is not proof of effect. Controller status and passive
observation record subsequent facts.

## Action safety policy

The proposed namespace `ActionSafetyPolicy` in [Atomic action contract](actions.md)
provides administrator-owned per-resource limits for custom actions. Native
Chaos Mesh resources receive equivalent RBAC/admission caps.

The initial release deliberately makes no atomic aggregate-impact claim across
independent custom and upstream resources. The external agent owns concurrency
choices within admitted per-object limits. Adding a hard aggregate boundary is
deferred until it can preserve direct native CRDs without side-effecting
admission or an orchestration wrapper.

Controllers use an API-server-persisted, target-scoped serialization
reservation only to prevent unsafe concurrent access, not to impose agent
workflow. A process-local keyed guard may reduce contention but is never the
source of exclusion. Policy changes affect new admission; already-admitted
bounded actions retain their recorded policy through cleanup. Before
`ActionSafetyPolicy` is implemented, schema bounds are absolute. For a kind
configured to use policy-based elevation, missing or stale policy fails closed
for new actions.

Mutable `BitcoinBlockProduction` and steady transaction demand are baseline
capabilities, separate from the bounded-action lifecycle and
`ActionSafetyPolicy`. Their schemas need absolute rate/work bounds,
target-selection rules, and explicit coordination with bounded overrides.
The revised production/exclusion contract remains open; see
[Bitcoin lifecycle](bitcoin-lifecycle.md).

## Irreversible actions

Bitcoin block production and reorganization are irreversible at the harness
level. Every Bitcoin mutation controller must:

- require explicit administrator permission and regtest verification;
- use typed closed RPC clients;
- use the target reservation and bounded RPC deadline; and
- avoid blind retry after an uncertain call.

Bounded actions additionally cap count, depth, timeout, and protocol-boundary
risk, persist intent before mutation, and return `Inconclusive` when effect or
attribution cannot be established.

Continuous production makes no completion claim, but it still needs a reviewed
outstanding-request and exclusion contract. RPC timeout, context cancellation,
Lease expiry, or current chain-state absence does not establish server-side
quiescence. R2 must close before enabling retry, takeover, or resumption.

Reorganization cleanup must address temporary invalidation markers on every
exit path (R4). Their removal is distinct from undoing irreversible history.

Deletion stops future work but does not pretend to undo prior chain history.

## Secrets and workload security

- Prefer short-lived projected credentials or immutable Secret references.
- Never copy credentials into status, logs, events, journal payloads, or
  evidence manifests.
- Controllers read only administrator-configured Secret `resourceNames`;
  public action specs never select credentials. R3 replaces the shared Bitcoin
  Secret proposal with separately restricted observation, Stacks-client, and
  mutation authority. Verify RPC permission enforcement server-side.
- The network operator mounts and content-verifies inputs without Secret-read
  RBAC; exact profiles and rotation remain design gates.
- Transaction signing credentials have a separate M0.6 Requirement 14 gate:
  designated worker access, account ownership, rotation/revocation, and
  isolation from actors, observers, and unrelated producers.
- Actor Pods use non-root, no privilege escalation, dropped capabilities,
  runtime-default seccomp, read-only root filesystem where compatible, and no
  ServiceAccount token unless justified.
- Action helpers are separately confined; privileged Chaos Mesh permissions
  remain isolated from stacks-k8s ServiceAccounts.
- NetworkPolicy denies unintended actor and operator egress while preserving
  declared topology dependencies and telemetry endpoints.

Topology editing delegates workload authority: an editor allowed arbitrary
images, commands, environment, or configuration references can influence code
that runs with mounted credentials. Denying direct workload/Secret API writes
does not contain that indirect authority. The supported trust profile must
either trust those authors for the exposed actor authority or constrain images,
templates, mounts, and Secret references through admission and rendering.
Keep observation credentials and unrelated controller credentials outside
actor-controlled workloads; document residual trust explicitly (R6).

## Observability integrity and privacy

Every record identifies source and evidence class. Capture gaps, truncation,
redaction, sampling, clock uncertainty, backend outage, and identity changes
are explicit. An export is complete only relative to declared sources and
known watermarks.

Rolling retention is bounded by time and bytes. Redaction occurs before
durable storage. Export destinations and deletion policy are
administrator-controlled. An observer compromise must not grant workload or
action mutation privileges.

The planned query Service relies on API-server proxy authentication only for
requests traversing that proxy. Direct backend connections bypass it.
NetworkPolicy/CNI and actor trust qualification must establish the supported
boundary; otherwise require a reviewed authenticated backend deployment (R7).
ClusterIP alone is not an authentication boundary.

## RBAC profiles

| Role | Allowed | Explicitly denied/not granted |
| --- | --- | --- |
| Network viewer | Read aggregate/leaves/status | Writes, Secrets |
| Network editor | Edit `StacksNetwork`; read leaves | Leaf/workload/status writes |
| Action user | Create/read/delete approved action kinds; read policy | Action update/patch, policy, status, workload, arbitrary Chaos kinds |
| Standalone baseline editor | Edit separately authorized unowned production resources | Aggregate-owned child specs, status, credentials, arbitrary RPC |
| Observer viewer | Read telemetry/export status and query data | Source or export writes |
| Evidence export requester | Create/read/watch/delete `EvidenceExport` | Telemetry configuration, destination/profile, status, or storage writes |
| Observer operator | Manage telemetry configuration and administrator-approved profiles | Observed environment mutation or agent action selection |
| Administrator | Install/configure policies and privileged dependencies | Subject to cluster policy/audit |

Rendered RBAC is verified against exact allowlists. Wildcards require a
documented exception and negative test. Finalizer updates require update/patch
on the primary resource, in addition to separate status permission. RBAC does
not restrict such patches to metadata; admission and ownership checks provide
complementary constraints. Test actual finalizer add/remove operations (R5).

Standalone and aggregate-owned resources share kinds. RBAC cannot select them
by owner reference; standalone editing needs an explicit namespace/name grant
or reviewed admission constraint before that Role is supported. Controller
reconciliation alone is not authorization.

## Threat-focused tests

- Cached-versus-live identity replacement and stale status.
- Same-name foreign object, owner UID replacement, and selector confusion.
- Agent attempts to edit compiled leaves, policy, status, Secrets, and
  unqualified Chaos kinds.
- Controller restart before/after every irreversible call and cleanup step.
- Forged actor telemetry and observer replacement.
- Audit/log/metric outage, storage full, redaction failure, oversized payload,
  and query exhaustion.
- Compromised controller ServiceAccount blast-radius assertions.
- Supply-chain verification for images/charts and dependency vulnerability
  scanning.

## Alternatives

| Alternative | Disposition |
| --- | --- |
| Trust the external agent with cluster-admin | Rejected as the supported model. |
| Rely on controller validation without RBAC/admission | Rejected; requests should fail before unsafe mutation where possible. |
| One privileged action operator for every mechanism | Avoid; split ServiceAccounts/deployments when permissions differ materially. |
| Treat missing telemetry as healthy | Rejected; detector health and gaps are explicit. |

## Definition of done

- Each component has a documented threat boundary and exact rendered RBAC.
- Supported Roles deny direct compiled-workload mutation; topology authors'
  delegated workload authority is explicitly constrained or trusted.
- Observer credentials cannot mutate workloads or protocol state.
- Every custom action is exact-identity-pinned; native Chaos actions pin their
  immutable request and logical targets and explicitly record Pod divergence.
  Every custom action is independently bounded and safe under retry. It is
  policy-bound only when that kind implements administrator elevation;
  otherwise its schema bounds are absolute.
- Irreversible ambiguity cannot become automatic success or blind repetition.
- Evidence preserves provenance and incompleteness without exposing secrets.

## Open decisions

1. Admission webhook availability and failure policy for enrolled native
   targets.
2. Separate Bitcoin RPC authority and actor testing-interface credentials (R3).
3. Initial supported NetworkPolicy/CNI matrix.
4. Evidence encryption, signing, and retention defaults.
