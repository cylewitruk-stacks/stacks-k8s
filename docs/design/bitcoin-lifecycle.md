# Bitcoin lifecycle design

## Purpose

Bitcoin topology and Bitcoin state transitions are separate concerns.
`StacksNetwork` declares Bitcoin nodes and peer relationships. Small action
resources control mining and explicit regtest state transitions.

Natural reorganizations caused by miners and network behavior require no
resource. They are observed facts. A controller-forced reorganization is a
bounded action and therefore does not belong in `StacksNetwork`.

## Proposed resources

| Resource | Kind | Purpose |
| --- | --- | --- |
| `BitcoinMiningWindow` | Bounded action | Generate blocks at a cadence up to an immutable count and deadline. |
| `BitcoinBlockRequest` | Bounded action | Generate a finite number of blocks, optionally at a short interval. |
| `BitcoinReorganization` | Bounded action | Replace a finite regtest suffix with a higher-work branch. |

Working API group: `actions.stacks.org/v1alpha1`.

## `BitcoinMiningWindow`

### Example

```yaml
apiVersion: actions.stacks.org/v1alpha1
kind: BitcoinMiningWindow
metadata:
  name: bitcoin-a-cadence
spec:
  networkRef:
    name: mixed-network
  bitcoinNodeRef:
    name: bitcoin-a
  interval: 60s
  blocksPerTick: 1
  maximumBlocks: 120
  deadline: 2h30m
  destination:
    address: bcrt1qexample
```

### Spec and status

| Spec field | Contract |
| --- | --- |
| `networkRef`, `bitcoinNodeRef` | Same-namespace logical references. |
| `interval` | Positive bounded cadence, recommended `1s..24h`. |
| `blocksPerTick` | Bounded `1..100`; values above a policy threshold require administrator permission. |
| `maximumBlocks` | Immutable positive total cap for the action. |
| `deadline` | Immutable runtime bound; expiry applies even if its controller is unavailable. |
| `destination` | Explicit regtest address or externally managed wallet reference. |
| `rpcProfileRef` | Administrator-configured credential profile, never copied to status. |

Status records observed generation, target network/actor UID, target runtime
image identity, admitted request/policy identity, completed block count, last
attempt/completion time, observed height and tip, phase, and conditions.

### Reconciliation, mutability, and ownership

The controller resolves one Ready Bitcoin actor through the admitted
`StacksNetwork` inventory and confirms `getblockchaininfo.chain == regtest`
before invoking a closed typed RPC client. It never patches topology. Before
each tick it records expected tip and durable intent, then reconciles an
ambiguous response by reading chain state. Unprovable completion becomes
`Inconclusive` and is not retried blindly.

Spec is immutable after admission. Completion count or deadline permanently
terminates the action. Deletion stops future ticks but does not reverse blocks
already mined. Only this controller writes its status and mining side effect.

A node-scoped coordination Lease prevents overlap with block requests or a
reorganization. The Lease protects one actor; it never orders resources for
the agent.

### Safety and RBAC

- Regtest only.
- One active mining action per Bitcoin actor, enforced by the node-scoped
  Lease shared by typed Bitcoin action controllers.
- Hard interval and blocks-per-tick limits.
- Secret read limited to the named credential reference.
- No generic RPC method or arbitrary JSON-RPC payload.
- Loss of target identity stops mining and reports `Inconclusive`.

### Tests and definition of done

- Unit-test cadence, count/deadline expiry, deletion, identity drift, Lease
  contention, controller outage, and ambiguous typed-RPC results.
- Envtest uniqueness and status ownership.
- Live-test two independently controlled miners and one follower.
- Done when block/count limits survive restarts and outages, no tick occurs
  after deadline or while identity is unknown, and observed height/tip facts
  are published without claiming protocol correctness.

## `BitcoinBlockRequest`

### Example

```yaml
apiVersion: actions.stacks.org/v1alpha1
kind: BitcoinBlockRequest
metadata:
  name: flash-20
spec:
  networkRef:
    name: mixed-network
  bitcoinNodeRef:
    name: bitcoin-b
  blocks: 20
  interval: 100ms
  destination:
    address: bcrt1qexample
  deadline: 2m
```

### Spec and status

The immutable action spec contains exact network and Bitcoin actor references,
`blocks`, optional inter-block `interval`, destination, deadline, and optional
administrator-approved RPC profile. Admission bounds blocks, interval, and
total requested duration.

Status contains phase, observed generation, admitted target identities,
starting height/tip, requested and completed block counts, latest observed
height/tip, timestamps, and conditions. It contains no block-signing secrets.

### Reconciliation, mutability, and ownership

One object represents one finite request. Action-defining spec fields are
immutable after admission. The controller obtains the same node Lease used by
the mining window, rechecks target identity, and generates at most one next
block per reconciled checkpoint.

