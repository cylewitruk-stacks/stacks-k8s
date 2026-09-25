# Atomic action contract

## Status and authority

This document is the normative lifecycle and API contract for custom
`actions.stacks.org/v1alpha1` resources. The first served kind is
[`BitcoinBlockGeneration`](../network-operator/bitcoin-generation.md).
[`BitcoinReorganization`](bitcoin-reorganization.md) adds the explicitly
compensated irreversible profile. Each implemented kind retains its own typed spec, controller package,
RBAC slice, example, and reference page.

Native Chaos Mesh resources keep their upstream schemas and status. An agent may
label a fault object with its network UID for journal attribution and a
correlation ID for search; custom action immutability rules do not apply to it.

Long-lived desired-operation resources also remain outside this contract.
In particular, mutable `BitcoinBlockProduction` has its own API group,
lifecycle, and contract. Sharing a typed RPC client or target reservation with
a Bitcoin action does not make production an action.

The machine-readable vocabulary is pinned by
[`action-lifecycle-v1.json`](../../contracts/action-lifecycle-v1.json).
Its contract identifier is `actions.stacks.org/lifecycle/v1alpha1`.

## Purpose

An external agent creates small Kubernetes resources independently. One custom
action resource describes one bounded mechanism against one logical target and
reports only that mechanism's observed lifecycle. The agent chooses ordering,
overlap, follow-up work, interpretation, reproduction attempts, and reduction.

## Non-goals

- Lists, graphs, stages, schedules, dependencies, or scenarios.
- Cross-resource sequencing or automatic follow-up actions.
- Replay, reduction, diagnosis, or regression generation.
- Generic RPC, shell, fixture, or free-form mechanism dispatch.
- Declaring protocol success because a mutation call returned successfully.
- Hiding uncertain effects or cleanup behind a success phase.

## Common resource shape

This example represents a kind whose schema fixes its target as a `StacksNode`:

```yaml
apiVersion: actions.stacks.org/v1alpha1
kind: ExampleAction
metadata:
  name: follower-a-example
  labels:
    actions.stacks.org/correlation-id: investigation-42
spec:
  networkRef:
    name: mixed-network
  stacksNodeRef:
    name: mixed-network-follower-a
  timeout: 2m
status:
  observedGeneration: 1
  phase: Active
  admittedAt: "2026-09-03T12:00:01Z"
  startedAt: "2026-09-03T12:00:02Z"
  expiresAt: "2026-09-03T12:02:00Z"
  correlationID: investigation-42
  admittedNetwork:
    name: mixed-network
    uid: 00000000-0000-0000-0000-000000000000
    observedGeneration: 3
    inventoryDigest: sha256:example
  admittedTarget:
    apiVersion: network.stacks.org/v1alpha1
    kind: StacksNode
    name: mixed-network-follower-a
    uid: 11111111-1111-1111-1111-111111111111
  conditions: []
```

Shared Go types may cover references, admitted identity, time bounds,
conditions, phases, and correlation metadata. Mechanism parameters remain
typed fields on their own CRD. There is no generic action payload or runtime
mechanism registry.

## Reference conventions

- `networkRef` contains only `name` and resolves a `StacksNetwork` in the
  action namespace.
- A kind with one target type uses a typed same-namespace object reference such as
  `bitcoinNodeRef`, `stacksNodeRef`, `signerRef`, or `minerRef`. The reference
  contains the compiled leaf resource name only; its schema fixes API group
  and kind. It resolves that exact resource through an uncached API read.
- A mechanism supporting a closed actor union may use `actorRef` with a
  required enum-constrained leaf `kind` and resource `name`. It never accepts
  arbitrary API groups, resources, namespaces, selectors, or expressions.
- Public specs never accept Pod, StatefulSet, Service, or Secret names as
  action targets. Controllers resolve them through admitted topology.
- Safety policy and credential selection are administrator-configured, not
  agent-selected references. Their admitted identities appear in status when
  applicable.

