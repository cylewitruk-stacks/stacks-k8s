# M0 architecture remediation plan

## Purpose

This document consolidates the approved remediation decisions from the first
review of the architecture design package. It is the authoritative M0 plan.
Where it conflicts with another document under `docs/design/`, this plan takes
precedence until that document is revised.

M0 changes design, packaging, and repository foundations. It does not add an
in-cluster experiment planner. The external agent remains the sole
orchestration and reasoning layer.

## Outcome

M0 is complete when:

- the shared network API-module spike has passed or has been rejected with a
  recorded technical reason;
- action APIs share one vocabulary and one typed lifecycle contract;
- Bitcoin generation, credentials, admission, and serialization have
  implementation-ready designs;
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

## Execution order

M0 proceeds in this order:

1. Run the shared network API-module spike.
2. Reconcile API vocabulary, action lifecycle, and immutable-field rules.
3. Resolve Bitcoin generation, serialization, credentials, and attribution.
4. Revise Chaos Mesh admission and observability access designs.
5. Add the missing bootstrap and actor-capability designs.
6. Align packaging, verification, examples, and the implementation roadmap.

## 1. Shared network API-module spike

**Implementation status:** implemented, locally verified, and independently
reviewed in the current M0 spike candidate.

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

## 2. Typed shared action lifecycle

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

## 3. Immutable action specifications

Action-defining fields are immutable from creation. Enforce this with CEL
transition rules such as `self == oldSelf`.

Do not make mutability depend on `status.phase`; spec-scoped CEL cannot safely
express that rule, and a webhook would add an avoidable Pending-to-Admitted
race. A materially different action requires a new resource.

## 4. Common action vocabulary

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

## 5. Structural anti-orchestration tests

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

## 6. Static Chaos Mesh admission first

The v1 ValidatingAdmissionPolicy enforces only static constraints:

- label-based selectors;
- request-namespace confinement;
- enrolled namespaces;
- bounded duration and target count; and
- no raw Pod-name or expression selectors; and
- no remote-cluster targeting.

Package the policy behind an explicit, disabled-by-default chart value. Its
installation requires Chaos Mesh to be installed or explicitly declared as an
external dependency. Match only namespaces enrolled for stacks-k8s use.

## 7. Defer dynamic enrollment admission

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

## 8. Action safety bounds

Each numeric bound must be classified as either:

- a fixed OpenAPI or CEL limit; or
- an administrator-configurable per-kind `ActionSafetyPolicy` limit.

If policy elevation is supported, the policy needs explicit per-kind fields
for offsets, rates, block counts, reorganization depths, and comparable
parameters. Until that policy exists, conservative CRD bounds are absolute.

Cross-kind aggregate admission remains deferred because it would introduce a
coordination layer over otherwise independent actions.

## 9. Consolidated Bitcoin generation API

**Design status:** completed by M0.3 and pinned by
[`bitcoin-actions-v1.json`](../../contracts/bitcoin-actions-v1.json) plus the
[Bitcoin lifecycle design](bitcoin-lifecycle.md).

Replace `BitcoinMiningWindow` and `BitcoinBlockRequest` with one bounded action,
named `BitcoinBlockGeneration`.

Its immutable spec covers:

- Bitcoin node reference;
- block count;
- interval;
- batch size, fixed to one when cadence is present;
- an explicit regtest destination address;
- timeout; and
- administrator-selected safety-policy identity in status when that policy
  exists.

Its status records admitted identity, persisted RPC intents, returned or
candidate block hashes, progress, attribution, and terminal outcome. Bounded
`generatetoaddress` batches may run between status checkpoints; one API-server
write per block cannot support immediate generation efficiently.

## 10. Attributable Bitcoin generation

Do not infer action ownership solely from the observed chain tip in a
multi-miner network. Only block hashes returned by a successful mutation RPC
support completed attribution. A dedicated destination improves diagnosis but
is not a correctness or admission invariant.

After a lost mutation response, wait for its recorded deadline and inspect
every known tip. Proven absence may retry within the original bounds; any
observed or uncertain effect is `Ambiguous` and the action is `Inconclusive`.
Never promote reconstructed hashes to acknowledged success or retry the
mutation blindly.

## 11. Bitcoin action serialization

V1 runs every Bitcoin action controller in one leader-elected process, but
leader election is not a per-target fencing mechanism. Each target therefore
uses an API-server-persisted node-scoped Lease
whose holder identity binds the action UID to a unique controller acquisition
token. The token distinguishes an old leader from a replacement reconciling
the same action.

Use:

- optimistic-concurrency acquisition and release of the shared reservation;
- an optional shared keyed mutex across all Bitcoin action kinds to reduce
  same-process contention;
- an uncached read confirming the reservation holder immediately before each
  material RPC batch;
