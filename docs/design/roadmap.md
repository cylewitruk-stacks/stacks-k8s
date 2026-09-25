# Implementation roadmap

## Authority and planning rules

The [M0 remediation plan](m0-remediation-plan.md) owns delivery status and
requirements. It adopts [Steady-state operation](steady-state-operation.md);
the R1–R8 ledger identifies review gates. This roadmap follows its revised
post-M0 order and supersedes the earlier journal-first sequence.

- Ship independently useful capabilities with explicit dependencies.
- Keep ordinary regtest operation available without agents, bounded actions,
  Chaos Mesh, or observability.
- Let capability controllers maintain declared protocol prerequisites; external clients sequence investigations.
- Qualify reliability, liveness, resilience, performance/efficiency, and
  defensive investigation use cases; do not claim deterministic execution.
- Update schemas, examples, permissions, operations, compatibility, and release
  documentation with each implemented capability.

## Dependency order

```text
M0: reviewed API, identity, execution, and packaging contracts
  -> M1: baseline and bounded Bitcoin production
  -> M2: bootstrap primitives and steady transaction demand
  -> M3: multi-actor and independent-upgrade qualification
  -> M4: passive journal and initial telemetry
  -> M5: native Chaos Mesh integration
  -> M6: remaining Bitcoin lifecycle actions
  -> M7: evidence export and query
  -> M8: qualified instrumented actions and agent ergonomics
```

This is a delivery order, not a controller workflow or mandatory runtime chain.
Observation consumes lifecycle facts without becoming a mutation dependency.
Each milestone may use a narrower reviewed capability profile, but must reject
unsupported requests explicitly.

## M0: close contracts and remediation

**Outcome:** implementation-ready contracts without unresolved assumptions
hidden in code.

M0.1 shared APIs and M0.2 common action vocabulary remain complete. M0.3 and
M0.4 have reviewed initial contracts and implementations for independent target
admission, multi-target cadence, static authority separation, and bounded
cleanup. Broader recovery and credential profiles remain explicitly deferred.

M0.5 uses [native Chaos Mesh faults](../chaos/operations.md) directly; broader
kind/platform effect qualification remains open. M0.6 covers bootstrap, transaction
demand and actor capabilities; M0.7 covers observability/access; M0.8 covers
packaging/release alignment.
The final consistency/security/roadmap audit is **M0 closeout**, not M0.9.

**Done:** affected schemas, ownership, bounds, status, endpoint identity,
execution recovery, and packaging are reviewed. Each R1–R8 item is resolved or
explicitly deferred for an unavailable capability; no enabled feature depends
on an unresolved gate.

