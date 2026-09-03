# Stacks observability operator

This independent chart begins the trusted-observation layer for topologies
managed by `stacks-network-operator`. The initial API verifies the admitted
Kubernetes identity of every actor through uncached API-server reads.

The first slice intentionally does not collect protocol values, logs, metrics,
or evidence bundles. It writes no topology or workload resource and has no
Secret access.

## One-shot identity observation

Run source-checkout commands from the repository root.

The default `0.1.0` image reference becomes usable when that release is
published. For an unpublished checkout, build and load a local image first:

```bash
docker build \
  -t stacks-observability-operator:local \
  operators/observability

kind load docker-image stacks-observability-operator:local --name YOUR_CLUSTER

helm upgrade --install stacks-observability-operator \
  charts/stacks-observability-operator \
  --namespace stacks-regtest \
  --set image.repository=stacks-observability-operator \
  --set image.tag=local \
  --set image.pullPolicy=Never
```

Install this chart in the same namespace as the topology operator, then create
a `NetworkObservation`:

```bash
helm upgrade --install stacks-observability-operator \
  charts/stacks-observability-operator \
  --namespace stacks-regtest

kubectl --namespace stacks-regtest apply \
  --filename examples/observability/identity.yaml

kubectl --namespace stacks-regtest get networkobservation minimal-identity
kubectl --namespace stacks-regtest get networkobservation minimal-identity \
  --output yaml
```

`Ready` means the topology was ready and every admitted leaf, Service,
StatefulSet, and Pod identity matched direct API reads. This includes ownership,
generation and revision convergence, canonical leaf-spec and inventory
digests, configuration digest, requested image, readiness, and immutable
runtime image identity. The topology UID/generation/inventory binding must also
remain stable across the complete observation. `Inconclusive` means the
operator declined to make that claim. `Pending` waits for a complete admitted
inventory.

The observer discovers resources through controller-owner UID chains. It then
checks operator labels as identity assertions, so label drift cannot hide a
retired or unexpected network-owned resource.

For a pre-bound workflow, set `spec.expectedInventoryDigest` to the digest the
caller admitted. The observation fails closed when it differs.

An observation is one-shot per resource generation. Create another resource
for another point in time. A future policy layer may schedule these resources;
that scheduling is deliberately absent from this trust-boundary slice.

Pending observations are event-driven from their referenced `StacksNetwork`
and also receive a 30-second safety reconciliation. They become `Inconclusive`
after `spec.pendingTimeoutSeconds` (300 seconds by default) rather than polling
forever. `spec.timeoutSeconds` separately bounds each direct observation read.

Leader election is enabled by default and required for multiple replicas. A
single-replica installation with leader election explicitly disabled uses a
`Recreate` Deployment strategy to prevent old and new writers overlapping.

The manager exposes unauthenticated controller metrics on Pod port 8080. This
chart does not create a metrics Service; expose the port only behind
namespace-appropriate access controls. The chart deliberately avoids the
cluster-scoped TokenReview and SubjectAccessReview permissions required by
controller-runtime's authenticated metrics filter.