- cancellation of the bounded RPC context on leader or reservation loss;
- action-status intent persisted before the bounded RPC batch;
- bounded per-RPC and per-batch deadlines; and
- mandatory chain-state revalidation after restart, reservation loss, or
  ambiguity.

A shared leader-gated `ReservationManager` Runnable owns every active handle
across Bitcoin action kinds. It renews independently of reconcile workqueues,
cancels handle-derived RPC contexts on reservation or leader loss, and routes
every Lease renewal, RPC-deadline update, and release through the handle's
serialized writer. Worker count and queue latency are not safety properties.

Expiry follows client-go's observed-record pattern: another holder is eligible
for replacement only after the same Lease record remains unchanged for a full
local Lease duration. Holder-written timestamps never permit an earlier
takeover.

The reservation spans requeues while an action remains active. Its renewal,
duration, loss, recovery, and administrative-release rules must ensure a new
holder cannot begin before the prior holder's bounded RPC context has been
cancelled or expired. Because Bitcoin RPC has no fencing token, ambiguous
handoff becomes `Inconclusive`; the controller never claims exactly-once
execution.

Restart and loss behavior is explicit:

- while another acquisition token holds the reservation, the controller makes
  no RPC call;
- with no durable intent or side effect, an action may remain `Pending` with
  `TargetBusy` and reacquire before its expiry;
- after recorded intent, the controller inspects every known tip; proven
  absence may return to bounded admission, while any observed or uncertain
  effect becomes `Inconclusive`;
- if another holder acquired the reservation after this action produced a side
  effect, this action never resumes mutation and ends `Inconclusive` after
  recording attributable state; and
- an expired but otherwise unclaimed reservation may be reacquired only after
  the prior maximum RPC deadline has elapsed and live chain state is
  revalidated.

The protocol is shared across action kinds and remains valid if controllers
later split into independent processes. M0.3 freezes its naming, 30-second
lease, 10-second renewal and RPC limits, status representation, and release
preconditions. The implementation must add leader-turnover,
same-action/new-token, stale-holder, saturated-workqueue, handle-write
serialization, and competing-action tests.

## 12. Bitcoin credential and configuration identity

### Provisioning

Use one fixed-name credential Secret in each network namespace. A helper:

- generates high-entropy credentials;
- derives a salted Bitcoin Core `rpcauth` verifier so plaintext credentials
  never enter `bitcoin.conf`;
- creates the Secret with `immutable: true`;
- computes the canonical content digest;
- prints only the digest; and
- never passes credentials through stdout, logs, or Helm values.

The digest is public metadata and therefore must not commit to guessable
credentials.

### Actor startup

Reference the Secret through the existing `ConfigObjectRef.expectedDigest`
path. Actor startup:

1. mounts the Secret;
2. verifies its bytes against `expectedDigest`;
3. renders only the `rpcauth` verifier into Bitcoin configuration and the
   matching client values into Stacks configuration in ephemeral volumes; and
4. refuses to start after any mismatch or render failure.

The centralized action and observation controllers use those client values.
Bitcoin Core cookie authentication remains preferable for same-Pod clients,
but sharing its restart-scoped file with separate controller Pods would require
a broader filesystem or helper trust boundary and is not the v1 design.

The topology prerequisite adds optional `StacksNetwork.spec.bitcoinRPCAuth`
and compiled `BitcoinNode.spec.bitcoinRPCAuth` and
`StacksNode.spec.bitcoinRPCAuth` fields using the constrained
`ConfigObjectRef`. Managed action RPC requires the fixed name and key plus a
non-empty expected digest. New `bitcoin-regtest/v2` and
`nakamoto-regtest-node/v2` profiles consume the input; v1 profiles remain
available but are not eligible for managed action RPC. The change updates
`leaf-spec-v1.json` and `inventory-v1.json` vectors before the first action
controller is enabled.

The network operator references the Secret but has no Secret-read permission.
Action and observation controllers receive exact-name `get` through a
RoleBinding in each enrolled network namespace. This is a narrow, documented
trust-boundary exception; arbitrary Secret names remain inaccessible.

### Identity and rotation

Inventory records configuration-input identity through the compiled leaf
`specDigest`: template, credential reference, expected digest, and renderer
contract. It does not claim to attest the final rendered TOML bytes.

Rotation replaces the immutable fixed-name Secret, updates `expectedDigest`,
and causes a controlled actor rollout. Controllers fail closed during the
availability interruption.

## 13. Time-bound semantics

A controller cannot act while unavailable. `spec.timeout` still advances from
the API-server creation timestamp. On restart the controller must refuse to
resume an expired action, inspect ambiguous state, and avoid blind retries.

Actor-side expiry is reserved for effects that genuinely continue without the
controller, such as an activated testing hook with its own absolute expiry.

