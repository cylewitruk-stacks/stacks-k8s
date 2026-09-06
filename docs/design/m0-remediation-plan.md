# M0 architecture remediation plan

## Purpose

This document consolidates the approved remediation decisions from the first
review of the architecture design package. It is the authoritative M0 plan.
Where it conflicts with another document under `docs/design/`, this plan takes
precedence until that document is revised. This plan adopts the
[steady-state operation amendment](steady-state-operation.md), including its
R1–R8 review ledger. M0.3 and M0.4 are reopened; prior Bitcoin fixture payloads
are explicitly superseded history, not implementation authority.

M0 changes design, packaging, and repository foundations. It does not add an
in-cluster experiment planner. The external agent remains the sole
orchestration and reasoning layer.

## Outcome

M0 is complete when:

- the shared network API-module spike has passed or has been rejected with a
  recorded technical reason;
- action APIs share one vocabulary and one typed lifecycle contract;
- Bitcoin baseline production, finite generation, credentials, independent
  target admission, and serialization have implementation-ready designs for
  the initial scope; additional action cleanup gates apply before those actions
  are enabled;
- steady transaction demand, baseline ownership, temporary overrides, and
  controller packaging have reviewed contracts;
- the local-first agent access and observability contracts are internally
  consistent;
- the design index, roadmap, examples, repository layout, and verification
  guidance agree; and
- no unresolved M0 decision blocks the first Bitcoin-generation vertical
  slice.

## Preserved boundaries

- The external agent selects, sequences, and evaluates actions.
- One action resource represents one bounded action.
- Operators do not implement scenarios, workflows, replay, reduction,
  diagnosis, or root-cause classification.
- Observability is passive toward the environment it observes.
- Distributed execution is not claimed deterministic or reproducible.
- New public APIs use Bitcoin rather than burnchain.
- Git checkout and OCI image construction remain outside the operators.
- Generic infrastructure faults use native Chaos Mesh resources.
- Static admission uses OpenAPI and CEL before webhooks are considered.
- Every field and external side effect has one logical writer.

## How to read this plan

M0 uses two identifiers for different purposes:

- **M0.x** identifies an ordered, independently reviewable delivery slice.
- **Requirement N** identifies a stable remediation requirement below. A
  requirement number is not a step number and may support more than one slice.

The slice ledger is the execution order. The requirement sections retain the
review's original numbering so later decisions and reviews can cite a stable
identifier. Cross-cutting requirements are completed incrementally and close
only when M0.8 verifies the package as a whole.

“Complete” in this document means the stated M0 design, contract, or repository
foundation is complete. It does not imply that a proposed controller or CRD is
implemented unless the slice explicitly says so.

For R1–R8, M0 resolves the design and defines acceptance evidence. Later M1–M8
milestones implement and validate those resolutions before enabling the
affected capabilities. A reviewed design does not satisfy a runtime gate.
In particular, M0.8 defines R8 exercises; M8 completes their cross-product
qualification, with relevant cases exercised in earlier vertical slices.

## Ordered delivery-slice ledger

| Slice | State | Primary requirements | Deliverable |
| --- | --- | --- | --- |
| M0.1 | Complete | 1 | Extract the shared network API module and make generation, module verification, and container builds work across the module boundary. |
| M0.2 | Complete | 2–5, 8, 13 | Freeze the typed, immutable, bounded atomic-action contract and structural anti-orchestration checks. |
| M0.3 | Reopened | 9–12; R1–R4 | Amend finite actions, independent target admission, credentials, quiescence, attribution, and cleanup. |
| M0.4 | Reopened | Extends 9–11; R1–R3 | Define Bitcoin role migration, aggregate-owned multi-target production, policy updates, bounded status, and safe action coexistence. |
| M0.5 | Planned | 6–8, 16 | Define and qualify native admission, protocol/control traffic separation, limits, and the initial platform matrix. |
| M0.6 | Planned | 14, 15, 26 | Define bootstrap, steady transaction demand, baseline/override composition, and instrumented-actor support. |
| M0.7 | Planned | 12, 17–23 | Reconcile RPC observation authority, Kubernetes-authenticated agent access, passive observation, journal, query, export, and evidence contracts. |
| M0.8 | Planned | 24–27 | Align layout, fixtures, validation, packaging, roadmap, examples, and the final M0 acceptance record. |