Cross-namespace references are excluded from v1alpha1. Missing, ambiguous,
stale, or mechanism-unready references keep an action `Pending` until expiry or
deletion; they never cause fallback target selection.

The target reference is valid only when the leaf's `spec.networkRef` and
controller owner UID match the resolved `StacksNetwork`. The leaf UID comes
from an uncached read, not an inventory field. Controllers verify the current
target's identity before admission and before every material side effect.

The former requirement to join every action through a complete Ready network
inventory is reopened under R1 in
[Steady-state operation](steady-state-operation.md). Preserve the existing
inventory contract, but define an independent target-admission contract that
does not wait for unrelated actors or progress the action itself supplies.
The exact runtime endpoint binding and admitted identity extension must be
reviewed before the first affected action API is served. The example above
illustrates an admission with a complete inventory; it does not mandate global
readiness for every mechanism.

## Specification immutability and time bounds

The complete `spec` is immutable from creation. Generated CRDs apply the CEL
transition rule `self == oldSelf` to `spec`. Immutability does not depend on
phase or controller availability. A materially different action requires a
new object.

Every custom action has a required, schema-bounded `timeout`. Its wire format
is `kubernetes-duration` over a JSON/YAML `string`, represented by
`metav1.Duration` in Go. The field-level CEL rule
`duration(self) > duration('0s')` rejects zero and negative values; every kind
also supplies a finite upper bound. The generated OpenAPI schema leaves
`format` unset so CEL sees a string rather than a pre-parsed duration. The controller
computes `status.expiresAt` from the API-server `metadata.creationTimestamp`
plus that timeout. Pending time consumes the bound; admission or controller
outage never resets it. Reversible mechanisms may additionally require a
bounded `duration` that cannot exceed the remaining timeout.

Whole-spec equality is safe only over bounded structural schemas. Every
string, list, and map beneath `spec` has an explicit maximum length, item
count, or property count. Specs do not preserve unknown fields or embed
unbounded recursive values.

A mechanism whose side effect can persist while its controller is unavailable
must enforce an absolute expiry outside that controller, no later than
`status.expiresAt`. A kind without such a mechanism is not supported as a
time-bounded reversible action.

Deletion is the cancellation signal. Custom `cancel`, `suspend`, and mutable
deadline fields are not part of action specs.

## Status ownership and timestamps

Only the kind's controller writes status. Common fields are:

| Field | Contract |
| --- | --- |
| `observedGeneration` | Spec generation represented by status and every condition. |
| `phase` | One value from the common phase table. |
| `admittedAt` | First time the exact request and identities were durably admitted. |
| `startedAt` | First attempt to perform the external side effect. |
| `expiresAt` | Immutable absolute bound derived from creation time and `spec.timeout`. |
| `finishedAt` | Time the terminal outcome was first recorded. |
| `correlationID` | Admitted snapshot of the correlation label, if supplied. |
| `admittedNetwork` | Network UID and generation; inventory binding and independent target-admission representation require the R1 amendment. |
| `admittedTarget` | Target leaf GVK, resource name, UID, and mechanism-required runtime identity. |
| `admittedPolicy` | Selected administrator policy UID, generation, and digest when used. |
| `conditions` | Generation-aware common conditions with stable reasons. |

Mechanism status may add bounded progress and observed-effect fields. Status
does not contain credentials, unbounded logs, raw payloads, diagnoses,
expected protocol outcomes, or instructions for another action.

`phase` is a denormalized projection of conditions and mechanism state for
printer columns and clients. Controllers update the projection and its source
facts in the same status patch. No lifecycle fact may exist only in `phase`.

## Common lifecycle

```text
Pending -> Admitted -> Active -> Completed
                         |
                         +-----> Recovering -> Recovered
                                           -> Failed
                                           -> Inconclusive

Any nonterminal phase -> Failed or Inconclusive
```

