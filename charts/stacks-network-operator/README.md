# Stacks network operator

`stacks-network-operator` creates and maintains disposable Stacks regtest
networks on Kubernetes. It is a standalone topology product: it does not
install fault injection, Attacknet run orchestration, trusted observation, or
evidence collection. It also does not impose a NetworkPolicy; operators or a
higher-level test system own deployment-specific connectivity and containment.

## Install

Run source-checkout commands from the repository root.

The default `0.1.0` image reference becomes usable when that release is
published. For an unpublished checkout, use the local-image procedure below.

The chart watches only its release namespace.

Optional [Bitcoin baseline production](../../docs/network-operator/bitcoin-production.md)
runs in a separate Deployment and ServiceAccount. Set
`bitcoinProduction.enabled=true` after provisioning its immutable static RPC
profile and `bitcoinProduction.credentialsSecret` (default
`stacks-bitcoin-production-rpc`). Production has its own leader election and a
45-second shutdown grace period. The default chart remains topology-only.
The chart also passes this setting to the topology controller so a configured
policy reports `ProductionConfigured=False` / `ControllerDisabled` when disabled.
This condition describes configuration, not producer health. The producer's
32 receipt slots and 25-second drain are fixed initial-profile limits. Its
ConfigMap `list` permission is required by the API-readiness probe.

Leader election is enabled by default and is required when `replicaCount` is
greater than one. If explicitly disabled for a single-replica installation,
the Deployment uses `Recreate` so an upgrade cannot overlap two writers.
`controller.logLevel` accepts `debug`, `info`, `error`, or `panic`.

```bash
helm upgrade --install stacks-network-operator \
  charts/stacks-network-operator \
  --namespace stacks-regtest \
  --create-namespace
```

For a local image, build and load it before installing:

```bash
docker build \
  --file operators/network/Dockerfile \
  -t stacks-network-operator:local \
  .

kind load docker-image stacks-network-operator:local --name attacknet

helm upgrade --install stacks-network-operator \
  charts/stacks-network-operator \
  --namespace stacks-regtest \
  --create-namespace \
  --set image.repository=stacks-network-operator \
  --set image.tag=local \
  --set image.pullPolicy=Never
```

Use the actual kind cluster name in place of `attacknet`.

The topology examples also reference actor images. Build and load
`stacks-core-network:local`, or edit the example to use an immutable image
available to the cluster, before applying it.

## Create a network

Edit the actor image in [the minimal example](../../examples/network/minimal.yaml), then
apply it:

```bash
kubectl --namespace stacks-regtest apply \
  --filename examples/network/minimal.yaml

kubectl --namespace stacks-regtest get \
  stacksnetworks,bitcoinnodes,stacksnodes,stackssigners
```

The minimal example creates one Bitcoin miner and one Stacks follower. Mining
Bitcoin blocks is intentionally outside the topology operator; another client
or a fault-testing system can drive the regtest RPC endpoint.

## API model

Users normally submit one `StacksNetwork`. The aggregate controller compiles
it into small, owned resources:

| Kind | Owns |
| ---- | ---- |
| `StacksNetwork` | Desired actor graph, leaf CRs, aggregate readiness, admitted inventory. |
| `BitcoinNode` | One Bitcoin Core StatefulSet, Service, configuration, and actor status. |
| `StacksNode` | One Stacks node StatefulSet, Service, configuration, and actor status. |
| `StacksSigner` | One Stacks signer StatefulSet, Service, configuration, and actor status. |

The leaf CRDs are observable implementation resources. Do not edit a leaf
owned by a `StacksNetwork`; the aggregate controller restores its compiled
specification. Controllers never read Secret contents.

See the [API reference](../../docs/network-operator/api.md),
[architecture](../../docs/network-operator/architecture.md),
[operations guide](../../docs/network-operator/operations.md), and
[Hacknet migration notes](../../docs/network-operator/migration.md).

## Change a topology

Update the `StacksNetwork` specification. Adding or removing an actor creates
or retires the corresponding leaf resource. Image and configuration changes
roll the affected StatefulSet.

```bash
kubectl --namespace stacks-regtest edit stacksnetwork minimal
```

Storage shape, StatefulSet selector, and service identity are immutable.
Changing them fails visibly rather than deleting persistent data. Replace the
actor under a new logical name when such a change is intentional.

Set `spec.suspended: true` to scale every actor to zero without deleting the
topology. A suspended network has no admitted inventory digest.

## Configuration

Generated profiles are available for a disposable Bitcoin regtest node and a
non-mining Stacks node. The Bitcoin profile contains fixed, public development
RPC credentials; they are configuration defaults, not secrets. Miners and
signers must use Secret-backed full configuration because their keys must not
be stored in `StacksNetwork`.

Raw configuration may use `${SERVICE:<logical-actor-name>}` and `__NODE_IP__`
placeholders. A Stacks node or signer must list additional placeholder targets
under its `serviceRefs`; direct Bitcoin, node, and signer dependencies are
included automatically. This per-actor dependency set prevents an unrelated
actor addition from rolling every workload. Stacks actor containers render
the values immediately before starting and therefore must provide `/bin/bash`.
An external configuration can declare `expectedDigest`; an init container then
verifies the mounted bytes and a digest change triggers a rollout.
Rendered Stacks node and signer configuration is held in a size-bounded,
memory-backed `emptyDir` so Secret-derived content is not copied into ordinary
node ephemeral storage. Its pages count against Pod memory; reserve at least
the 2 MiB volume limit in the actor's memory request and limit.

## Metrics exposure

The optional metrics Service is enabled by default and serves controller
metrics over unauthenticated HTTP inside the namespace. Disable it with
`--set metrics.service.enabled=false` when it is not needed, or restrict access
with namespace-appropriate NetworkPolicy. The chart intentionally avoids the
cluster-scoped TokenReview and SubjectAccessReview permissions required by
controller-runtime's authenticated metrics filter.

## Admitted inventory

Each leaf publishes identity only after exactly one Ready Pod exists with a
runtime image digest and a converged StatefulSet revision. `StacksNetwork`
publishes `status.inventoryDigest` only when every desired actor has such an
identity. Partial rollout, suspension, or actor replacement withdraws or
changes the digest.

Observation time and Kubernetes resource versions are not digest inputs.

## Uninstall

Delete networks before uninstalling the controller:

```bash
kubectl --namespace stacks-regtest delete stacksnetworks --all
helm --namespace stacks-regtest uninstall stacks-network-operator
```

Helm intentionally does not delete installed CRDs. Remove them only after all
resources are gone and no other release uses them.