Supporting requirements such as safety bounds, evidence integrity, repository
layout, and roadmap alignment may be updated by an earlier slice. Their final
owner remains the primary slice listed above.

### Reopened Bitcoin scope

M0.3 and M0.4 retain the separation between mutable desired production and
bounded immutable actions. Replace their superseded single-target/global-Ready
contracts with the direction in [Bitcoin lifecycle](bitcoin-lifecycle.md):

- neutral Bitcoin nodes; production policy separately declares timing and
  selection among referenced nodes;
- aggregate-owned policy with reviewed standalone and update semantics;
- target readiness independent of unrelated network health;
- explicit unavailable/reserved-target handling without silent weight changes;
- bounded summaries and honest uncertainty, with passive retained history;
- one effective mutation writer and reviewed server-side quiescence before
  retry, takeover, or baseline resumption; and
- separate observation/mutation authority and complete reorganization cleanup.

Exact schemas, target limits, destinations, precedence, and execution protocol
remain open. Central production requires a qualified management path under
supported protocol faults; autonomous local fallback is deferred. Random
choices are recorded facts, not deterministic replay.

M0.2's shared lifecycle vocabulary remains reviewed. R1 may require an explicit
admitted-identity extension, and R5 requires primary-resource metadata permission
for finalizers; neither is permission to silently change the shared fixture.

The [R1/R2 decision proposal](target-admission-and-rpc-execution.md) recommends
a compiled-declaration catalog and protected single-dispatch execution record.
It includes local feasibility evidence and the explicit availability cost of
retaining unresolved requests. R1/R2/R3 remain open for broader capability
contracts; the constrained implementation and qualification are recorded in
the narrower delivery scope below.

### Initial delivery scope

Start with trusted, disposable regtest networks. Compromise of test-Pod service
credentials is outside this initial profile. Keep basic access separation,
target ownership/identity checks, and honest execution accounting. Static
per-environment RPC credentials are acceptable; credential rotation, per-process
credential fencing, cryptographic process attestation, and in-place recovery
are deferred. They are not prerequisites for R1 publication or initial production.

R1 declaration publication is implemented with an additive schema and
compatibility vector, generation-bound publication, conflict protection, and
preservation through unrelated actor failures. Complete inventory semantics
remain unchanged. This does not close admission for every future capability.

The [initial Bitcoin baseline](../network-operator/bitcoin-production.md) now
implements one-target fixed-cadence production with separated static RPC
permissions, a UID-pinned durable production ledger, bounded work, and explicit
response-loss behavior. Its CRD status provides exclusion for this sole enabled
mutation owner; a shared reservation/action API remains deferred. R1–R3 stay
open for wider capability contracts, and M1 finite generation remains pending.
Possible unresolved execution
keeps the target closed across restarts and replacement. No blind retry or
in-place reset reopens it. Preserve evidence and use a fresh independently
isolated environment: new namespace and UIDs, fresh per-environment RPC
credentials, and no reused data/configuration. A namespace alone does not
fence an already-resolved request. Graceful controller drain stops new arming
and persists outstanding receipts within the shutdown grace period; failed
drains remain uncertain. Network/namespace teardown explicitly abandons work
and must not wait indefinitely for RPC quiescence or an optional evidence sink.
Use observed disruption to prioritize advanced recovery. R4 remains a gate for
reorganization, not for declaration publication or baseline-only production.

## Requirement 1: Shared network API-module spike

**Slice:** M0.1. **Status:** Complete and committed in `0dcf5ac`.

### Decision

Run the spike before implementing action APIs. A shared, types-only module is
the likely foundation for typed action-controller reads, while observability's
byte-preserving inventory verification remains unstructured.

### Target layout

```text
apis/network/
├── go.mod
├── v1alpha1/
└── tools/
    └── go.mod
```

### Scope