| Phase | Meaning |
| --- | --- |
| `Pending` | Waiting for a Ready target, policy/profile, serialization guard, or another admission prerequisite. No side effect has begun. |
| `Admitted` | Exact request, expiry, policy, and target identities are durably recorded. No side effect has begun. |
| `Active` | A reversible action is applied, or a finite irreversible operation is in progress. |
| `Recovering` | The controller is removing a reversible action and verifying cleanup. |
| `Completed` | A finite irreversible action reached its requested observed endpoint. |
| `Recovered` | A reversible action ended and cleanup was observed. |
| `Failed` | A definite request, precondition, or mechanism failure occurred and no required cleanup remains unknown. This is not a protocol verdict. |
| `Inconclusive` | Effect, attribution, identity, or required cleanup could not be established safely. |

`Completed`, `Recovered`, `Failed`, and `Inconclusive` are terminal outcome
phases. A terminal outcome is never re-admitted after metadata edits. A
controller may continue finalizer cleanup after recording an inconclusive
outcome, but it cannot revise that outcome to success.

### Terminal outcome mapping

| Trigger and prior state | Required outcome |
| --- | --- |
| Timeout before any side effect, including recorded intent proven not to have executed | `Failed` with `DeadlineExceeded`. |
| Identity divergence before any side effect | `Failed` with `IdentityDiverged`. |
| Finite irreversible action stops after attributable partial progress and no call is ambiguous | `Failed` with the triggering definite reason; retain exact progress. |
| Irreversible call result is ambiguous, or identity diverges after a side effect | `Inconclusive` with `EffectUncertain` or `IdentityDiverged`; never retry blindly. |
| Reversible action reaches its natural duration or timeout after application | Enter `Recovering`; proven normal cleanup ends `Recovered`. |
| Reversible mechanism definitely fails after application | Enter `Recovering`; proven cleanup ends `Failed` with `MechanismFailed` and `CleanupComplete=True`. |
| Reversible cleanup or attribution cannot be established | End `Inconclusive` with `CleanupUncertain` or `EffectUncertain`; retain the finalizer while safe cleanup remains possible. |

Deletion while `Pending`, before a finalizer exists, removes the object without
a terminal phase. If a safety finalizer was added during `Admitted` but no
external intent or effect exists, the controller verifies that absence, clears
the finalizer, and the object disappears without fabricating a terminal phase.
Deletion after a reversible side effect enters `Recovering` under the deletion
timestamp and follows the table above. Deletion after an irreversible side
effect stops future calls and follows the attributable-partial-progress or
ambiguous-effect row; it does not imply that prior state can be restored.

Kubernetes may remove the object immediately after its finalizer is cleared,
so clients must not depend on observing a durable terminal phase for
deletion-triggered cancellation. A configured observation journal may retain
deletion, cleanup, and uncertainty, subject to its source coverage.

## Conditions and reasons

| Condition | Contract |
| --- | --- |
| `Admitted` | Whether the immutable request and exact target/policy identities were accepted. |
| `Progressing` | Whether the controller is applying, observing, or recovering the action. |
| `EffectObserved` | Whether the mechanism-specific effect was observed; `Unknown` preserves ambiguity. |
| `CleanupComplete` | Whether required reversible cleanup was observed; absent when not applicable. |

Every condition carries the action's `observedGeneration`. Controllers use
`meta.SetStatusCondition` and preserve transition times when status is
unchanged.

Common reason values are stable machine interfaces:

| Reason | Meaning |
| --- | --- |
| `TargetNotReady` | The declared logical target cannot yet be admitted. |
| `TargetBusy` | Another action holds the target's durable serialization reservation. |
| `PolicyUnavailable` | Required administrator policy/profile is absent, stale, or invalid. |
| `AdmissionSucceeded` | Immutable request and identities were admitted. |
| `ActionApplying` | A bounded mechanism operation is in progress. |
| `EffectConfirmed` | The requested mechanism effect was observed. |
| `RecoveryInProgress` | Reversible cleanup is in progress. |
| `ActionCompleted` | A finite irreversible operation completed. |
| `ActionRecovered` | Reversible cleanup was observed. |
| `RequestInvalid` | A dynamic invariant not expressible in schema failed. |
| `IdentityDiverged` | Admitted network or target identity changed. |
| `DeadlineExceeded` | The action exhausted its absolute time bound. |
| `MechanismFailed` | The mechanism returned a definite failure. |
| `EffectUncertain` | The effect or attribution cannot be proven. |
| `CleanupUncertain` | Required cleanup cannot be proven. |

