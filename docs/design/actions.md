# Atomic action contract

## Purpose

Protocol-specific controllers expose small Kubernetes resources an external
agent can create independently. Each resource describes one bounded action and
reports its own observed lifecycle.

This contract applies to recommended `actions.stacks.org` resources. Native
Chaos Mesh resources retain their upstream schemas and status contracts.

## Non-goals

- A resource containing a list, graph, stage, schedule, or scenario.
- Cross-resource sequencing or automatic follow-up actions.
- Replay, reduction, diagnosis, or regression generation.
- Declaring an action successful because its mutation call returned success.
- Hiding incomplete or ambiguous effects behind a generic `Succeeded` phase.

## Common resource shape

Every action CRD should use the following conceptual shape without embedding a
generic polymorphic action payload:

```yaml
apiVersion: actions.stacks.org/v1alpha1
kind: ExampleAction
metadata:
  name: example
spec:
  networkRef:
    name: network
  targetRef:
    kind: StacksNode
    name: follower-a
  deadline: 2m
status:
  observedGeneration: 1
  phase: Active
  admittedTarget:
    networkUID: example
    actorUID: example
    podUID: example
  startedAt: "2026-09-03T12:00:00Z"
  conditions: []
```

Shared Go types may cover references, admitted identity, timestamps,
conditions, and phase vocabulary. Mechanism parameters remain typed fields on
their own CRD so one generic controller cannot become an action multiplexer.

## Common lifecycle

| Phase | Meaning |
| --- | --- |
| `Pending` | Waiting for a Ready target, safety policy, dependency, or Lease. |
| `Admitted` | Exact request and target identity recorded; no mutation yet. |
| `Active` | Reversible action applied or irreversible action in progress. |
| `Completed` | Finite irreversible action reached its requested observed state. |
| `Recovered` | Reversible action was removed and cleanup was observed. |
| `Failed` | A definite precondition or mechanism failure occurred. |
| `Inconclusive` | Identity, effect, completion, or cleanup could not be proven. |

Not every action uses both `Completed` and `Recovered`. Status and condition
reason names are stable machine interfaces. Human messages may change.

Initial common conditions are `Admitted`, `Active`, `EffectObserved`,
`CleanupComplete`, and `Ready`. Their `observedGeneration` always identifies
the spec generation evaluated. Terminal phases retain their final conditions
and admitted identity; a later metadata-only generation does not restart work.

## Admission and identity

Before mutation, a controller records:

- action generation, UID, and canonical request identity;
- `StacksNetwork` name, UID, and observed generation;
- complete admitted inventory identity;
- selected logical actor and leaf UID;
- Pod, StatefulSet, image, and configuration identity when relevant; and
- applicable `ActionSafetyPolicy` UID and generation.

The controller uses an uncached API read immediately before mutation. Any
identity difference fails closed. It never silently targets a replacement Pod
or newer network generation.

Action-defining fields become immutable after `Admitted`. A user creates a new
resource for a materially different action. Mutable cancellation fields are
avoided; deletion is the common cancellation signal.

## Apply, cleanup, and idempotency

- Reconcile is level-based and re-reads actual state.
- Reversible actions carry a finalizer until cleanup is observed.
- Irreversible actions record a durable intent/checkpoint before each side
  effect and never blindly repeat an ambiguous operation.
- Ownership is limited to resources created by that action UID.
- Deletion uses UID preconditions and never adopts a same-named foreign object.
- Cleanup failure remains visible and retains the finalizer unless an explicit
  administrator escape procedure is used.
- Controller restart cannot expand targets or duration.

## Proposed `ActionSafetyPolicy`

`ActionSafetyPolicy` is an administrator-owned, namespace-scoped policy—not an
execution resource. Working API group: `actions.stacks.org/v1alpha1`.

### Example