- Move network API types, registration, and generated deepcopy code into
  `apis/network`.
- Replace controller-runtime's scheme builder with
  `runtime.NewSchemeBuilder`.
- Permit only the required Kubernetes API and API machinery dependencies.
- Prohibit `controller-runtime`, `client-go`, reconcilers, clients, and
  operator-internal helpers from the API module.
- Make the network runtime import the new module.
- Keep observability's unstructured, byte-preserving inventory reader and
  digest verification unchanged.
- Keep shared wire fixtures. Typed sharing does not replace JSON-field,
  canonical-digest, or skew-compatibility tests.
- Move CRD generation into the isolated `apis/network/tools` module and
  continue writing generated CRDs into the owning chart.
- Add both new modules to root module verification.
- Put module-boundary verification under top-level `tools/`.
- Run generation and independent module builds with `GOWORK=off`.
- Build operator images from the repository root so sibling modules are in
  the Docker context.
- Add a root `.dockerignore` that excludes Git metadata, documentation,
  evidence, test output, build output, and unrelated artifacts.

### Release contract

Document:

- the module path;
- the minimum Kubernetes dependency version;
- the API compatibility policy;
- API-module-before-runtime release ordering;
- subdirectory tags such as `apis/network/v0.1.0`; and
- how local replacements coexist with resolvable published module versions.

### Exit criteria

- Every module builds independently with `GOWORK=off`.
- Generated CRDs are byte-identical to the pre-spike versions.
- Shared contract vectors remain unchanged.
- Runtime behavior remains unchanged.
- `make verify` and `make docker-check` pass.
- Dependency verification proves that the API module excludes
  controller-runtime and client-go.

If an exit criterion cannot be met cleanly, retain independent wire adapters
and record the concrete reason. Do not leave a partially shared contract.

### Spike outcome

- The API and generator modules build independently with `GOWORK=off`.
- Module policy rejects controller-runtime or client-go in the API graph.
- Generated CRDs and shared contract fixtures are byte-identical to the
  pre-spike versions.
- The relocated generated deepcopy file has one formatting-only import-alias
  change caused by the API package importing apimachinery runtime directly.
- Root-context builds for both operator images pass with the repository's
  default-deny `.dockerignore` applied and are enforced in CI.
- `make verify`, `make docker-check`, and `make vuln` pass.

## Requirement 2: Typed shared action lifecycle

**Slice:** M0.2. **Status:** Complete and committed in `dcdd58c`.

**Design status:** reconciled by M0.2 and pinned by the normative
[atomic-action contract](actions.md) plus
[`action-lifecycle-v1.json`](../../contracts/action-lifecycle-v1.json).
Generated-schema enforcement activates non-vacuously with the first action
CRD.

Adopt the compositional controller structure used successfully by Chaos Mesh:

- one typed CRD per bounded action;
- one controller registration per typed CRD;
- one shared lifecycle reconciler instantiated for each action type; and
- one small mechanism implementation per action kind.

The mechanism interface should expose only operations such as validation,
application, observation, and recovery. Shared lifecycle code owns conditions,
finalizers, time bounds, admitted identity, status transitions, and common
evidence correlation.

This is not an untyped action multiplexer. Protocol-specific schemas, RBAC,
packages, and side effects remain independently reviewable.

## Requirement 3: Immutable action specifications

**Slice:** M0.2. **Status:** Complete and committed in `dcdd58c`.

Action-defining fields are immutable from creation. Enforce this with CEL
transition rules such as `self == oldSelf`.

Do not make mutability depend on `status.phase`; spec-scoped CEL cannot safely
express that rule, and a webhook would add an avoidable Pending-to-Admitted
race. A materially different action requires a new resource.

## Requirement 4: Common action vocabulary

**Slice:** M0.2. **Status:** Complete and committed in `dcdd58c`.

Freeze one convention for:

- target and object references;
- phases, terminal states, conditions, and reasons;
- admission, start, expiry, completion, and recovery timestamps;
- admitted network and actor identity;
- admitted safety-policy identity;
- result summaries; and
- observation and evidence correlation.

Kinds may deviate only for a documented semantic reason.

