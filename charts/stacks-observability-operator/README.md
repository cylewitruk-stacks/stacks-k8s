# Stacks observability operator

This independent chart begins the trusted-observation layer for topologies
managed by the network operator. New installations select the `v1alpha2`
foundation API. The initial API verifies the admitted
Kubernetes identity of every actor through uncached API-server reads.

The identity reader does not collect protocol values, logs, metrics,
or evidence bundles. It writes no topology or workload resource and has no
Secret access.

## One-shot identity observation

Run source-checkout commands from the repository root.

The default `0.1.0` image reference becomes usable when that release is
published. For an unpublished checkout, build and load a local image first:

```bash
docker build \
  --file operators/observability/Dockerfile \
  -t stacks-observability-operator:local \
  .

kind load docker-image stacks-observability-operator:local --name YOUR_CLUSTER

helm upgrade --install stacks-observability-operator \
  charts/stacks-observability-operator \
  --namespace stacks-regtest \
  --set image.repository=stacks-observability-operator \
  --set image.tag=local \
  --set image.pullPolicy=Never
```

Install this chart in the namespace of the network to observe, then create
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

For `network.stacks.org/v1alpha2`, `Ready` means every
selected Bitcoin node, Stacks node and signer has a directly verified current
participant/workload/Pod/container identity. The reader checks allocation UIDs,
controller ownership, complete admitted policy digest, requested and resolved
images, readiness, rollout revision, Service routing, mounted configuration name
and public configuration annotations. It re-reads the collected objects before
publishing. Unrelated management workers and the root's `Operational` condition
do not gate this physical actor observation.

`status.binding.snapshotDigest` belongs to the observation snapshot, not the
network. `spec.expectedSnapshotDigest` optionally pins a previously observed
snapshot. Service UID and container changes produce a different digest. An
observation is a bounded series of reads, not an atomic Kubernetes snapshot;
read-time drift returns `Inconclusive`. Missing or unfinished actors remain
`Pending` until the configured deadline.

Private configuration content and Secret UID are **controller-reported** evidence,
corroborated against the public resolver ConfigMap. They are marked separately
from the directly observed Pod mount and annotation identity. Secret objects are
never read, and this chart grants no Secret permission. The snapshot is evidence,
not authority to dispatch actions or proof of protocol health.

The final consistency pass compares identity-bearing inputs, including declarations,
allocation, admission, runtime bindings, workload state and configuration reports.
Root progress conditions and participant protocol/execution heartbeats do not change
actor identity. Replacement, deletion or a changed consumed input makes the snapshot
inconclusive.

The reader supports only the participant API. Legacy `expectedInventoryDigest`
requests become `Inconclusive` without observation; use `expectedSnapshotDigest`.
Previously completed observations remain unchanged.

An observation is one-shot per resource generation. Create another resource
for another point in time. An external agent may create observations whenever
its investigation needs them; this operator does not schedule observations or
direct an experiment.

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
