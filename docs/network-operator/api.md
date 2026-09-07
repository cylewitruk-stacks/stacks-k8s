# API reference

Topology uses `network.stacks.org/v1alpha1`; Bitcoin production uses
`bitcoin.stacks.org/v1alpha1`; transfer production uses
`stacks.stacks.org/v1alpha1`. All resources are namespaced. The network CRDs
require Kubernetes 1.32+ for shared genesis admission rules.

## StacksGenesisProfile

An immutable, namespaced public recipe with a `spec` matching `StacksNetwork.spec.genesis`.
The external provisioning command copies its values into a new network; it is
not a controller or live reference. Export a profile with `kubectl get ... -o yaml`
and pass it through `--genesis-profile`. A new profile revision uses a new resource
name. See [configuration and genesis](configuration.md).

## StacksNetwork

`StacksNetwork` is the supported user entry point.

| Field | Purpose |
| ---- | ---- |
| `spec.suspended` | Scale every compiled actor to zero. |
| `spec.defaults` | Default images, pull behavior, storage, resources, and placement. |
| `spec.genesis` | Immutable public genesis balances, named accounts, epoch schedule and PoX cycle lengths. |
| `spec.bitcoinNodes` | Bitcoin Core actors and their directed peer graph. |
| `spec.stacksNodes` | Miner, follower, and signer-node actors. |
| `spec.signers` | Signer actors bound one-to-one to signer-node actors. |
| `spec.stacksTransactionProduction` | Optional fixed-interval tiny STX transfers; see the [transfer contract](stacks-production.md#policy-and-evidence). |
| `spec.bitcoinBlockProduction` | Optional weighted multi-target policy at a fixed or bounded jittered cadence; see the [production contract](bitcoin-production.md#desired-policy-and-ownership). |

`status.targetDeclarations` publishes current compiled actor intent before
workload readiness; it is not admitted inventory. `status.bitcoinProductionUID`
pins its optional production ledger across policy changes and deletion.
`status.transactionProductionUID` independently pins the transfer ledger.
`TransactionsConfigured` reports compilation/enabling, not protocol progress.
See [admission and credentials](bitcoin-production.md#admission-and-credentials).

Logical actor names must be unique across all three lists. Every Stacks node
references a declared Bitcoin node. Every signer references one distinct
`signer-node`. Kubernetes resource names and logical actor names must be DNS
labels no longer than 63 characters. The stricter bound keeps every derived
Service, StatefulSet, label value, and leaf-resource name valid.

Stacks node and signer templates may declare `serviceRefs`, a set of other
logical actors whose Services may be referenced from raw configuration. Their
direct dependencies are included automatically. Every explicit reference must
name a declared actor, and each actor may declare at most eight. The resulting
leaf `serviceMap` therefore contains only that actor's dependencies rather than
the entire network.

An image default is required only when at least one actor of that kind omits
its own `image`. Bitcoin-only and signer-free topologies need not provide
unused image defaults.

## Configuration sources

Exactly one source is required:

```yaml
config:
  generated:
    profile: nakamoto-regtest-node/v1
```

```yaml
config:
  inline:
    key: config.toml
    data: |
      [node]
      name = "example"
```

```yaml
config:
  configMapRef:
    name: actor-config
    key: config.toml
    expectedDigest: sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef
```

```yaml
config:
  secretRef:
    name: actor-config
    key: signer.toml
    expectedDigest: sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef
```

Inline configuration is stored in the CR and is visible to anyone permitted to
read it. Do not place sensitive credentials there; use `secretRef` instead.
Generated Stacks configuration is deliberately unavailable for miners and
signers. Supply a complete Secret-backed configuration for those actors.

The generated Bitcoin profile includes fixed `devnet` RPC credentials for a
disposable regtest network. They provide protocol compatibility, not a security
boundary. Apply suitable namespace isolation and use an externally managed
Secret-backed configuration when credentials must be confidential.

An omitted external digest permits mounting but cannot prove which bytes were
admitted. Its inventory value is explicitly a declaration digest. Use
`expectedDigest` when downstream identity or evidence requires byte identity.

## Leaf resources

Leaf resources contain resolved images, actor references, Services, and
workload policy. Their status has:

| Field | Meaning |
| ---- | ---- |
| `observedGeneration` | Leaf generation evaluated by its controller. |
| `phase` | `Progressing`, `Ready`, `Suspended`, or `Degraded`. |
| `ready` | Whether a complete admitted identity exists. |
| `identity` | Immutable runtime identity when ready. |
| `conditions` | Kubernetes-standard readiness diagnostics. |

Users may create an unowned leaf for advanced standalone use, but it will not
appear in a `StacksNetwork` inventory. An aggregate-owned leaf is reconciled
from its parent and must be changed through `StacksNetwork`.

Leaf `spec.actorName` values are DNS labels of at most 63 characters.
`BitcoinNode.spec.rpcPort` and `spec.p2pPort` are required and must be between
1 and 65535. Workload termination grace periods, when present, cannot be
negative. Invalid leaf declarations are rejected by the API server before a
controller can create an invalid workload.

The admitted identity includes a canonical digest of the complete leaf
specification. Topology-only changes such as signer index or weight therefore
change the aggregate inventory even when they do not require a Pod rollout.
The network and observability operators independently verify this wire
contract from the shared versioned fixtures in
[`contracts/`](../../contracts/).

`Ready=True` and `Suspended=True` are mutually exclusive aggregate conditions.
Compilation errors are terminal for the current generation: the resource is
`Degraded` until its specification changes. Transient API and observation
errors remain retriable, but clear stale readiness and admitted identity first.

## Container overrides

`container.command`, `container.args`, `container.env`, and
`container.workingDir` adapt version-specific images. For Stacks nodes and
signers, an overridden command remains behind the operator's configuration
rendering wrapper; it replaces the actor executable and arguments, not the
wrapper. These images must provide `/bin/bash`.

Custom environment names are rendered in lexical order. The following
operator-owned names cannot be overridden: `POD_IP`, `STACKS_ACTOR`,
`STACKS_ACTOR_ROLE`, `STACKS_CONFIG_RENDERED`, `STACKS_CONFIG_TEMPLATE`,
`STACKS_NETWORK`, and `STACKS_SERVICE_MAP`.

## Transaction rejection evidence

`StacksTransactionProduction.status.rejectionReason` retains a matching ingress
rejection for the last `txID`, using only `FeeTooLow`, `BadNonce`, or `Other`.
It does not release the reserved nonce or prove permanent non-execution. See the
[policy and evidence contract](stacks-production.md#policy-and-evidence).
