# Protocol-specific atomic actions

## Purpose

Native Chaos Mesh resources cover generic infrastructure disruption. Stacks
and Bitcoin protocol behavior needs narrowly typed resources whose controllers
understand one bounded mechanism. These resources follow the common contract
in [Atomic action contract](actions.md).

All names and schemas below are recommendations, not current implementation.
Working API group: `actions.stacks.org/v1alpha1`.

## Baseline and temporary behavior

Permanent mining/configuration changes belong to the actor's desired state in
`StacksNetwork`. Ongoing transaction demand is a separate baseline producer
capability, provisionally named `StacksTransactionProduction`; it is not a
bounded action. These contracts are described in
[Steady-state operation](steady-state-operation.md).

Temporary behavior resources leave baseline specs intact. Their integration
must identify one effective-behavior writer, conflict rules, actor-enforced
expiry, and restoration of the latest baseline. The examples below illustrate
mechanisms, not finalized override precedence or schemas.

## Resource summary

| Resource | Purpose | Initial disposition |
| --- | --- | --- |
| `ApplicationClockOffset` | Apply a bounded clock offset through an actor's explicit test interface. | Conditional on a demonstrated native `TimeChaos` gap |
| `SignerBehavior` | Activate one bounded signer testing behavior on one signer. | Recommended |
| `MinerBehavior` | Activate one bounded miner testing behavior on one Stacks miner. | Recommended |
| `ProtocolInputInjection` | Submit one bounded malformed, stale, or oversized protocol input. | Recommended after endpoint design |
| `ActorDiskPressure` | Use an actor-visible helper when qualified native `IOChaos`/`StressChaos` cannot express the test. | Conditional |

Each object selects exactly one action. An enum chooses a mechanism variant;
it does not turn the object into a list or scenario.

## `ApplicationClockOffset`

### Example

```yaml
apiVersion: actions.stacks.org/v1alpha1
kind: ApplicationClockOffset
metadata:
  name: signer-a-plus-90s
  labels:
    actions.stacks.org/correlation-id: investigation-42
spec:
  networkRef:
    name: mixed-network
  actorRef:
    kind: StacksSigner
    name: mixed-network-signer-a
  offset: 90s
  duration: 2m
  timeout: 3m
```

### API and lifecycle

Spec contains exact network/target references, signed offset, duration, and an
optional explicit test-interface port. Status contains the common admitted
identity, active/observed offset, start/expiry/recovery times, phase, and
conditions.

The controller requires an image-declared clock-control capability and a
mounted clock-policy object owned by the selected network. It verifies that
all offsets are zero before admission, applies only the selected actor's
offset, and restores the latest declared baseline on deletion or expiry. The example
assumes a zero-offset baseline; nonzero baseline support requires an explicit
composition contract. A finalizer remains until
restoration is observed. The actor-side clock mechanism also enforces the
absolute expiry recorded in the request, so controller unavailability cannot
extend the offset.

The complete spec is immutable from creation. Only the controller may write
the clock policy. The target Pod and workload are never patched.

### Safety, RBAC, observation, and tests

- Hard offset and duration bounds; values above conservative thresholds need
  administrator policy permission.
- Refuse images without the declared testing capability and policies not
  owned by the target network.