The frozen correlation label is `actions.stacks.org/correlation-id`. It is a
search hint; Kubernetes object UID remains authoritative.

## Requirement 5: Structural anti-orchestration tests

**Slice:** M0.2. **Status:** Contract complete in `dcdd58c`; generated-schema
enforcement activates with the first action CRD.

Replace untestable statements that a controller "cannot become orchestration"
with schema contracts. Atomic action CRDs must not acquire fields such as:

```text
actions
dependsOn
executionPlan
reduction
replay
scenario
schedule
stages
steps
workflow
```

Tests must also assert that one resource continues to represent one bounded
action.

## Requirement 6: Static Chaos Mesh admission first

**Slice:** M0.5. **Status:** Planned.

The v1 ValidatingAdmissionPolicy enforces only static constraints:

- label-based selectors;
- request-namespace confinement;
- enrolled namespaces;
- bounded duration and target count; and
- no raw Pod-name or expression selectors; and
- no remote-cluster targeting.

Qualify source/target/direction semantics separately for Bitcoin P2P,
Stacks-to-Bitcoin RPC, and producer control RPC. Prove the supported protocol
fault preserves the intended management path; separately test loss of that
path. Separate Service names or port-only rules are not proof of isolation.

Package the policy behind an explicit, disabled-by-default chart value. Its
installation requires Chaos Mesh to be installed or explicitly declared as an
external dependency. Match only namespaces enrolled for stacks-k8s use.

## Requirement 7: Defer dynamic enrollment admission

**Slice:** M0.5. **Status:** Planned.

A parameterized admission policy may later compare the requested actor with a
`StacksNetwork` inventory. The vacuous-success form for unrelated network
parameters is viable, but it cannot prove that a matching named network exists
when unrelated parameters are present.

Treat this as defense in depth. Before adoption, test zero, one, and multiple
network objects, absent networks, deleted networks, and stale inventories on
kind. Runtime Pod-UID capture and identity-divergence handling remain
authoritative.

A webhook is the last resort. Any future webhook requires enrolled-namespace
scoping, certificate lifecycle, availability analysis, and an explicit failure
policy.

## Requirement 8: Action safety bounds

**Slices:** M0.2–M0.5. **Status:** Conservative action bounds are designed;
Chaos Mesh limits remain for M0.5 and policy elevation remains deferred.

Each numeric bound must be classified as either:

- a fixed OpenAPI or CEL limit; or
- an administrator-configurable per-kind `ActionSafetyPolicy` limit.

If policy elevation is supported, the policy needs explicit per-kind fields
for offsets, rates, block counts, reorganization depths, and comparable
parameters. Until that policy exists, conservative CRD bounds are absolute.

Cross-kind aggregate admission remains deferred because it would introduce a
coordination layer over otherwise independent actions.

## Requirement 9: Consolidated Bitcoin generation API

**Slices:** M0.3 and M0.4. **Status:** Reopened.

The earlier finite-action baseline in `ba9a666` and M0.4 production amendment
remain available in [historical Bitcoin lifecycle](history/bitcoin-lifecycle-m0.4.md).
Their three Bitcoin fixtures are marked superseded. Current direction is
[Bitcoin lifecycle](bitcoin-lifecycle.md) and
[Steady-state operation](steady-state-operation.md).

Keep `BitcoinBlockGeneration` finite and immutable, with immediate, fixed,
uniform-random, or explicit-sequence cadence. Keep `BitcoinBlockProduction`
mutable and outside the action lifecycle. Its revised policy separates timing
from selection among neutral `BitcoinNode` references. Freeze list bounds,
destination mapping, cadence bounds, weights, update/pause behavior, conflict
rules, and status before implementation; do not infer those fields from the
historical fixture.

### Bitcoin role migration

M0.4 owns this part of Requirement 9. Define removal of Bitcoin miner/follower
roles, compatibility for existing declarations, defaults, generated profiles,
and compiler/leaf/inventory fixture changes. M1 implements and validates the
reviewed migration before enabling the revised production API. The role
contract remains unchanged until then.

