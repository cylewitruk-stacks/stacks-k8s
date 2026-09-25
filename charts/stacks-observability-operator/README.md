# Stacks observability operator

This independently installed operator supports one-shot identity observations and
continuous `NetworkTelemetry` recording. See the [telemetry guide](../../docs/observability/README.md)
for the GreptimeDB backend, collection policy, queries and cleanup.

`watchNamespaces` explicitly enrolls namespaces for one operator installation; empty
selects the installation namespace. Roles are namespaced. The manager can manage
recording Deployments, DaemonSets, ConfigMaps, ServiceAccounts and Roles only there;
ownership checks prevent adoption. Source Pods and protocol resources
are read-only. Native fault observation covers the namespaced Chaos Mesh 2.8.4
fault and orchestration resources listed in the
[telemetry guide](../../docs/observability/README.md). No Secret API access is granted.
Backend credentials are mounted into scoped recorder/collector workloads.
The recorder status Role names its exact telemetry
resource. No ClusterRole or cluster-wide mutation permission is installed.

Changing enrollment does not delete existing collectors. Remove `NetworkTelemetry`
resources before unenrolling a namespace. The backend is independent of recordings;
a bundled backend shares this Helm release lifetime.
Helm installs files in `crds/` only on first installation. Before upgrading an
existing release to the expanded Chaos Mesh watch inventory, apply the generated
`crds/observation.stacks.org_networktelemetries.yaml` with the intended context;
`status.recording.sources` must allow 48 entries before the new recorder runs.
Objects-only recordings create no node collectors. Log-enabled OTel collectors use
host log mounts and UID-scoped node checkpoint directories;
the node-local process runs as root to read container logs, drops all capabilities
and does not use privileged mode or host networking. Metrics-only collectors run
non-root with an ephemeral queue volume and no host mounts. The one-shot
`NetworkObservation` also reads Services and StatefulSets. An unenrolled installation
namespace receives only readiness and leader-election permissions.
Every admitted `NetworkTelemetry` recorder reads namespace-scoped PodMetrics and
retains CPU/memory samples for exact actor Pod and participant UIDs. This remains
enabled when logs or native metrics are disabled and requires Metrics Server.

## Optional backend and dashboard

`greptime.enabled=false` installs only the operator at runtime. Enable the
canonical `greptimedb-standalone` dependency with `greptime.enabled=true`; its version
is locked in `Chart.lock`. Run `make dependencies` before rendering/installing from
this directory. It runs Helm's standard repository and dependency commands with a
temporary repository configuration and index cache, leaving personal repository
settings untouched. Helm's content cache uses its normal location. Downloaded
archives are ignored by Git.
Helm requires the dependency even when the backend is disabled; preparing a source
checkout can require network access. Packaged releases include the dependency.

The bundled profile requires `greptime.auth.existingSecretName` (default
`greptime-auth`) containing a `passwd` file with an `admin:readwrite` entry and scoped
reader/ingestion entries. No passwords are generated or stored in Helm values.
A bounded, tokenless post-install/post-upgrade Job initializes 24-hour retention
using the same operator image. Set `backendInitialization.enabled=false` only when
retention is administered separately.

`dashboard.enabled=true` creates an HTTP-only NodePort Service; `dashboard.nodePort`
defaults to 30400. Greptime's other protocol ports remain ClusterIP-only. The
[local values](values-local.yaml) enable the backend and dashboard and place the
backend on the kind control-plane node. With the repository's kind mapping, open
`http://127.0.0.1:14000/dashboard/` without a port-forward. This port exposes the
HTTP API as well as embedded Perses; backend query authentication still applies.

The NodePort Service is removed when disabled. Helm cannot modify Docker's host-port
mappings; existing clusters without the mapping require recreation. On other cluster
providers, configure host/network access separately. Backend PVCs are retained on
uninstall by default; destroying the kind cluster removes their data. Enabling this
dependency does not adopt an already installed backend from another Helm release.

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

Install this chart in the network namespace, or enroll it through `watchNamespaces`,
then create a `NetworkObservation`:

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