Bitcoin RPC does not provide an idempotency key. Before each call the
controller records the expected prior height/tip and intent. After an ambiguous
transport failure it reads chain state. If it cannot prove whether the call
completed, the action becomes `Inconclusive` and is not automatically retried.

Deletion requests cancellation. The finalizer waits only for an in-flight RPC
deadline and releases the Lease; already generated blocks remain.

### Safety, tests, and definition of done

- Regtest-only typed RPC and hard block/deadline limits.
- No overlap on the same node; independent nodes may run concurrently.
- Negative tests for stale actor UID, non-regtest chain, ambiguous RPC result,
  deadline, and retry after controller restart.
- Live test ordinary and flash intervals while a mining window contends for
  the same node Lease.
- Done when completed count and observed chain facts are attributable and a
  retry cannot silently generate an unbounded extra block.

## `BitcoinReorganization`

### Example

```yaml
apiVersion: actions.stacks.org/v1alpha1
kind: BitcoinReorganization
metadata:
  name: replace-two-blocks
spec:
  networkRef:
    name: mixed-network
  bitcoinNodeRef:
    name: bitcoin-b
  depth: 2
  replacementBlocks: 3
  replacementInterval: 250ms
  deadline: 2m
  boundaryPolicy:
    allowEpochBoundary: false
    allowRewardCycleBoundary: false
```

### Spec and status

The immutable spec names one Ready Bitcoin actor and supplies depth,
replacement block count, interval, deadline, destination/credentials, and
explicit protocol-boundary permissions. `replacementBlocks` must exceed
`depth` so the replacement branch has more work.

Status records the admitted node/network identity, original height/tip,
invalidated hashes, replacement hashes, final height/tip/chainwork, phase,
timestamps, and conditions. Observation of other Bitcoin or Stacks nodes is
the observability operator's responsibility.

### Reconciliation, mutability, and ownership

The controller obtains the node Lease, confirms regtest, checks the requested
suffix against current tip, and uses a closed RPC surface such as
`getblockchaininfo`, `getblockhash`, `invalidateblock`, `reconsiderblock`, and
bounded block generation. It does not create a network partition. The agent
uses native `NetworkChaos` separately when it wants split views.

Because the action changes chain history, it is not generally recoverable.
Deletion stops remaining work but cannot restore prior consensus state. An
ambiguous irreversible RPC result becomes `Inconclusive`; the controller never
guesses and repeats it.

### Safety and RBAC

- Regtest only and exactly one actor target.
- Recommended initial bounds: depth `1..144`, replacement blocks `2..288`.
- Reject depth beyond observed height.
- Boundary crossing requires explicit permission for each known epoch,
  reward-cycle, or prepare-phase class; an unknown schedule is not safe.
- No arbitrary RPC passthrough.
- Network and actor UID/image identity rechecked immediately before every
  irreversible mutation.

### Tests and definition of done

- Boundary tests at depth/replacement limits and protocol boundaries.
- Crash/ambiguous-call tests prove no blind retry.
- Live test replaces a suffix with a higher-work branch and records all hashes.
- Combined live test lets an agent apply a separate partition and confirms
  two Bitcoin views are observable; the reorg controller itself remains
  unaware of that scenario.
- Done when the action is bounded, identity-pinned, regtest-only, and reports
  exact observed mutations without claiming downstream recovery.

## Alternatives

| Alternative | Disposition |
| --- | --- |
| Mining fields inside `StacksNetwork` | Rejected; process topology and bounded block production change independently. |
| Unbounded mining policy | Rejected; irreversible generation must have count and time bounds. |
| Reorganization as a fault stage | Rejected; no in-cluster stage abstraction. |
| Generic Bitcoin RPC resource | Rejected; expands the mutation boundary without bounded semantics. |
| Controller-managed partition during reorg | Rejected; the agent composes a separate native fault. |

## RPC and destination profiles

`rpcProfileRef` and wallet/destination profiles name immutable entries in
administrator-provided operator configuration. Each entry binds endpoint
policy, Secret name, permitted network namespace, and configuration digest.
The chart grants Secret `resourceNames` only for configured profiles; an agent
cannot point an action at an arbitrary Secret or endpoint. Status records the
profile name/digest but never credentials. Profile removal or digest change
stops new admission and makes an active action `Inconclusive` before its next
call.

Unit and chart tests prove arbitrary Secret names, cross-namespace profiles,
and unconfigured endpoints are inaccessible. Credential rotation uses a new
profile version or a documented projected-credential reload contract.

## Open decisions

1. Destination address versus wallet-reference API for initial release.
2. Whether bootstrap reserve-output mining is a policy mode or external setup.
3. Exact initial depth/block limits after real-image qualification.
4. How protocol schedule facts are supplied without coupling to Stacks config
   parsing.