```yaml
apiVersion: actions.stacks.org/v1alpha1
kind: ActionSafetyPolicy
metadata:
  name: default
  namespace: stacks-regtest
spec:
  allowedActionKinds:
    - BitcoinBlockRequest
    - BitcoinMiningWindow
    - BitcoinReorganization
    - ApplicationClockOffset
    - SignerBehavior
  maximumDuration: 10m
  maximumTargetsPerAction: 1
  allowIrreversibleBitcoinActions: true
  allowUnenrolledTargets: false
```

### Spec and status

Spec contains an allowlist of custom action kinds, hard per-action duration and
target limits, irreversible-action permissions, and target-enrollment policy.
Status records observed generation, active policy digest, validity, and
conditions. It contains no active-action schedule.

### Ownership and mutability

Cluster administrators create and update the policy. Agent Roles receive read
but not write access. Exactly one selected policy applies to an action
namespace; selection is fixed by chart configuration or a reserved name to
avoid agent-controlled policy choice.

Policy changes affect new admission. Already-admitted bounded actions retain
their admitted limits through terminal cleanup; the policy controller never
deletes or sequences them. Deletion fails closed for new custom actions until
a replacement policy is available.

### Safety, observability, and tests

- Admission refuses missing, invalid, stale, or ambiguous policy selection.
- Every action status records policy UID/generation.
- Observability records policy mutations and action-policy bindings.
- Envtest covers uniqueness, policy changes during active actions, agent RBAC,
  and deletion behavior.
- Done when an agent cannot widen or omit policy and every custom action stays
  within its admitted per-resource limits.

### Alternatives

| Alternative | Disposition |
| --- | --- |
| Limits repeated in every action | Required as hard schema bounds and supplemented by administrator policy. |
| Agent-supplied safety block | Rejected; the mutating principal cannot choose its own limits. |
| One policy reference per action | Rejected by default; it permits policy shopping. |
| Cross-kind atomic reservation controller | Deferred; wrapping native resources or admission side effects would violate the direct-resource model. |

## Controller organization

Use one reconciler per top-level kind and one writer per status field or
external side effect. Shared libraries may provide:

- identity resolution;
- safety-policy evaluation;
- finalizer and condition helpers;
- target-scoped Lease acquisition;
- typed RPC clients; and
- action event/correlation metadata.

Shared code must not contain a registry that accepts arbitrary action payloads
or a pipeline that advances from one resource to another. Each controller must
be describable independently and operate if all other action controllers are
disabled.

## Common RBAC

- Agent: create/get/list/watch/delete approved action resources; no status
  writes, policy writes, Secret reads, or direct workload mutation.
- Action controller: status/finalizer writes for its own kinds and only the
  mechanism-specific resource/RPC permissions it needs.
- Policy administrator: write `ActionSafetyPolicy`.
- Observer: read action and policy resources; no mutation.

Controller ServiceAccounts may be split when combining permissions would
materially enlarge a compromise. Exact rendered-RBAC allowlists remain release
gates.

## Common tests

- Structural schema and CEL bounds.
- Status phase and condition transition tables.
- Cached-versus-uncached identity negative control.
- Controller restart at every side-effect boundary.
- Foreign-object and same-name collision refusal.
- Policy changes and concurrent independent actions.
- Deadline, cancellation, finalizer, cleanup, and ambiguous-effect paths.
- Envtest status ownership and generation behavior.
- Live mechanism effect and cleanup without requiring scenario orchestration.

## Definition of done

- Every implemented action kind has one resource, controller, example, and
  reference page.
- No action spec contains multiple actions or dependency/order fields.
- Every custom mutation is exact-identity-pinned. Native Chaos mutations pin
  immutable logical targets and explicitly report Pod-identity divergence.
  Every custom action is policy-bound; every action is bounded, observable,
  and cleanup-safe for its mechanism.
- The agent can compose resources concurrently without any controller assuming
  their order.
- Unknown or ambiguous outcomes are `Inconclusive`, never successful.

## Open decisions

1. Whether a missing safety policy rejects all custom actions or applies
   immutable compiled chart defaults.
2. Which conditions are common enough to share without coupling controllers.
3. Whether a future cross-kind aggregate safety protocol can preserve direct
   native-resource use without admission side effects or orchestration.