## Requirement 10: Attributable Bitcoin generation

**Slices:** M0.3 and M0.4. **Status:** Reopened; R2.

Only successful mutation RPC responses support acknowledged attribution.
Observed chain tips alone do not establish which producer caused an effect.
Lost responses remain uncertain. Current absence at every known tip and an
elapsed client deadline do not prove that a server operation cannot complete
later; neither is sufficient authority to retry.

Continuous production has no terminal completion claim, but unresolved work
still matters for exclusion and bounds. Review outstanding-request persistence,
server-side quiescence, restart, and resumption together. The earlier omission
of per-request production intent is not a frozen requirement. Passive
observation owns retained history and independently collected chain facts.

## Requirement 11: Bitcoin action serialization

**Slices:** M0.3 and M0.4. **Status:** Reopened; R1, R2, R4.

Require shared target-scoped exclusion across production and finite actions,
one serialized owner of reservation writes, uncached admitted-identity checks,
and bounded client resources. Separate action deadlines, transport cancellation,
and receipt collection. A closed response path cannot supply a late receipt.
Reservation ownership persists across dependent RPCs, durable accounting, and
cleanup; executor takeover is not reservation release.
Kubernetes leader election, Lease ownership, and
context cancellation do not fence Bitcoin RPC execution.

Before enabling a mutation kind, define how outstanding server work is known
to be quiescent across response loss, process restart, leader/Lease loss, and
endpoint replacement. State the limits of any execution mechanism or deployment
assumption. Do not implement the old elapsed-deadline takeover rule as proof.

Bounded actions receive priority only when they can actually use the target.
The starvation suite must prove that valid waiting actions progress while
malformed, not-Ready, or otherwise stuck `Pending` actions do not stall baseline production.
Exact eligibility, fair sharing, and overlap rules remain part of M0.4.

For reorganization, specify invalidation-marker cleanup for success, definite
failure, timeout, cancellation, divergence, and restart. Removing a temporary
invalidity marker is distinct from undoing chain history. Unsafe or uncertain
cleanup must retain honest status and exclusion under the reviewed protocol.

## Requirement 12: Bitcoin credential and configuration identity

**Slices:** M0.3 and M0.7. **Status:** Reopened; R3.

Replace the single shared mutation-capable Secret with separately reviewed
authority for observation, Stacks clients, and mutation clients. Define
server-enforced RPC permissions, exact namespaced Secret grants, authentication
profiles, and supported Bitcoin versions. Observation must be unable to invoke
mutation methods with its granted credentials.
Actor clients get only their own restricted credentials, never the shared
producer/action mutation password. Bitcoin servers receive salted verifiers.
Use static rpcauth with per-user rpcwhitelist entries and rpcwhitelistdefault=1
to deny unlisted principals; freeze the necessary method lists with each profile.

Retain high-entropy credentials immutable within their configured profile,
salted `rpcauth` rendering for profiles that use it,
startup digest verification, and credential-free output/status. The network
controller references Secret inputs without reading their bytes. Mounting,
leaf-spec/inventory identity, and client rollout behavior must be reviewed
together. Exact names, keys, API fields, and generated-profile versions remain
open. Static per-environment credentials satisfy the initial scope; rotation
is not an initial production gate.

If in-place recovery is later enabled, evaluate per-process cookie or rotated
mutation-profile epochs with R2's deferred reset/readmission candidate.
Old Armed requests must never refresh
credentials or retarget. Cookie rotation does not itself authenticate the
server, isolate observation authority, or prove old-process termination.
Static mutation credentials cannot satisfy the reset fence. Freeze the
provisioning and termination-evidence contract before claiming recovery.

Configuration-input identity is not rendered-byte attestation. Arbitrary actor
image/command/configuration authority can expose mounted credentials; document
and constrain that delegated workload authority under R6.

## Requirement 13: Time-bound semantics

**Slice:** M0.2, specialized by each action design. **Status:** Complete for
the shared lifecycle; Bitcoin execution guarantees are reopened under R2.

A controller cannot act while unavailable. `spec.timeout` still advances from
the API-server creation timestamp. On restart the controller must refuse to
resume an expired action, inspect ambiguous state, and avoid blind retries.

