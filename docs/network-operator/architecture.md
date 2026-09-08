# Controller architecture

The operator follows a level-based, composable reconciliation model inspired
by [Chaos Mesh controller architecture](https://github.com/chaos-mesh/chaos-mesh/tree/master/controllers).
Every reconcile reads current state and moves it toward declared state; no
controller depends on the event that caused the request.

```text
Helm installation: one network operator
├── StacksNetwork → owned actor and capability resources
├── Actor controllers → StatefulSets
├── Bitcoin scheduler → weighted opportunities and target ledgers
├── Administrative lifecycle → account, policy and execution-ledger disposal
└── Capability workload controllers → owned Deployments
    ├── Bitcoin target execution
    ├── STX transfer production
    ├── Contract deployment and observation
    ├── Stacking administration
    └── Network-scoped legacy receipt collection
```

## Ownership

| Component | Runs in | Owned state and effects |
| --- | --- | --- |
| Aggregate reconciler | Operator | Actor and capability specifications, ownership and UID pins; `StacksNetwork.status`. |
| Production scheduler | Operator | Weighted opportunities and UID-pinned target ledgers in `BitcoinBlockProduction.status`; policy retention. |
| Capability workload controllers | Operator | Owned Deployments, Services, ServiceAccounts, Roles and RoleBindings; capability `WorkerReady` conditions. |
| Administrative lifecycle controllers | Operator | Account, production-policy, Bitcoin-target and transfer abandonment status and retention-finalizer release. |
| Bitcoin leaf reconciler | Operator | Bitcoin workload resources; `BitcoinNode.status`. |
| Stacks node reconciler | Operator | Stacks node workload resources; `StacksNode.status`. |
| Signer leaf reconciler | Operator | Consensus signer workload resources; `StacksSigner.status`. |
| Actor workload collaborator | Operator, called by leaf controllers | Rendering, owned-object convergence and runtime identity observation; no independent controller or CR status. |
| Bitcoin production reconciler | Target worker | Initialization, baseline generation and admitted bounded RPC effects; target dispatch/receipt status and retention. |
| Transfer reconciler | Transfer worker | Signing, nonce reservation, submission and inclusion accounting in `StacksTransactionProduction.status`. |
| Contract and stacking reconcilers | Separate capability workers | Declared contract deployment and direct stacking maintenance; capability observations and receipt acknowledgements. |
| Account ledger collaborator | Managed-operation and receipt workers | Durable `StacksAccount` authorizations, nonce state and acknowledgements for managed consumers; receipt workers use only receipt accounting. |
| Legacy receipt receiver | Network receipt worker | Validated legacy execution receipts for existing account authorizations in its network; no signing keys. |

The aggregate package does not import workload Kubernetes kinds. Leaf
controllers share lifecycle mechanics but independently translate their API
into a normalized workload descriptor. The shared collaborator owns no custom
resource status and runs no controller.

## Reconciliation rules

- Controllers own defined fields and effects, not necessarily an entire status.
  Execution status, workload conditions and receipt accounting coexist through
  optimistic-lock patches that preserve other writers' fields. Administrative
  disposal may overlap executor cleanup without granting new execution authority.
- Compiled leaf specifications declare no API-server defaults, so exact
  stored-versus-compiled equality remains a valid drift check.
- Kubernetes convergence is owner-checked and idempotent. Bitcoin dispatches and
  Stacks submissions use durable authorization; ambiguous mutations are not
  blindly retried or reset by worker replacement.
- A labelled object without the expected controller UID is never adopted or
  deleted.
- Controller-owner UIDs, rather than mutable labels, define the set of managed
  leaf resources. Labels remain verified routing and identity assertions.
- Informer delivery order is not a correctness dependency.
- Leaf readiness uses workload and Pod watches. Capability and lifecycle
  controllers also use timed requeues for protocol observations, scheduling and
  parent-retirement checks.
- Leaf and aggregate status use uncached reads for admitted runtime identity.
- Identity admission waits for the StatefulSet controller to observe the
  current generation and verifies the Pod revision and requested image.
- Secret contents are mounted directly; operator and worker ServiceAccounts have
  no Secret-read API permission. Actor configuration and assigned worker
  credentials remain in their respective Pods, outside the operator process.
- Mutable owned Service drift is restored; a non-headless ClusterIP Service is
  replaced only with an API-server UID precondition.
- Immutable StatefulSet identity or storage changes fail explicitly.
- Removing an aggregate actor uses a UID-preconditioned foreground deletion.
- Aggregate readiness remains false until a retired leaf and its owned
  workload resources have finished foreground deletion.

The aggregate compiles desired actors and publishes generation-bound declarations,
then synchronizes Bitcoin production, transfer production and managed operation.
It separately converges topology by bulk-listing leaf kinds, applying missing or
drifted specifications, pruning retired leaves and aggregating uncached runtime
observations. Capability synchronization and topology convergence are both
attempted even when one fails. `ProductionConfigured`, `TransactionsConfigured`
and `Operational` report capability state separately from topology readiness;
none guarantees sustained consensus progress.

The aggregate filters status-only events from production-policy, transfer and
account ledgers while retaining generation, ownership, finalizer, deletion and UID
changes. Contract and stacking status changes enqueue prerequisite observation;
actor status changes update inventory when observed identity changes. Actor
listing uses a fixed number of list operations per reconcile rather than a list
per actor.

Declaration publication uses optimistic-lock status patches before workload
synchronization and preserves current declarations through later degraded or
retiring states. The [Bitcoin producer](bitcoin-production.md) admits its target
independently of complete inventory. It runs in a separate Deployment and
ServiceAccount with no workload mutation permissions.
Its bounded receipt collectors run outside reconcile workers and retain
received-but-unaccounted evidence until accounting, administrative abandonment,
or exhausted shutdown drain.

Each Stacks leaf receives only direct and explicitly declared Service
dependencies. This keeps configuration and Pod-template digests local to the
affected dependency graph; adding an unrelated actor does not roll existing
workloads.

## Inventory contract

The canonical digest payload contains only:

```json
{
  "actors": [],
  "observedGeneration": 1,
  "schemaVersion": "network.stacks.org/inventory/v1"
}
```

Actors are ordered by kind and logical name. Each identity binds the leaf
resource, Service, StatefulSet UID and revision, Pod UID, requested image,
runtime image digest, configuration digest, and complete leaf specification
digest. Observation timestamps and
resource versions remain unhashed metadata.

Every identity field except `specDigest` is sourced from, or verified against,
the observed leaf, Service, StatefulSet, and Pod. Verification includes leaf
ownership and generation, canonical leaf-spec digest, Service ownership,
StatefulSet generation and revision, configuration digest, requested image,
Pod ownership and readiness, and immutable runtime image ID. `specDigest`
intentionally binds the admitted runtime identity to the declared leaf
specification.

The digest is absent until every actor is admitted. Consumers must treat an
absent digest as not ready, not as an empty network.

The versioned compatibility vectors in [`contracts/`](../../contracts/) are
verified independently by the network and observability operators. They pin
the canonical inventory, complete leaf specification, actor Service ports,
and immutable image-ID contracts across the module boundary. Minimal and rich
leaf vectors cover configuration, scheduling, storage, dependency, image-pull,
and process fields. Envtest additionally round-trips every vector through the
API server before comparing its stored-spec digest.

## Extension rule

Add a new leaf kind only for a materially different executable or lifecycle.
Role labels sharing the same binary, workload model, and status contract belong
in one kind. A new kind requires:

1. API and structural schema;
2. deterministic aggregate compilation;
3. a focused descriptor translator;
4. workload, ownership, status, deletion, and idempotency tests;
5. aggregate inventory integration; and
6. Helm RBAC and operator documentation.

Experiment planning and adaptive orchestration belong to the external agent;
bounded action lifecycles and general telemetry/journaling belong to their
respective operators. Network execution workers still observe protocol state and
retain the receipts required for their own capabilities. See the repository
[architecture](../architecture.md#agent-and-operator-boundary).

## Managed network operation

The aggregate compiles the optional same-name `StacksTransactionProduction` and
pins its UID. Its worker signs transfers, reserves the account nonce and observes
native inclusion. Managed operation separately compiles `StacksAccount`,
`StacksContractSet` and `StacksStackingParticipant` resources with retained
incarnation pins. Accounts are ledgers, not Pods. Contract and stacking workers
use their explicitly assigned accounts; the keyless receipt worker serves the
network's legacy inclusion path.

Declared Bitcoin initialization, pinned contract deployment and supported direct
PoX-4/PoX-5 stacking and renewal converge through these workers. Provisioning tools
render network-level genesis and referenced artifacts; an external bootstrap or
renewal process is not required for a managed network. Funding a genesis
account alone does not enroll it in managed operation. Stacking
administration runs separately from consensus signer Pods. Go owns reconciliation,
submission and evidence accounting; JavaScript is limited to offline SDK encoding
and signing.

See the [transfer contract](stacks-production.md),
[managed-operation design](../design/managed-network-operation.md) and
[configuration guide](configuration.md) for declarations and supported profiles.

## Capability lifecycle

Workload controllers reconcile single-replica execution Deployments with `Recreate`
updates. `WorkerReady` on Bitcoin targets, transfers, contract sets and stacking
participants reports Deployment availability, not protocol readiness. The receipt
worker uses native Deployment status instead. Workers bind to namespace/name/UID
and independently admit current target identity before authorizing effects.

Chart capability switches gate provisioning; the Bitcoin switch also gates
baseline scheduling. Existing workers may continue, including Bitcoin
initialization without a scheduled opportunity. Network pause policy controls new
work while allowing outstanding receipt accounting. Withdrawing managed operation
retains repairable observation workers without signing mounts; transfer workers
still require their mounted account profile.

Administrative lifecycle controllers remain registered independently of these
switches. When a parent is removed, replaced or deleting, they record abandonment
and release account, Bitcoin policy-root, target and transfer retention without
requiring execution Pods. Garbage collection can then dispose of owned workers.
Deletion and leader election never establish RPC quiescence or erase an unknown
outcome. See [operator workloads](../design/operator-workloads.md) and the
[upgrade procedure](../../charts/stacks-network-operator/README.md#upgrading-execution-workers).

## Installation scope

The operator watches all namespaces by default; a namespace-scoped installation
is optional. Networks and their credentials are independent of Helm releases.
The operator has no Secret API permission and does not sign or submit protocol
transactions. Execution workers bind to a capability UID, mount only assigned
credentials, and retain the existing CAS/receipt guarantees across Pod replacement.
See [operator workloads](../design/operator-workloads.md) for the full boundary.