- RBAC grants update/patch on its own primary resources for metadata
  finalizers, its status subresource, and clock-policy resources. Primary
  resource permissions are not field-scoped; see [Common RBAC](actions.md#common-rbac).
- Observation records requested/observed offsets, target identity, expiry,
  restoration, and all incomplete reads.
- Unit tests cover every capability failure; envtest covers finalizers and
  ownership; a real-image test proves actor-visible offset and restoration,
  including controller unavailability beyond expiry.

Done means the action cannot alter system time, cannot target an unenrolled
actor, and cannot report recovery until the effective baseline is observed.

## `SignerBehavior`

### Example

```yaml
apiVersion: actions.stacks.org/v1alpha1
kind: SignerBehavior
metadata:
  name: signer-2-equivocates
  labels:
    actions.stacks.org/correlation-id: investigation-42
spec:
  networkRef:
    name: mixed-network
  signerRef:
    name: mixed-network-signer-2
  behavior: ConflictingResponses
  activation:
    bitcoinHeight: 420
  duration: 90s
  timeout: 2m
  maximumResponses: 4
```

### API and lifecycle

Initial behavior enum: `WithholdResponse`, `DelayResponse`,
`ConflictingResponses`, and `MisreportLocalState`. Each behavior has a typed
parameter branch validated by CEL; unrelated branches are forbidden.
Activation may name one Bitcoin height, tenure identifier, or message digest.
Omitting activation means immediate activation. Duration and an
effect-specific count cap are mandatory.

Status records admitted signer/network/image identity, capability profile,
activation observation, emitted/suppressed count, expiry, restoration, phase,
and conditions. It reports mechanism facts, not whether consensus was harmed.

The controller calls an action-UID-idempotent `activate/query/revoke` contract
on the signer testing interface. Activation supplies action UID, immutable
request digest, and absolute expiry; repeating it returns the same activation
identity and cannot widen behavior. Actor-side expiry restores normal behavior
even if the response or controller is lost. The controller never fabricates
or signs protocol messages itself. Deletion revokes by action UID, and a
finalizer waits for the signer to report the normal profile. Spec is immutable
from creation.

### Safety, RBAC, observation, and tests

- Testing-only image capability and exact runtime image identity are required.
- One signer target; bounded duration/count/delay; no arbitrary message bytes,
  signing key, shell, RPC method, or free-form behavior name.
- The controller has no Secret read or workload write permission.
- `ActionSafetyPolicy` enforces the custom action's signer-impact bound. The
  initial architecture does not claim atomic aggregate accounting across
  independent native and custom resources.
- Observation records control-plane lifecycle separately from actor-reported
  counters and labels their evidence origins.
- Tests cover capability refusal, exact activation predicates, lost activation
  response, controller unavailability beyond expiry, identity replacement,
  restart, forged status, and per-action signer-impact limits.

Done means a normal image is rejected before mutation, the behavior cannot
outlive its bounds after controller recovery, and status never turns a
protocol assertion into an action result.

## `MinerBehavior`

### Example

```yaml
apiVersion: actions.stacks.org/v1alpha1
kind: MinerBehavior
metadata:
  name: miner-b-stale-parent
  labels:
    actions.stacks.org/correlation-id: investigation-42
spec:
  networkRef:
    name: mixed-network
  minerRef:
    name: mixed-network-miner-b
  behavior: StaleParentProposal
  activation:
    tenure: "0x0123456789abcdef"
  maximumProposals: 1
  duration: 2m
  timeout: 3m
```

### API and lifecycle

Initial enum: `WithholdProposal`, `StaleParentProposal`, and
`InvalidTenureProposal`. The schema uses typed branches and requires a
duration and maximum proposal count. Status mirrors `SignerBehavior` with
miner-specific activation and proposal counters.

The controller talks only to a testing capability exposed by the admitted
miner image. It uses the same action-UID-idempotent `activate/query/revoke` and
actor-enforced absolute-expiry contract as `SignerBehavior`. It does not patch
miner configuration, submit arbitrary blocks, or select the next action.
Identity, immutability, finalizer, and restoration semantics match
`SignerBehavior`.

Safety policy independently caps impacted miners. Tests cover all enum
branches, normal-image refusal, exact count exhaustion, tenure activation,
controller restart, identity drift, and restoration. Observation labels miner
telemetry as self-reported unless corroborated externally.

Done means one resource can influence only its selected miner and bounded
proposal class, while ambiguous activation or cleanup becomes `Inconclusive`.

## `ProtocolInputInjection`

### Example

```yaml
apiVersion: actions.stacks.org/v1alpha1
kind: ProtocolInputInjection
metadata:
  name: follower-a-stale-block
  labels:
    actions.stacks.org/correlation-id: investigation-42
spec:
  networkRef:
    name: mixed-network
  stacksNodeRef:
    name: mixed-network-follower-a
  inputKind: StaleBlock
  fixtureRef:
    configMap:
      name: stale-block-fixture
      key: input.bin
    sha256: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
  maximumBytes: 1048576
  timeout: 30s
```

### API and lifecycle

Initial input enum is deliberately closed: `MalformedBlock`, `StaleBlock`,
`MalformedTransaction`, and `OversizedP2PFrame`. The object references one
immutable, content-digested fixture; raw bytes are not stored in the CRD.
Status records admitted identity, fixture identity/size, endpoint capability,
delivery facts, and phase. It does not call rejection a vulnerability or
acceptance a success.

The controller validates the fixture and target before one delivery through a
purpose-built testing endpoint. Because delivery can be ambiguous, it writes
intent first and never blindly retries. The action is finite and immutable;
deletion only stops delivery if it has not begun.

### Safety, RBAC, observation, and tests

- Hard fixture size and timeout limits; immutable ConfigMap or read-only
  object-store references only.
- No arbitrary destination, socket, RPC, command, or host path.
- Controller reads only named fixtures and cannot write actor workloads.
- Observation records API request, fixture digest, delivery uncertainty, and
  subsequent telemetry without diagnosing the response.
- Property/fuzz tests cover decoder bounds; envtest covers reference and
  identity changes; real-image tests exercise every supported input kind.

Done requires a reviewed testing endpoint and fixture format for each enum
value. Until then this CRD remains a recommendation, not an implementation
commitment.

## Conditional `ActorDiskPressure`

### Example

```yaml
apiVersion: actions.stacks.org/v1alpha1
kind: ActorDiskPressure
metadata:
  name: follower-a-disk-pressure
  labels:
    actions.stacks.org/correlation-id: investigation-42
spec:
  networkRef:
    name: mixed-network
  actorRef:
    kind: StacksNode
    name: mixed-network-follower-a
  bytes: 256Mi
  workers: 2
  duration: 3m
  timeout: 4m
```

This fallback is allowed only where the qualified platform matrix says native
Chaos Mesh cannot produce actor-visible bounded pressure. A helper Job writes
and removes files inside an explicitly enrolled scratch or data volume; it
must not mount the host filesystem. Status records allocated bytes, helper
identity, expiry, cleanup, and uncertainty.

The action owns its helper and files through an action-unique directory. Its
Role grants helper Job creation; Kubernetes RBAC does not constrain which PVC
a Job mounts. Admission, renderer confinement, and ownership checks must enforce
the volume/path boundary. Cleanup during controller/helper failure and absolute
expiry remain implementation gates; a Job timeout alone does not remove files.
Tests enforce path confinement, bounded numeric inputs, partial
allocation, restart, cleanup, and foreign-file preservation.

Done means the fallback is separately named, documented as non-native, and
cannot run on platforms where its volume/path contract is unproven.

## Controller boundaries

Start with one independently versioned `stacks-action-operator` chart and one
reconciler package per kind. Split deployments when privilege, dependency, or
failure-isolation differences warrant it. Shared identity, policy,
target-serialization, and condition libraries must not become a generic
mechanism engine.

## Alternatives

| Alternative | Disposition |
| --- | --- |
| Add protocol mechanisms to a Chaos Mesh fork | Rejected initially; it couples protocol code to a privileged generic daemon. |
| Generic `Fault` CRD with `type` and free-form parameters | Rejected; obscures schema, RBAC, and controller ownership. |
| Put temporary experiment overrides in `StacksNetwork` | Rejected; baseline behavior belongs there, while bounded overrides use separate resources. |
| Let the agent call testing endpoints directly | Possible for prototypes, but loses bounded admission, status, cleanup, and audit correlation. |

## Open decisions

1. Exact image capability discovery protocol and authentication.
2. Which signer/miner behaviors are feasible without weakening production
   code paths.
3. Fixture storage backend for binary protocol inputs.
4. Whether `ActorDiskPressure` is needed after the platform matrix is known.