Actor-side expiry is reserved for effects that genuinely continue without the
controller, such as an activated testing hook with its own absolute expiry.

## Requirement 14: Protocol-bootstrap design

**Slice:** M0.6. **Status:** Planned.

Add a dedicated design for the primitives required to make a Stacks network
productive:

- Bitcoin coinbase maturity;
- account funding;
- signer-key authorization;
- stacking or signer registration;
- miner readiness;
- reward-cycle advancement; and
- Nakamoto readiness verification.

Expose bounded atomic resources or an external helper CLI. The external agent
sequences one-time bootstrap operations. Do not build an in-cluster bootstrap
workflow.

Also define steady transaction demand as an independently reconciled baseline
capability, provisionally `StacksTransactionProduction`. Cover versioned
workload profiles, funding/signing, ingress, per-account nonce ownership,
offered rate, bounded in-flight work, backpressure, pause/update, and ambiguous
submission. It must work without action or observation controllers. Define
single-writer composition with temporary overrides and latest-baseline
restoration; one-time bootstrap remains distinct from ongoing demand.

Transaction signing authority is an implementation gate for this capability.
Before M0.6 closes, define administrator-selected account/credential profiles,
designated worker access, rotation/revocation, and isolation from observer,
actor, and unrelated producer workloads. Public specs must not select arbitrary
Secrets or disclose signing material. M2 validates credential isolation and
account/nonce ownership before enabling producers.

## Requirement 15: Actor testing-capability design

**Slice:** M0.6. **Status:** Planned.

Add a design for instrumented Stacks images covering capability discovery,
activation, bounded parameters, expiry, query, revocation, authentication,
production-build isolation, evidence classification, and unsupported-image
behavior.

Treat this as a cross-repository dependency. Actions that require unavailable
hooks remain conditional and fail before mutation.

## Requirement 16: Qualify native TimeChaos first

**Slice:** M0.5. **Status:** Planned.

Qualify native Chaos Mesh `TimeChaos` against the required Stacks behavior
before designing `ApplicationClockOffset`. Build a custom action only when the
qualification establishes a concrete semantic gap.

## Requirement 17: Agent access and query authentication

**Slice:** M0.7. **Status:** Planned.

### Kubernetes identity

Use the Kubernetes API server as the v1 authentication and authorization
boundary:

- host-side agents authenticate with their existing kubeconfig;
- in-cluster agents use projected ServiceAccount credentials;
- both reach the query Service through the API-server Service proxy;
- the query Service is `ClusterIP`-only and performs no application-level
  authentication; and
- RBAC grants only `get` on `services/proxy`, restricted by `resourceNames` to
  the query Service.

Keep the v1 query protocol read-only and GET-based. A thin CLI uses the normal
Kubernetes client transport and owns no credentials.

Authorization is intentionally coarse: an identity allowed to proxy to the
Service may call every read-only query endpoint. Defer TokenReview,
SubjectAccessReview, kube-rbac-proxy, direct OIDC, mTLS, and per-query
authorization until direct exposure or multi-tenancy creates a real need.

### Network boundary

Use default-deny ingress and permit API-server or control-plane traffic where
the environment exposes a stable way to express it. NetworkPolicy is defense
in depth, not a portable proof that only the API server can connect. Do not
create a NodePort, LoadBalancer, or Ingress by default.

R7 remains open: direct connections bypass proxy authentication. Qualify
reachability from actor and other untrusted Pods and document the supported
trust boundary. If it cannot be enforced on a platform, require a reviewed
authenticated backend path or mark that deployment profile unsupported.

### Follow and streaming queries

The Service proxy supports long-running follow requests, so journal-follow and
streaming query endpoints remain available. Clients must nevertheless use
resumable cursors and reconnect after interruption because managed-cluster
gateways and other intermediaries may impose their own limits. A stream is not
the durable evidence record.

### Control path versus bulk path

Use the API-server proxy for bounded queries, follow streams, manifests,
export status, and small previews. Do not carry large evidence archives or
bulk log downloads through the control plane.