Kinds may add mechanism-specific reasons, but cannot change common meanings or
use a generic `Error` reason. Human messages may change.

## Admission and identity

Before mutation, a controller durably records:

- action UID and generation;
- API-server-derived expiry and admitted correlation value;
- `StacksNetwork` name, UID, and generation under the reviewed admission
  contract, with inventory identity when applicable;
- logical target API version, kind, resource name, and UID from the uncached
  leaf read;
- the target leaf's matching network reference and controller owner UID,
  independently validated through the R1 target-admission contract;
- Pod, StatefulSet, runtime image, and configuration-input identity where the
  mechanism depends on them; and
- selected administrator policy/profile identity where applicable.

The controller performs an uncached read immediately before every material
side effect. Identity drift fails closed and is never treated as a new target.
Digests bind admitted inputs or observed artifacts; they are not predictions
of distributed behavior or reproducibility claims.

## Apply, cleanup, and idempotency

- Reconciliation is level-based and derives its next step from current API and
  mechanism state.
- The controller adds `actions.stacks.org/action-cleanup` and persists status
  before the first side effect that may require cleanup or ambiguity handling.
- Reversible mechanisms retain the finalizer until cleanup is observed.
- Irreversible mechanisms persist intent before each external call and inspect
  state after ambiguity. They never retry a mutation blindly.
- Child resources are action-owned, exact-spec checked, and never adopted by
  name. Deletion uses UID preconditions.
- A restart cannot widen target, parameters, duration, timeout, or policy.
- An administrative finalizer escape is explicit and audited; it never
  fabricates `Recovered`.

## Correlation and events

`actions.stacks.org/correlation-id` is the common label for custom actions and
native Chaos Mesh resources. Values use Kubernetes label-value syntax and are
at most 63 characters. The label is a user-selected search hint, not an
attribution, authorization, deduplication, or idempotency key. Object UID
remains authoritative.

Controllers snapshot the admitted value into status and do not change action
semantics if metadata is later edited. Observability records the submitted
object, metadata changes, action UID, controller lifecycle, target identity,
and surrounding telemetry separately. Bounded Kubernetes Events improve live
ergonomics but are not durable evidence.

## Proposed `ActionSafetyPolicy`

`ActionSafetyPolicy` is a recommended administrator-owned namespace policy,
not an execution resource. Agents cannot create, modify, or choose among
policies. Operator configuration selects one policy name for a namespace;
status records its admitted identity.

Every action retains conservative OpenAPI/CEL hard bounds. Until the policy is
implemented, those bounds are absolute. Once a kind supports policy-based
elevation, missing, stale, or ambiguous policy selection blocks admission.
Policy updates affect only actions not yet admitted; admitted actions retain
their recorded limits through cleanup.

The initial release makes no atomic aggregate-impact guarantee across
independent custom and native actions. That limitation never permits one
resource to exceed its hard or admitted policy bounds.

## Controller organization

Use one reconciler per top-level kind and one writer per status field or
external side effect. Shared libraries may provide:

- identity resolution;
- safety-policy evaluation;
- finalizer and condition helpers;
- target-scoped serialization;
- typed RPC clients; and
- action event/correlation metadata.

Serialization is mechanism-specific rather than a generic scheduler. Bitcoin
actions and production require shared target exclusion, whose execution and
recovery guarantees are reopened in [Bitcoin lifecycle](bitcoin-lifecycle.md).
If Leases are retained, one leader-gated manager owns their serialized writes;
this does not fence server-side RPC work. Other action families share exclusion
only when their side effects and execution constraints require it.