M0 resolves designs and specifies acceptance. M1–M8 implement and validate
those resolutions; design completion does not claim runtime qualification.
The [initial delivery scope](m0-remediation-plan.md#initial-delivery-scope)
permits the implemented R1 foundation and constrained Bitcoin baseline while
other M0 work remains open. Publication alone enables no RPC execution; the
[initial baseline profile](../network-operator/bitcoin-production.md) defines
the scoped mutation worker and its limitations.

## M1: baseline and bounded Bitcoin production

**Current delivery:** weighted multi-target baseline supports fixed or bounded
jittered cadence, mutable policy, static RPC separation, and durable response-loss
exclusion. Optional [finite generation](../network-operator/bitcoin-generation.md)
supports immediate, fixed, uniform, and explicit-delay cadence through the
independent action operator and the network worker's per-target executor.
Broader recovery/compatibility qualification remains pending. M1 is not complete.

**Outcome:** an ordinary regtest network advances Bitcoin through a declared
baseline, with finite generation separately available.

Deliver baseline production first under the trusted disposable-network profile,
then finite generation with its additional lifecycle gates. Static credentials
and basic authority separation suffice initially. Per-process credential
rotation, cryptographic process attestation, reset/readmission, and an
execution-aware adapter are deferred capabilities, not M1 prerequisites.

- Qualify credential/config profiles, leaf/inventory identity vectors, and
  rollout semantics across supported Bitcoin targets.
- Compile owned baseline policy from `StacksNetwork`. Standalone capability
  support is conditional on reviewed ownership, overlap, and authorization.
- Implement the reviewed timing/target-selection profile without silently
  changing weights or substituting targets.
- Implement finite `BitcoinBlockGeneration` with immutable bounded specs.
- Implement and validate the M0 resolutions of R1–R3: independent target/endpoint
  admission, outstanding-RPC exclusion and quiescence, and separated RPC authority.
- Exercise primary-object finalizers through actual RBAC (R5).
- Keep bounded status and structured mechanism facts available without a
  journal; do not infer acknowledged effects from observed chain tips.

**Done:** baseline production runs with actions/observation/Chaos disabled,
continues across operator restarts that preserve its worker processes,
and progresses on a valid target while
unrelated actors are unready. Ambiguous server work cannot authorize unsafe
takeover, retry, or resumption.
Worker termination requires receipt drainage; grace-period exhaustion, crashes
and lost receipts may leave a target closed.
Unresolved targets remain closed across restarts/replacement; fresh isolated
environments are the initial recovery option. Measure that limitation in real
use before prioritizing in-place recovery.

## M2: bootstrap primitives and steady transaction demand

**Current delivery:** the [initial Stacks profile](../network-operator/stacks-production.md)
implements managed direct PoX-4/PoX-5 initialization/renewal and an isolated
one-account transfer worker. The [PoX-5 profile](../network-operator/pox5.md)
uses real sBTC contracts with explicit test registry initialization. Native
inclusion, pause/update, backpressure and surviving-process receipt retention
are implemented; bound Stacks-worker loss is a terminal experiment failure. Multi-profile
compatibility, overrides,
and wider rotation/revocation remain open; M2 is not complete.

**Outcome:** supported Stacks images reach productive operation with explicit
bootstrap inputs and ongoing offered traffic.

- Expose separate controller-managed desired-state capabilities for
  funding, coinbase maturity, signer authorization/registration, and reward
  activation. Controllers reconcile these prerequisites from declared state.
- Implement the reviewed steady transaction producer capability, provisionally
  `StacksTransactionProduction`, through owned baseline declarations.
- Keep per-worker nonce ownership and bounded in-flight work; permit shared-key experiments
  without cross-worker coordination. Validate funding assumptions,
  backpressure, and explicit ambiguous submission.
- Validate the M0.6 signing-authority contract, including credential isolation
  and rotation/revocation for designated transaction workers.
- Keep Stacks miner behavior in `StacksNode` configuration and qualify
  proposal behavior against concrete image versions.
- Define pause/update and latest-baseline restoration without stale snapshots.

**Done:** a documented ordinary-use profile produces Bitcoin progress,
transaction demand, and observed Stacks progress without a chaos agent or
observation operator. Stalled bootstrap and submission failure remain
distinguishable; the aggregate compiles resources and never becomes an experiment planner.

## M3: multi-actor topology and independent upgrades

**Current qualification:** one [full-cohort lifecycle sequence](../network-operator/public-api-qualification.md#full-cohort-lifecycle-and-image-roll--2026-09-13)
passed with two fresh-storage follower joins, an independent follower image roll,
suspend/resume, removal, renewal and network teardown. Other actor-role upgrades,
image pairs and late joins after fault/action sequences remain open; M3 is not complete.

**Outcome:** reusable heterogeneous networks preserve identity and storage
across supported changes.

- Qualify multiple Bitcoin targets, multiple Stacks miners, followers, and
  transaction ingress through appropriate nodes.
- Exercise add/remove/roll/suspend and baseline policy updates.
- Add reviewed ConfigMap watching and credential rotation semantics.
- Qualify persistent-data compatibility across selected real image pairs.
- Publish aggregate editor/viewer Roles and constrain or explicitly trust
  delegated workload authority (R6).
- Decide `SbtcSigner` only from distinct executable/lifecycle requirements.

**Done:** [Topology](topology.md) acceptance passes, with current target
admission distinct from complete aggregate inventory and protocol progress.

## M4: passive journal and initial telemetry

**Current delivery:** [continuous GreptimeDB telemetry](../observability/README.md)
provides opt-in recording workloads, public Kubernetes facts, actor logs, native
node/signer metrics, capture gaps and indexed HTTP SQL queries. Metrics listeners
are enforced in generated and private rendered actor configurations. M4 remains
open for broader lifecycle/coverage qualification and additional protocol sources;
MCP is low priority.

**Outcome:** retain a truthful recent record in an existing durable backend.

- Implement the reviewed `NetworkTelemetry` surface, source metadata,
  pre-storage redaction, bounded retention, and honest gaps.
- Capture topology, baseline, action, Event, and configured protocol facts.
- Support optional-CRD discovery without startup dependency on action/Chaos APIs.
- Prove Kubernetes and RPC observation authority cannot mutate the environment.
- Keep audit webhooks optional; watches never promise every intermediate write.
- Enable Prometheus exports on every actor profile whose binary supports them,
  independently of collector installation. Verify any environment-variable override
  against the pinned binary; otherwise set the listener through the structured TOML
  renderer. Define enforcement for complete user-supplied configurations, expose
  discoverable endpoints, and qualify real samples with network/participant/Pod
  identity. Do not advertise unsupported native metrics. The
  [continuous telemetry profile](../observability/README.md) informs this work.

**Done:** rollout, baseline updates, and independently created resources remain
correlated through restart/source outage, with bounded CRD status and explicit
coverage.

## M5: native Chaos Mesh integration

**Outcome:** clients directly use qualified upstream fault resources.

- Pin and qualify the initial platform matrix; install upstream Chaos Mesh separately.
- Let the agent use native faults under its granted Kubernetes authority.
- Document UID-scoped actor targeting and network correlation labels.
- Record selected Pod identity and replacement without claiming UID-pinned
  native targeting.
- Qualify protocol partitions separately from production control-path failure.
- Test injection, expiry, cancellation, recovery, and telemetry gaps.
- Qualify native `TimeChaos` before accepting a custom clock-control fallback.

**Done:** [Chaos Mesh](chaos-mesh.md) operates without wrapper APIs or a baseline
dependency. Continuity claims state their traffic and platform assumptions;
each further fault kind needs effect and recovery evidence.

## M6: remaining Bitcoin lifecycle actions

**Current delivery:** the [initial local profile](bitcoin-reorganization.md)
implements bounded suffix replacement, acknowledged compensation and retained
RPC uncertainty. Wider lifecycle qualification remains open.

**Outcome:** bounded Bitcoin lifecycle mechanisms compose with baseline
production under the reviewed exclusion contract.

Broader `BitcoinReorganization` profiles require corresponding R4 invalidation-marker
cleanup for every exit path and R2 coverage of server work across takeover/recovery.
Retain conservative bounds and fail-closed protocol-boundary handling until a
trusted schedule source is designed. Passive observations report branch facts
without claiming causal attribution or deterministic results.

**Done:** lifecycle, ambiguity, deletion, timeout, restart, target replacement,
and cleanup acceptance in [Bitcoin lifecycle](bitcoin-lifecycle.md) passes.
A resource's successful mechanism does not assert a protocol outcome.

## M7: evidence export and agent query

**Outcome:** clients inspect and preserve bounded retained facts.

Available tools include [portable telemetry export](../../tools/stacks-evidence/README.md)
and [bounded native protocol snapshots](../../tools/stacks-inspect/README.md).
Their coverage and identity limits are explicit; they do not classify causes.

- Implement read-only HTTP GET queries through the API-server Service proxy.
- Qualify direct backend reachability and actor trust, or require a reviewed
  authenticated backend path (R7).
- Prefer backend-native pagination; report expired/pruned cursors honestly.
- Export already-redacted evidence and content-integrity manifests to approved
  destinations; keep bulk archives off the API-server proxy.
- Separate `Exported` operation status from evidence completeness and coverage.

**Done:** clients verify an evidence window and distinguish transfer failure,
capture gaps, and source absence without a replay or diagnosis endpoint.

## M8: qualified instrumented actions and agent ergonomics

**Outcome:** supported capabilities are usable for ordinary testing and
authorized investigation with clear limits.

[Read-only preflight](../../tools/stacks-preflight/README.md) checks selected
network, recording, operator-scope, namespace-enrollment and backend prerequisites.
It does not authorize a fault or establish protocol health; the external experimenter
still chooses interventions and verifies their outcomes.

Enable each instrumented action only after its bounded mechanism, image
capability, expiry, credentials, and cleanup are reviewed. Clock/storage
fallbacks require a demonstrated native-platform gap. Unavailable hooks must
not block independent baseline or observation releases.

Validate discovery, dry-run, watch recovery, stable reasons, overrides,
concurrent capabilities, upgrades, storage recovery, and supply-chain checks.
M0.8 defines R8's ordinary regtest, reliability/liveness/resilience,
performance/efficiency, and reported-behavior verification acceptance tasks;
M8 completes their cross-product qualification.
Verification records observed behavior and evidence limits, not guaranteed
reproduction.

**Done:** a supported client can change baseline policy, submit independent
actions, inspect facts, and export evidence through documented interfaces.
Publish independently versioned artifacts and a tested compatibility matrix.

## Cross-cutting release checks

Every enabled capability needs structural schemas/CEL, meaningful controller
and envtest coverage, exact rendered RBAC, admission negative tests, and
proportional real-cluster qualification. Cover deletion, identity drift,
restart, ambiguity, storage/telemetry failure, and configuration migration.
Publish API/operations documentation and signed/scanned artifacts where released.

First qualify three-node kind on Docker Desktop, macOS arm64/Apple Silicon.
Managed Kubernetes remains a required direction, with explicit CNI, storage,
proxy, actor-image, and native-fault qualification before support claims.

## Deferred capabilities

### Reusable chainstate snapshots

**Optional future slice:** reduce repeated bootstrap and synchronization time by
capturing a network's Bitcoin and Stacks chainstate for use by fresh networks.
An illustrative `StacksChainstateSnapshot` resource would request bounded capture;
a future `StacksNetwork` could select the resulting immutable artifact at creation.
Names and schemas require design review. This is not required for M0–M8 completion.

- Define coordinated quiescence and application-consistent capture. Cooperative
  pause alone does not prove that actors stopped writing. Evaluate clean shutdown
  and file export versus supported storage snapshots, including the effect on
  the source network's ability to resume.
- Record per-actor chain tips/branches, Bitcoin–Stacks correspondence, genesis,
  epoch schedule, image/database-format compatibility and artifact checksums.
  Include the necessary signer/protocol state; chain databases alone may not
  constitute a usable checkpoint. Define account/key requirements separately
  from public artifact metadata.
- Retain artifacts independently of the source network. Restore into fresh
  volumes and new network/participant identities; never copy live worker bindings
  or blindly replay completed bootstrap transactions. Define worker nonce and
  protocol initialization against restored state without relaxing the existing
  worker-crash contract.
- Keep capture/restore in an explicit mutating capability, separate from passive
  observability. Qualify restoring one artifact into two independent networks,
  continued production and late-node synchronization; reject incompatible inputs
  before starting workloads. Reuse does not promise deterministic experiment results.

### Other deferred work

Autonomous node-local Bitcoin production, hard aggregate action accounting,
advanced native faults, and unavailable instrumented-image hooks need separate
contracts. External image building, investigation planning, replay, reduction,
and diagnosis remain outside operators. Runtime determinism is not a goal.

## Composable runtime delivery

The `v1alpha2` implementation replaces the network/action runtime and external
bootstrap workflow with explicit reusable inputs and network-owned participants.
Go workers perform ongoing production, initialization and renewal. The
[current-state inventory](current-state.md) and [public API](public-api/README.md)
define delivered behavior; broader action, telemetry/export and release matrices
remain the milestones above. A successful local cohort does not close arbitrary
image, placement or fault compatibility requirements.
