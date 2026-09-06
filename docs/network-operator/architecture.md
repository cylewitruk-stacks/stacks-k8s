# Controller architecture

The operator follows a level-based, composable reconciliation model inspired
by [Chaos Mesh controller architecture](https://github.com/chaos-mesh/chaos-mesh/tree/master/controllers).
Every reconcile reads current state and moves it toward declared state; no
controller depends on the event that caused the request.

```text
StacksNetwork reconciler
└── compiles and owns leaf custom resources
    ├── BitcoinNode reconciler
    ├── StacksNode reconciler
    └── StacksSigner reconciler
        └── shared workload lifecycle collaborator
```

## Ownership

| Owner | Fields and effects |
| ---- | ---- |
| Aggregate reconciler | Leaf specifications and ownership; `StacksNetwork.status`. |
| Production reconciler | UID-pinned `BitcoinBlockProduction.status`, finalizer, and typed single-block RPC effects. |
| Bitcoin leaf reconciler | Bitcoin workload resources; `BitcoinNode.status`. |
| Stacks node reconciler | Stacks node workload resources; `StacksNode.status`. |
| Signer leaf reconciler | Signer workload resources; `StacksSigner.status`. |
| Workload collaborator | Pure rendering, owned-object convergence, and live identity observation on behalf of one leaf reconciler. |

The aggregate package does not import workload Kubernetes kinds. Leaf
controllers share lifecycle mechanics but independently translate their API
into a normalized workload descriptor. The shared collaborator owns no custom
resource status and runs no controller.

## Reconciliation rules

- One logical controller writes each status or managed child specification.
- Compiled leaf specifications declare no API-server defaults, so exact
  stored-versus-compiled equality remains a valid drift check.
- Kubernetes convergence is owner-checked and idempotent. Non-idempotent Bitcoin
  production uses durable authorization and never blindly retries an RPC.
- A labelled object without the expected controller UID is never adopted or
  deleted.
- Controller-owner UIDs, rather than mutable labels, define the set of managed
  leaf resources. Labels remain verified routing and identity assertions.
- Informer delivery order is not a correctness dependency.
- Expected waits are driven by owned StatefulSet and Pod watches.
- Leaf and aggregate status use uncached reads for admitted runtime identity.
- Identity admission waits for the StatefulSet controller to observe the
  current generation and verifies the Pod revision and requested image.
- Secret contents are mounted directly; no controller has Secret-read API
  permission. Only the separate producer mounts and reads its RPC password.
- Mutable owned Service drift is restored; a non-headless ClusterIP Service is
  replaced only with an API-server UID precondition.
- Immutable StatefulSet identity or storage changes fail explicitly.
- Removing an aggregate actor uses a UID-preconditioned foreground deletion.
- Aggregate readiness remains false until a retired leaf and its owned
  workload resources have finished foreground deletion.

The aggregate reconciler is an ordered pipeline: compile desired leaf objects,
publish generation-bound declarations, synchronize the optional production ledger,
bulk-list each leaf kind, synchronize only missing or drifted specifications,
prune retired leaves, then bulk-list directly from the API server to aggregate
status. Production synchronization and topology convergence both run even if
the other fails; a separate `ProductionConfigured` condition reports optional
capability configuration. Production status-only updates are filtered from
the aggregate watch, while generation, ownership, finalizer, deletion, and UID
changes remain observable. Actor status-only events still update inventory
when observed identity changes. The number of Kubernetes list operations is
constant per reconcile rather than proportional to actor count.

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

Faults, protocol observations, evidence, and adaptive sessions do not belong
in this manager. Their ownership boundaries are defined in the repository
[architecture](../architecture.md#agent-and-operator-boundary).
