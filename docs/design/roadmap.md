# Implementation roadmap

## Authority and planning rules

The [M0 remediation plan](m0-remediation-plan.md) owns delivery status and
requirements. It adopts [Steady-state operation](steady-state-operation.md);
the R1–R8 ledger identifies review gates. This roadmap follows its revised
post-M0 order and supersedes the earlier journal-first sequence.

- Ship independently useful capabilities with explicit dependencies.
- Keep ordinary regtest operation available without agents, bounded actions,
  Chaos Mesh, or observability.
- Let external clients sequence bootstrap and investigations.
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
  -> M5: native Chaos Mesh profile
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
M0.4 are reopened for independent target admission, multi-target production,
RPC quiescence, authority separation, and cleanup. Historical Bitcoin fixtures
are marked superseded; passing their tests does not authorize implementation.

M0.5 covers native faults, M0.6 bootstrap/transaction demand and actor
capabilities, M0.7 observability/access, and M0.8 packaging/release alignment.
The final consistency/security/roadmap audit is **M0 closeout**, not M0.9.

**Done:** affected schemas, ownership, bounds, status, endpoint identity,
execution recovery, and packaging are reviewed. Each R1–R8 item is resolved or
explicitly deferred for an unavailable capability; no enabled feature depends
on an unresolved gate.

M0 resolves designs and specifies acceptance. M1–M8 implement and validate
those resolutions; design completion does not claim runtime qualification.
The [initial delivery scope](m0-remediation-plan.md#initial-delivery-scope)
permits R1 declaration publication as the next repository foundation while
other M0 work remains open. Freeze its additive schema and test its publication
semantics before landing it; publication alone enables no RPC execution.

## M1: baseline and bounded Bitcoin production

**Outcome:** an ordinary regtest network advances Bitcoin through a declared
baseline, with finite generation separately available.

Deliver baseline production first under the trusted disposable-network profile,
then finite generation with its additional lifecycle gates. Static credentials
and basic authority separation suffice initially. Per-process credential
rotation, cryptographic process attestation, reset/readmission, and an
execution-aware adapter are deferred capabilities, not M1 prerequisites.

- Implement neutral Bitcoin-node API migration and required credential/config
  profiles, including leaf/inventory identity vectors and rollout semantics.
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
resumes after a successful [graceful controller drain](target-admission-and-rpc-execution.md#deadlines-and-receipt-collection),
and progresses on a valid target while
unrelated actors are unready. Ambiguous server work cannot authorize unsafe
takeover, retry, or resumption.
Grace-period exhaustion, crashes, and lost receipts are outside successful
drain and may leave a target closed, including during a routine rollout.
Unresolved targets remain closed across restarts/replacement; fresh isolated
environments are the initial recovery option. Measure that limitation in real
use before prioritizing in-place recovery.

## M2: bootstrap primitives and steady transaction demand

**Outcome:** supported Stacks images reach productive operation with explicit
bootstrap inputs and ongoing offered traffic.

- Expose reviewed bounded bootstrap primitives or an external helper for
  funding, coinbase maturity, signer authorization/registration, and reward
  activation. The external client sequences them.
- Implement the reviewed steady transaction producer capability, provisionally
  `StacksTransactionProduction`, through owned baseline declarations.
- Enforce account/nonce ownership, bounded in-flight work, funding assumptions,
  backpressure, and explicit ambiguous submission.
- Validate the M0.6 signing-authority contract, including credential isolation
  and rotation/revocation for designated transaction workers.
- Keep Stacks miner behavior in `StacksNode` configuration and qualify
  proposal behavior against concrete image versions.
- Define pause/update and latest-baseline restoration without stale snapshots.

**Done:** a documented ordinary-use profile produces Bitcoin progress,
transaction demand, and observed Stacks progress without a chaos agent or
observation operator. Stalled bootstrap and submission failure remain
distinguishable; no controller becomes a bootstrap workflow.

## M3: multi-actor topology and independent upgrades

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

**Outcome:** retain a truthful recent record in an existing durable backend.

- Implement the reviewed `NetworkTelemetry` surface, source metadata,
  pre-storage redaction, bounded retention, and honest gaps.
- Capture topology, baseline, action, Event, and configured protocol facts.
- Support optional-CRD discovery without startup dependency on action/Chaos APIs.
- Prove Kubernetes and RPC observation authority cannot mutate the environment.
- Keep audit webhooks optional; watches never promise every intermediate write.

**Done:** rollout, baseline updates, and independently created resources remain
correlated through restart/source outage, with bounded CRD status and explicit
coverage.

## M5: native Chaos Mesh profile

**Outcome:** clients directly use qualified upstream fault resources.

- Pin and qualify the initial platform matrix and static admission profile.
- Bound namespace/selector/duration scope; deny Workflow/Schedule.
- Record selected Pod identity and replacement without claiming UID-pinned
  native targeting.
- Qualify protocol partitions separately from production control-path failure.
- Test injection, expiry, cancellation, recovery, and telemetry gaps.
- Qualify native `TimeChaos` before accepting a custom clock-control fallback.

**Done:** [Chaos Mesh](chaos-mesh.md) acceptance passes without wrapper APIs or a
baseline dependency on Chaos Mesh. Continuity claims state their traffic and
platform assumptions.

## M6: remaining Bitcoin lifecycle actions

**Outcome:** bounded Bitcoin lifecycle mechanisms compose with baseline
production under the reviewed exclusion contract.

Implement `BitcoinReorganization` only after R4 defines invalidation-marker
cleanup for every exit path and R2 covers server work across takeover/recovery.
Retain conservative bounds and fail-closed protocol-boundary handling until a
trusted schedule source is designed. Passive observations report branch facts
without claiming causal attribution or deterministic results.

**Done:** lifecycle, ambiguity, deletion, timeout, restart, target replacement,
and cleanup acceptance in [Bitcoin lifecycle](bitcoin-lifecycle.md) passes.
A resource's successful mechanism does not assert a protocol outcome.

## M7: evidence export and agent query

**Outcome:** clients inspect and preserve bounded retained facts.

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

Autonomous node-local Bitcoin production, hard aggregate action accounting,
advanced native faults, and unavailable instrumented-image hooks need separate
contracts. External image building, investigation planning, replay, reduction,
and diagnosis remain outside operators. Runtime determinism is not a goal.