`EvidenceExport` returns metadata, integrity information, and a destination
reference. Agents retrieve bulk content through the destination's native
client. For an in-cluster destination on local kind, documented port-forward
is an operator fallback.

Port-forward is not the minimal RBAC path: it generally requires `create` on
`pods/portforward` plus Pod discovery and cannot be restricted to one stable
Pod name. The Service proxy remains preferred for control traffic.

### Optional direct backend access

If agents need raw Prometheus or Loki queries, grant `get` on
`services/proxy` separately for each exact Service name. This grants access to
every GET endpoint exposed by that Service because RBAC cannot constrain proxy
URL paths. Prefer read-only backend configurations; keep restricted or
normalized queries behind the query Service.

## Requirement 18: Authenticated protocol observation

**Slice:** M0.7. **Status:** Planned; depends on reopened R3.

Bitcoin RPC observation uses separately restricted observation credentials from
Requirement 12, with exact-name Secret access in enrolled namespaces. Verify
server-side refusal of mutation RPCs. Unauthenticated Stacks endpoints may
remain directly accessible, with that evidence class explicitly recorded.

Every observation distinguishes trusted Kubernetes identity, authenticated
protocol facts, unauthenticated actor observations, and actor-self-reported
metrics.

## Requirement 19: Reduced observability v1

**Slice:** M0.7. **Status:** Planned.

Do not make the first observability milestone a custom event-storage platform.
Start with:

- structured journal records;
- source and sequence metadata;
- export to one existing durable backend;
- bounded time-window queries;
- honest gaps and coverage;
- Kubernetes Events for immediate action correlation; and
- a small evidence-export controller.

Audit webhooks are optional advanced sources, not a v1 prerequisite.

## Requirement 20: Export success and completeness

**Slice:** M0.7. **Status:** Planned.

Keep transport outcome separate from evidence quality:

```yaml
status:
  phase: Exported
  completeness: Incomplete
  coverage: NoKnownGap
  redactionProfileDigest: sha256:...
```

Operation phases are `Pending`, `Exporting`, `Exported`, and `Failed`.
Completeness is `Complete` or `Incomplete`. Coverage is one of:

- `ContinuouslyAccounted`;
- `NoKnownGap`;
- `Unverifiable`; or
- `Unavailable`.

Only continuously accounted evidence may be marked complete. An export can
succeed operationally while honestly remaining incomplete.

## Requirement 21: Redaction and cursor scope

**Slice:** M0.7. **Status:** Planned.

V1 redacts before durable storage, records the applied profile digest, and
exports only already-redacted records. Refuse export when the requested policy
requires different or stronger processing.

Defer export-time re-redaction, signed custom cursors, and custom tombstone
machinery. Prefer backend-native bounded pagination. If a cursor is exposed,
expiration must not appear as an empty, complete result.

## Requirement 22: Evidence-integrity semantics

**Slice:** M0.7, with constraints applied by every earlier slice. **Status:**
Partially complete; final observation and export contract remains.

Preserve:

- explicit source-coverage classification;
- identity-bound attribution;
- refusal to attribute post-divergence evidence to the original Pod;
- recording the actual Pod UIDs selected by native Chaos Mesh faults;
- durable intent before irreversible RPC calls;
- state inspection rather than blind retry after ambiguity;
- administrator-controlled credential and destination profiles; and
- refusal to turn incomplete evidence into a successful protocol conclusion.

## Requirement 23: Observation examples

**Slice:** M0.7. **Status:** Planned.

Every observation example includes:

- `redactionProfileRef`;
- sampling limits;
- time window;
- selected sources;
- destination profile;
- expected coverage semantics; and
- operation and completeness status examples.

## Requirement 24: Repository layout documentation

**Slice:** M0.8, maintained incrementally. **Status:** In progress.

Describe actual and proposed paths accurately:

```text
apis/network/
operators/network/
operators/observability/
operators/action/        # proposed
contracts/
tools/
charts/
```

List every existing contract fixture. Clearly distinguish implemented code,
approved direction, recommendation, and unresolved decision.

