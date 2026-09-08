# Action operator

The action operator owns bounded-action status and finalizers. Generation is
its default capability; the previously delivered local reorganization is
optional. Each kind has its own controller. The network Bitcoin worker owns
admission, reservations, RPC dispatch and receipts in per-target
`BitcoinProductionTarget` ledgers, owned by `BitcoinBlockProduction`.
The operators communicate through versioned Kubernetes resources.

## Install

Use the same namespace for the network and action releases, with one action
release per namespace. Its replicas share a fixed namespace-local Lease;
separate releases would compete for the same leadership, including when their
enabled kinds differ. Before published images exist, build and load both
images from this checkout:

```bash
docker build -f operators/network/Dockerfile -t stacks-network-operator:local .
docker build -f operators/action/Dockerfile -t stacks-action-operator:local .
```

Provision a network using the [Bitcoin baseline guide](../network-operator/bitcoin-production.md).
Keep network action-selection flags disabled until the action CRDs are
installed. First install the action release:

```bash
helm upgrade --install stacks-action-operator charts/stacks-action-operator \
  --namespace "$STACKS_NAMESPACE" \
  --set image.repository=stacks-action-operator --set image.tag=local
```

Then enable `bitcoinGeneration.enabled=true` on the existing network release,
preserving its production credentials and other values. Create the request:

```bash
kubectl -n "$STACKS_NAMESPACE" create -f examples/actions/bitcoin-generation.yaml
kubectl -n "$STACKS_NAMESPACE" get bitcoinblockgenerations,bitcoinproductiontargets
```

The example references network `bitcoin` and leaf `bitcoin-bitcoin`; adjust
those references for a differently named network. ResourceQuota discovery may
take several minutes after the first CRD installation. Wait for quota
`status.used` before creating requests.

For reorganization, provision its method-restricted RPC profile, enable
`bitcoinReorganization.enabled=true` on the action chart first, then enable
the matching network flag. Generation can be independently disabled. See the [generation](../network-operator/bitcoin-generation.md)
and [reorganization](../network-operator/bitcoin-reorganization.md) guides for
bounds and outcome semantics.

Enabling a network action flag requires that kind’s served action API. The
worker checks discovery during setup and reports a missing prerequisite before
starting its watches. A failed setup also prevents baseline production in that
worker; this check improves diagnosis, not availability under misconfiguration.
Do not remove action CRDs while their executor flags remain enabled.

## Ownership and failure behavior

| Surface | Owner |
| --- | --- |
| Action CRDs, object quotas and lifecycle Deployment | Action chart. |
| Action status and cleanup finalizer | Action operator, for each enabled kind. |
| Production CRD, ledger and mutation credentials | Network chart and Bitcoin worker. |
| Action spec | Client; immutable after creation. |

The action ServiceAccount can read the parent and ledger, patch enabled action
objects/status, write leader-election Events, and maintain its namespaced Lease.
It cannot read Secrets, mutate the ledger, or alter actor workloads. The network
executor has read-only action access. Rendered RBAC checks enforce both sides.

An action-controller outage stops new dispatch until admission is acknowledged.
An already admitted action may continue within its original bounds under the
executor. Receipts remain in the ledger; release waits for terminal
acknowledgement. Restarting the action operator does not restart the executor,
clear reservations, or authorize replay. Generation completion acknowledges
block receipts, not global adoption. Reorganization cleanup removes its temporary
invalidation marker; it does not roll back the resulting chain.

The executor and lifecycle controller use their own wall clocks for deadlines.
Keep host clocks synchronized. Clock skew can conservatively produce an
`Inconclusive` outcome while a receipt is still being accounted. Matching
receipt acknowledgement and cleanup remain prerequisites for release; an
executor-published timing authority is deferred.

## Upgrade and removal

The initial compatibility pair is network/action `0.1.0` with
`apis/network` `v0.1.0`. Arbitrary version skew is unqualified. The cadence API and ledger extension
require matching current source builds; do not upgrade an active environment
containing requests or reservations from an incompatible schema. Provision a
fresh disposable environment for such a change. Apply changed
CRDs from the owning chart explicitly before upgrading; Helm does not upgrade
existing CRDs automatically. Never run an older bundled lifecycle writer
alongside this independent action operator.

For ordinary removal, stop submitting actions, let admitted work reach terminal
acknowledgement and ledger release, then disable the network executor's matching
selection flags before removing the action release. Keep lifecycle controllers
and executor enabled while cleanup is outstanding. Disabling or uninstalling a
controller is not cancellation. Delete an action to request cancellation.

Unresolved RPC work retains exclusion. Preserve evidence and dispose of the
owning `StacksNetwork` while its controllers are still running, then remove
releases and the namespace. This is administrative abandonment, not proven
cleanup or recovery. Use a new namespace for a replacement environment.
Baseline-only deployments need neither action controllers nor action CRDs.

See the [initial qualification record](qualification.md) for measured coverage.