## 14. Protocol-bootstrap design

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
sequences them. Do not build an in-cluster bootstrap workflow.

## 15. Actor testing-capability design

Add a design for instrumented Stacks images covering capability discovery,
activation, bounded parameters, expiry, query, revocation, authentication,
production-build isolation, evidence classification, and unsupported-image
behavior.

Treat this as a cross-repository dependency. Actions that require unavailable
hooks remain conditional and fail before mutation.

## 16. Qualify native TimeChaos first

Qualify native Chaos Mesh `TimeChaos` against the required Stacks behavior
before designing `ApplicationClockOffset`. Build a custom action only when the
qualification establishes a concrete semantic gap.

## 17. Agent access and query authentication

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

## 18. Authenticated protocol observation

Bitcoin RPC observation uses the fixed-name credential Secret from section 12.
The observation controller may read only that exact Secret name in enrolled
namespaces. Unauthenticated Stacks endpoints may remain directly accessible.

Every observation distinguishes trusted Kubernetes identity, authenticated
protocol facts, unauthenticated actor observations, and actor-self-reported
metrics.

## 19. Reduced observability v1

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

## 20. Export success and completeness

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

## 21. Redaction and cursor scope

V1 redacts before durable storage, records the applied profile digest, and
exports only already-redacted records. Refuse export when the requested policy
requires different or stronger processing.

Defer export-time re-redaction, signed custom cursors, and custom tombstone
machinery. Prefer backend-native bounded pagination. If a cursor is exposed,
expiration must not appear as an empty, complete result.

## 22. Evidence-integrity semantics

Preserve:

- explicit source-coverage classification;
- identity-bound attribution;
- refusal to attribute post-divergence evidence to the original Pod;
- recording the actual Pod UIDs selected by native Chaos Mesh faults;
- durable intent before irreversible RPC calls;
- state inspection rather than blind retry after ambiguity;
- administrator-controlled credential and destination profiles; and
- refusal to turn incomplete evidence into a successful protocol conclusion.

## 23. Observation examples

Every observation example includes:

- `redactionProfileRef`;
- sampling limits;
- time window;
- selected sources;
- destination profile;
- expected coverage semantics; and
- operation and completeness status examples.

## 24. Repository layout documentation

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

## 25. Markdown validation

Keep `.markdownlint.yaml` authoritative for the VS Code extension and CLI.
Provide an overridable Make command:

- developers may use a local `markdownlint-cli2`;
- CI uses the official image pinned by version and digest;
- `markdown-check` remains separate from `make verify`; and
- CI requires both.

Keep project-specific Go contract tests for repository paths, links, and
architecture constraints. Do not create a second Markdown style ruleset that
can drift from `.markdownlint.yaml`.

## 26. Instrumented-image support matrix

Packaging and roadmap documents identify which capabilities require standard
Bitcoin Core, standard Stacks images, feature-gated testing images, externally
prepared OCI images, or unavailable upstream hooks.

Do not allow an unavailable testing hook to silently block an otherwise
independent milestone.

## 27. Revised implementation roadmap

After M0, use this dependency order:

1. Minimal Bitcoin block generation.
2. Protocol-bootstrap primitives.
3. Multi-actor topology qualification.
4. Passive journal foundation.
5. Native Chaos Mesh policy integration.
6. Bitcoin lifecycle actions.
7. Evidence export and query.
8. Instrumented protocol actions.

### Minimal Bitcoin-generation slice

The first slice includes the shared typed lifecycle, immutable action spec,
conservative CEL bounds, API-server-persisted per-node serialization,
attributable generation, and evidence correlation. It may defer
`ActionSafetyPolicy`; until that policy exists, schema bounds are absolute.

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

- [x] API-module spike has a recorded result and all exit checks pass or the
      design records why it was rejected.
- [x] All action documents use the shared lifecycle, vocabulary, and immutable
      specification rules.
- [x] The shared fixture and repository test pin forbidden orchestration fields;
      generated-schema tests are explicitly required with the first action CRD.
- [x] Bitcoin generation is one attributable, bounded action API.
- [x] V1 serialization uses the durable per-node reservation and uncached
      holder validation.
- [x] Credential provisioning, digest verification, trust exceptions, and
      rotation are specified consistently.
- [ ] Static Chaos Mesh admission has no mandatory webhook dependency.
- [ ] Bootstrap and actor testing-capability documents exist.
- [ ] Agent access uses Kubernetes identity and separates control traffic from
      bulk evidence transfer.
- [ ] Observation phases, completeness, coverage, redaction, and credentials
      are consistent across documents and examples.
- [ ] Layout, fixtures, verification targets, support matrix, and roadmap match
      the repository.
- [ ] Links, Markdown policy, repository contracts, `make verify`, and relevant
      container checks pass.
