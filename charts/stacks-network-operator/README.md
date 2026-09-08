# Stacks network operator

`stacks-network-operator` creates and maintains disposable Stacks regtest
networks on Kubernetes. It is a standalone topology product: it does not
install fault injection, Attacknet run orchestration, trusted observation, or
evidence collection. It also does not impose a NetworkPolicy; operators or a
higher-level test system own deployment-specific connectivity and containment.

## Install

CRD admission requires Kubernetes 1.32+ for bounded genesis account-name
validation. Live qualification uses Kubernetes 1.37.0. Run source-checkout
commands from the repository root.

The default `0.1.0` image reference becomes usable when that release is
published. For an unpublished checkout, use the local-image procedure below.

The chart installs one operator, watching all namespaces by default. Set
`controller.watchNamespace` to a single namespace for a scoped installation.
Leader election remains in the Helm release namespace. Install once, then create
network declarations and their referenced Secrets/ConfigMaps in fresh namespaces.
Do not run overlapping operator installations.

Capability controllers create worker Deployments on demand. Bitcoin execution
uses the operator image; Stacks execution uses `workers.sdkImage`. Network
production policies select their same-namespace `credentialsSecret`, rather than
putting network credentials in Helm values. See the
[workload architecture](../../docs/design/operator-workloads.md).

`bitcoinProduction.enabled`, `stacksTransactions.enabled` and
`stacksOperation.enabled` default to true and control capability reconciliation.
Disabling them stops the corresponding provisioning controllers; the Bitcoin
switch also stops new baseline scheduling. It does not revoke existing workers.
An existing Bitcoin worker can still perform declared initialization up to
`initialHeight` without a scheduled opportunity; network pause stops new
initialization dispatches as well as baseline production. Use the network's explicit pause policy to stop new
production while allowing outstanding evidence collection. A topology-only
network creates no production workers without capability declarations.

Workers have individual ServiceAccounts, namespace-bounded reads and named ledger
writes. They cannot read Secrets through the API or mutate workloads. Only the
assigned workers mount signing/RPC keys. Bitcoin receipt collection retains the
existing 25-second drain inside a 45-second Pod termination grace period.

Leader election is enabled by default and is required when `replicaCount` is
greater than one. If explicitly disabled for a single-replica installation,
the Deployment uses `Recreate` to reduce process overlap during an upgrade;
ledger admission remains the execution authority.
`controller.logLevel` accepts `debug`, `info`, `error`, or `panic`.

```bash
helm upgrade --install stacks-network-operator \
  charts/stacks-network-operator \
  --namespace stacks-network-system \
  --create-namespace
```

For a local image, build and load it before installing:

```bash
docker build \
  --file operators/network/Dockerfile \
  -t stacks-network-operator:local \
  .

docker build -f operators/network/transactions/Dockerfile -t stacks-transaction-worker:local .
kind load docker-image stacks-network-operator:local stacks-transaction-worker:local --name stacks-k8s

helm upgrade --install stacks-network-operator \
  charts/stacks-network-operator \
  --namespace stacks-network-system \
  --create-namespace \
  --set image.repository=stacks-network-operator \
  --set image.tag=local \
  --set workers.sdkImage.tag=local \
  --set image.pullPolicy=Never
```

Use the explicitly selected kind cluster name in place of `stacks-k8s`.

The topology examples also reference actor images. Build and load
`stacks-core-network:local`, or edit the example to use an immutable image
available to the cluster, before applying it.

## Create a network

Edit the actor image in [the minimal example](../../examples/network/minimal.yaml), then
create its namespace and apply it:

```bash
kubectl create namespace stacks-regtest
kubectl --namespace stacks-regtest apply \
  --filename examples/network/minimal.yaml

kubectl --namespace stacks-regtest get \
  stacksnetworks,bitcoinnodes,stacksnodes,stackssigners
```

The minimal example creates one Bitcoin node and one Stacks follower. Bitcoin
block generation is configured separately through production or action resources.

## API model

Users normally submit one `StacksNetwork`. The aggregate controller compiles
it into small, owned resources:

| Kind | Owns |
| ---- | ---- |
| `StacksNetwork` | Desired actor graph, leaf CRs, aggregate readiness, admitted inventory. |
| `BitcoinBlockProduction` | Optional fixed-interval weighted Bitcoin production policy. |
| `BitcoinProductionTarget` | Internal per-target dispatch, receipt, and action-exclusion ledger. |
| `StacksTransactionProduction` | Optional fixed-interval STX demand and bounded account/transaction ledger. |
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