Shared code must not accept arbitrary action payloads, enumerate a runtime
mechanism registry, or advance from one action resource to another. Each
controller operates when every other action controller is disabled.

Common ownership is:

| Surface | Writer |
| --- | --- |
| Action spec | Agent at creation; immutable afterward. |
| Action metadata | Kubernetes and authorized clients; never execution identity. |
| Action status/finalizer | Controller for that exact kind. |
| Safety policy | Administrator or policy controller. |
| Mechanism side effect | Controller for that exact kind. |
| Observation journal/evidence | Observability operator. |

## Temporary overrides

A bounded override is distinct from baseline desired state. It must coordinate
with the baseline capability's single effective-behavior writer, refuse
unsupported overlaps, and expire without relying solely on its controller.
Removal resumes the latest baseline, including edits made during the override;
it must not restore a stale snapshot. Exact override kinds, precedence, and
identity contracts remain open in
[Steady-state operation](steady-state-operation.md).

## Common RBAC

- Agent: create/get/list/watch/delete approved action resources; no update,
  patch, status, policy, Secret, workload, or arbitrary RPC permission.
- Action controller: status writes and primary-resource update/patch for
  metadata finalizers on its own kinds, and only the
  mechanism-specific resource/RPC permissions it needs.
- Policy administrator: write `ActionSafetyPolicy`.
- Observer: read action and policy resources; no mutation.

Controller ServiceAccounts may be split when combining permissions would
materially enlarge a compromise. Exact rendered-RBAC allowlists remain release
gates.

Finalizers live in primary-object metadata. Granting only `/status` or a
`/finalizers` rule does not authorize ordinary metadata patches. RBAC cannot
restrict a primary-resource patch to one metadata field; immutable-spec
admission and controller ownership checks complement the exact Role. Envtest
and rendered-RBAC tests must exercise adding and removing real finalizers.

## Structural contract and tests

Action schemas must not contain orchestration fields named `actions`,
`dependsOn`, `executionPlan`, `reduction`, `replay`, `scenario`, `schedule`,
`stages`, `steps`, or `workflow`. They must not contain arrays of embedded
action payloads or dependency references to other actions.

Before the first action CRD merges, schema tests must consume the lifecycle
fixture and non-vacuously assert:

- the complete spec has `self == oldSelf`;
- phase enums and allowed common-condition values match the fixture exactly;
- every forbidden field is absent recursively under `spec`;
- `timeout` has the fixture's duration-string wire type, no OpenAPI `format`,
  the exact positive CEL rule, and a finite per-kind maximum;
- every string, list, and map under `spec` has a structural size bound so CEL
  equality remains cost-bounded;
- reference schemas exclude namespace and arbitrary GVK/selector fields;
- the status subresource and metadata finalizer each have one writer; and
- one valid and one invalid server-side admission example exist per kind.

Every controller then adds transition-table tests proving `phase` agrees with
conditions and mechanism state, plus cached-versus-uncached identity, restart,
foreign-object, timeout, deletion, cleanup, and ambiguous-effect tests.
Current repository tests consume the fixture and validate the served
generation and reorganization schemas, including condition-value CEL restrictions. The generation
profile explicitly amends independent target admission and the meaning of
`startedAt` as the first durable authorization; broader mechanisms remain open.

## Definition of done

- Every custom kind follows the fixture and this document or records a
  reviewed semantic exception.
- Every kind has one resource, controller package, example, reference page,
  exact RBAC slice, and independent lifecycle tests.
- One object affects only one bounded mechanism and logical target.
- Uncertain effect, attribution, identity, or cleanup cannot become success.
- An agent composes resources without any controller assuming their order or
  interpreting the protocol result.

## Deferred decisions

1. Exact per-kind hard and policy-elevated safety bounds.
2. Whether a future cross-kind aggregate safety protocol can preserve direct
   native-resource use without admission side effects or orchestration.