## Requirement 25: Markdown validation

**Slice:** M0.8. **Status:** Local policy exists; repository and CI integration
remain.

Keep `.markdownlint.yaml` authoritative for the VS Code extension and CLI.
Provide an overridable Make command:

- developers may use a local `markdownlint-cli2`;
- CI uses the official image pinned by version and digest;
- `markdown-check` remains separate from `make verify`; and
- CI requires both.

Keep project-specific Go contract tests for repository paths, links, and
architecture constraints. Do not create a second Markdown style ruleset that
can drift from `.markdownlint.yaml`.

## Requirement 26: Instrumented-image support matrix

**Slices:** M0.6 and M0.8. **Status:** Planned.

Packaging and roadmap documents identify which capabilities require standard
Bitcoin Core, standard Stacks images, feature-gated testing images, externally
prepared OCI images, or unavailable upstream hooks. Include transaction-worker
profiles and their funding, nonce, and supported-image assumptions.

Do not allow an unavailable testing hook to silently block an otherwise
independent milestone.

## Requirement 27: Revised implementation roadmap

**Slice:** M0.8, maintained incrementally. **Status:** In progress.

After M0, use this dependency order:

1. Continuous and bounded Bitcoin block production.
2. Protocol-bootstrap primitives and steady transaction demand.
3. Multi-actor topology qualification.
4. Passive journal foundation.
5. Native Chaos Mesh policy integration.
6. Bitcoin lifecycle actions.
7. Evidence export and query.
8. Qualified instrumented protocol actions and agent ergonomics.

### Initial Bitcoin-production slice

The first slice implements mutable `BitcoinBlockProduction` for baseline chain
progress alongside finite `BitcoinBlockGeneration`. Both use the reviewed
credential profiles and target-exclusion contract, while only the finite
resource uses the shared typed action lifecycle, immutable action spec,
conservative CEL bounds, attributable completion, and terminal outcome. It may
defer `ActionSafetyPolicy`; until that policy exists, schema bounds are
absolute. The slice includes the smallest reviewed target-selection profile;
it must not silently collapse a declared multi-target policy to one node.
Baseline packaging must remain useful without enabling bounded actions.

### Primary qualification target

Qualify first on:

```text
kind, three nodes, Docker Desktop, macOS arm64/Apple Silicon
```

Before managed-cluster qualification, verify Chaos Mesh bootstrap,
`TimeChaos`, `IOChaos`, NetworkPolicy behavior, API-server Service proxy, and
host-side agent query/evidence access on that platform.

Managed Kubernetes remains a required compatibility direction, not a reason
to complicate the first local vertical slices.

## M0 acceptance checklist

The slice ledger owns completion. Requirement-level details remain in the
sections above.

- [x] **M0.1:** the API-module spike is implemented, independently reviewed,
      and passes its module, generation, container, and dependency checks.
- [x] **M0.2:** the shared action lifecycle, vocabulary, immutability,
      time-bound behavior, and anti-orchestration contracts are frozen.
- [ ] **M0.3:** finite Bitcoin generation and reorganization have bounded,
      attributable, credentialed, serialized, implementation-ready designs.
- [ ] **M0.4:** multi-target baseline production, policy updates, finite cadence,
      bounded status, action priority, and safe exclusion are reconciled.
- [ ] **M0.5:** static Chaos Mesh admission and initial native-fault/platform
      qualification require no mandatory webhook.
- [ ] **M0.6:** protocol bootstrap, steady transaction demand, overrides, and the
      instrumented-image support matrix are implementation-ready.
- [ ] **M0.7:** agent access, passive observation, journal, query, export,
      completeness, coverage, redaction, and evidence integrity agree across
      documents and examples.
- [ ] **M0.8:** repository layout, fixtures, verification targets, packaging,
      support matrix, roadmap, and design index match the repository.
- [ ] **M0 closeout:** R1–R8 have reviewed resolutions or explicit per-capability
      deferrals, with implementation acceptance defined for supported
      capabilities; links, Markdown policy, repository contracts, `make verify`,
      and relevant container checks pass on the complete package.