Delete networks and wait for the Bitcoin policies, all retained Bitcoin target
ledgers, and Stacks transaction ledgers to disappear before uninstalling the
controllers. Continue only after the deletion wait succeeds:

```bash
kubectl --namespace stacks-regtest delete stacksnetworks --all
kubectl --namespace stacks-regtest wait --for=delete \
  bitcoinblockproductions,bitcoinproductiontargets,stackstransactionproductions \
  --all --timeout=120s
helm --namespace stacks-network-system uninstall stacks-network-operator
```

Helm intentionally does not delete installed CRDs. Remove them only after all
resources are gone and no other release uses them.

## Optional finite Bitcoin generation

First install the action chart in the same namespace. Then enable
`bitcoinGeneration.enabled` together with `bitcoinProduction.enabled` on the
network release to use bounded `BitcoinBlockGeneration` actions. The network
chart adds read-only action Role rules; the action chart owns the 64-object
namespace quota and lifecycle permissions.
See the [operating profile](../../docs/network-operator/bitcoin-generation.md)
for immutable fields, admission, cancellation, attribution, and recovery.

## Optional local reorganization

`bitcoinReorganization.enabled` adds bounded local suffix replacement to the
shared executor and requires `bitcoinProduction.enabled`. Provision the helper's
explicit `--reorganization` RPC profile in a fresh environment first.
See the [operating guide](../../docs/network-operator/bitcoin-reorganization.md)
for depth/boundary limits, retained cleanup, cancellation, and teardown.

## Action lifecycle deployment

`bitcoinGeneration.enabled` and `bitcoinReorganization.enabled` enable executor
selection only. First install the [action chart](../stacks-action-operator/README.md)
with matching controllers in the same namespace, then enable the network flags.
That chart owns action CRDs, quotas, status and finalizers. The network worker
has read-only action access.
Baseline-only installation needs no action chart or action CRDs.

Baseline `jitterSeconds` and finite generation cadence are API fields, not Helm
scheduling settings. See the [baseline](../../docs/network-operator/bitcoin-production.md)
and [finite cadence](../../docs/network-operator/bitcoin-generation.md#cadence) contracts.

The chart also installs `StacksGenesisProfile`, an immutable public recipe for
external provisioning. It has no controller or runtime profile lookup. Network
genesis snapshots are immutable; see [configuration and genesis](../../docs/network-operator/configuration.md).

## Managed protocol worker

`StacksContractSet` and `StacksStackingParticipant` each own a separate Go worker
Deployment using `workers.sdkImage`. They mount only their declared account keys;
stacking also mounts the consensus authorization key. The consensus signer binary
continues to run in its independently owned actor StatefulSet.

Each managed network owns a keyless legacy receipt Deployment and Service named
`<network>-receipts` (long names use the shared naming hash). Generated ingress
configuration points to this service. Custom ingress configurations must provide
the same callback destination and native transaction indexing. The receiver
checks network UID, exact transaction bytes, current ingress Pod identity and
canonical legacy ancestry before accounting an existing authorization.

See [managed operation](../../docs/design/managed-network-operation.md) and
[PoX-5 operation](../../docs/network-operator/pox5.md). Keep the shared operator
installed until all networks and retained ledgers have been disposed of.

### Worker registry credentials

Set `workers.pullSecrets` to image-pull Secret names provisioned in each network
namespace. `image.pullSecrets` applies to the operator installation namespace.
The operator references these Secrets without reading or copying their contents.
Worker resource requests, limits and placement currently use built-in defaults;
operator chart scheduling settings apply only to the operator Pod.

### Upgrading execution workers

The operator image also runs Bitcoin and receipt workers. Changing it can roll
those workers across every managed namespace; changing `workers.sdkImage` rolls
transfer, contract and stacking workers. Before such upgrades, pause managed
operation and transfers through each network's policies, leave Bitcoin running
until outstanding transactions are accounted, then pause Bitcoin and wait for
outstanding dispatch receipts. Keep the operator and workers running during drain.

Upgrade only after the ledgers have reached acknowledged boundaries. An unknown
outcome cannot be drained into certainty; retain its evidence and follow the
capability's documented recovery model. Resume the latest desired policies after
the new workloads are available. Pod replacement and the 25-second Bitcoin drain
limit do not establish server-side quiescence.
