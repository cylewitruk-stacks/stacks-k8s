# Stacks action operator chart

This independently versioned chart owns the served `actions.stacks.org` CRDs
and their lifecycle Deployment. `BitcoinBlockGeneration` is enabled by default;
`BitcoinReorganization` is optional. The network chart provides the sole Bitcoin
executor. First install this chart and its CRDs, then enable the corresponding
network action-selection flags. Use one action chart release per namespace;
replicas of that release share a single leader-election Lease.

```bash
helm upgrade --install stacks-action-operator charts/stacks-action-operator \
  --namespace stacks-regtest
```

Before images are published, build/load a local image and override
`image.repository` and `image.tag`. See [operations](../../docs/action-operator/operations.md)
for the full setup, matching network configuration, outcomes and removal.

| Value | Default | Meaning |
| --- | --- | --- |
| `bitcoinGeneration.enabled` | `true` | Run finite-generation lifecycle controller and its quota/RBAC. |
| `bitcoinReorganization.enabled` | `false` | Run the existing local-reorganization lifecycle controller and its quota/RBAC. |
| `replicaCount` | `1` | 1–5 manager replicas; more than one requires leader election. |
| `controller.leaderElection` | `true` | Namespace-local single active lifecycle writer. |
| `controller.maxConcurrentReconciles` | `2` | 1–32 concurrent reconciles per kind. |
| `serviceAccount.create` | `true` | Create the namespaced lifecycle identity. |

At least one kind must be enabled. Each enabled kind has a namespace quota of
64 objects. The chart grants no Secret, workload or production-ledger writes.
No RPC credential is mounted. Health/readiness and non-root, read-only container
settings are included; repository checks validate rendered permissions and
workloads.

```bash
make -C charts/stacks-action-operator verify
```

Generation accepts immediate, fixed, uniform, or explicit-delay cadence; see the
[cadence contract](../../docs/network-operator/bitcoin-generation.md#cadence).
Timing and RPC execution remain in the network worker.
