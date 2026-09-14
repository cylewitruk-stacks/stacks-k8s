# Continuous network telemetry

`NetworkTelemetry` records a selected `StacksNetwork` in GreptimeDB through scoped
Go recorders and node-local OpenTelemetry collectors. Installation is optional:
networks and actions work without it. Agents query retained facts with read-only
HTTP SQL; MCP integration is low priority. See the [qualified profile and limits](qualification.md).

## Installation

Use `kind-stacks-k8s`, created with the repository's current configuration. It maps
host `127.0.0.1:14000` to NodePort `30400` for optional dashboard access. Existing
clusters without this mapping require recreation or a manually managed port-forward.
The local profile installs the operator and Greptime in one Helm release; the default
chart installs only the operator for use with an independently managed backend.

Run repository commands from its root:

```bash
export KUBECONFIG="$PWD/tools/local-cluster/kubeconfig"
kubectl config current-context # kind-stacks-k8s
kubectl create namespace stacks-observation-system
```

Create `greptime-auth` in that namespace, with a `passwd` key containing three
separately generated credential lines. Keep passwords outside Git and use a protected
local file:

```text
admin:readwrite=<admin-password>
ingest:writeonly=<ingestion-password>
reader:readonly=<query-password>
```

```bash
kubectl create secret generic greptime-auth -n stacks-observation-system \
  --from-file=passwd=/path/to/private/passwd
```

For an unpublished checkout, build and load the operator image before installation.
Use a fresh tag when its source changes:

```bash
docker build -f operators/observability/Dockerfile \
  -t stacks-observability-operator:local .
kind load docker-image stacks-observability-operator:local --name stacks-k8s
make -C deploy/telemetry install \
  HELM_ARGS='--set image.repository=stacks-observability-operator --set image.tag=local'
```

The helper uses Helm to fetch the canonical dependency pinned in `Chart.lock`, with
a temporary repository configuration and index cache. Personal repository settings
are unaffected. Downloaded archives are ignored by Git.
To install directly with Helm, first run
`make -C charts/stacks-observability-operator dependencies`, then use
[values-local.yaml](../../charts/stacks-observability-operator/values-local.yaml).
The pinned backend is GreptimeDB 1.2.0 (upstream chart 0.4.14), with a 10 GiB PVC,
2 GiB memory limit and bounded query concurrency/memory. It requires the local
cluster's storage provisioner. This is a single-instance development backend.

A bounded post-install/post-upgrade Job runs `/backend-init` from the operator image.
It mounts the existing administrator credential and has no Kubernetes API token or
RBAC permissions. It sets the dedicated `public` database and any existing physical
metric table to 24-hour retention. Failed initialization fails the Helm operation;
recordings should start only after it succeeds. This administrator operation is not
part of the observation controller or its scoped recording workers.

Open the [dashboard](http://127.0.0.1:14000/dashboard/) and select **Visualization**
for the embedded Perses dashboard. In its connection settings, set Host to `http://127.0.0.1:14000`,
Database to `public`, Username to `reader`, and Password to the reader credential,
then select **Save and Test**. Retrieve only that password in your local terminal:

```bash
kubectl -n stacks-observation-system get secret greptime-auth \
  -o jsonpath='{.data.passwd}' | base64 --decode | sed -n 's/^reader:readonly=//p'
```

A missing-authorization-header toast before login means a data query was sent
without credentials; serving the UI does not bypass query authentication.
The page and HTTP API share port 4000: this exposes the backend HTTP interface,
not just static UI assets.
No separate Perses deployment or background port-forward is required. See the
[dashboard qualification](dashboard-qualification.md) for tested lifecycle behavior. Host exposure
is loopback-only with our kind mapping; NodePort remains reachable on cluster nodes.

Set `dashboard.enabled=false` to remove the NodePort Service while keeping the
backend and its internal Service. Changing `dashboard.nodePort` also requires a
matching kind host mapping. Changing an existing Docker mapping requires cluster
recreation. The mapping survives `make cluster-stop` / `make cluster-start`.

### Independently managed backend

Omit the local values file and leave `greptime.enabled=false`. Install/configure the
backend separately, then install this operator with your enrolled namespaces:

```bash
make -C charts/stacks-observability-operator dependencies
helm upgrade --install telemetry charts/stacks-observability-operator \
  -n stacks-observation-system \
  --set image.repository=stacks-observability-operator --set image.tag=local \
  --set 'watchNamespaces[0]=my-experiment'
```

Enrolled namespaces must exist. An empty `watchNamespaces` list observes only the
installation namespace. Set the same option on the bundled installation to enroll
experiment namespaces; this does not install another operator per network.
`dashboard.enabled` is only supported for the bundled backend. Expose an external
backend through its own installation. For a dedicated external backend, the explicit
administrator initializer remains available:

```bash
GOWORK=off go -C operators/observability run ./cmd/backend-init \
  --endpoint http://your-backend:4000 --auth-file /path/to/private/passwd
```

Do not run this against a shared production database. Metric ingestion does not set
TTL from request hints; log/object tables use each recording's retention window.
An existing independent backend is not automatically adopted when enabling the
bundled dependency; keep it external or plan its storage migration explicitly.

Enrollment grants namespace-scoped workload/RBAC management for recording resources;
there is no cluster-wide mutation role. Controllers verify ownership before writes.
Source Pods, networks, participants and actions have read-only access. The one-shot
`NetworkObservation` additionally reads Services and StatefulSets. When the installation
namespace is not enrolled, its permissions cover only readiness and leader election.
Neither manager nor recorder can read Secrets through the API. The kubelet mounts
only the explicitly selected backend credential into recording workloads.
Recorders have name-scoped telemetry-status writes; node collectors only read Pods
through the API. Their trusted DaemonSet also mounts host Pod logs read-only and a
UID-specific checkpoint directory read-write; it is not a tenant-isolation boundary.

## Declare collection

Create `telemetry-storage` in the experiment namespace with these `stringData` keys:

| Key | Value |
| --- | --- |
| `endpoint` | `http://greptime.stacks-observation-system.svc:4000` |
| `authorization` | `Basic <base64(ingest:password)>` |

The endpoint must be an HTTP(S) base URL without credentials, query or path.
Use the reader credential only for investigation clients; it is not mounted in
collectors. The backend and its credentials are trusted administrator inputs. Use
immutable backend Secrets; choose a new recording when changing the destination.

Resolve the current root UID and substitute it into this resource:

```yaml
apiVersion: observation.stacks.org/v1alpha2
kind: NetworkTelemetry
metadata:
  name: capture
  namespace: my-experiment
spec:
  networkName: network
  networkUID: "00000000-0000-0000-0000-000000000000"
  storageSecretRef: telemetry-storage
  retention:
    window: 24h
  sources:
    objects: true
    logs: true
    metrics: true
```

An exact live root UID is required for initial admission. The telemetry resource has no network
owner reference. It owns one recorder Deployment and namespace-scoped identities. Logs or native
metrics also provision a collector DaemonSet and configuration; objects-only recordings do not.
It continues recording after root deletion; a same-name replacement never inherits membership.
Independent recordings have separate log/object tables; native metric series include recording
identity to distinguish overlapping subscriptions. Metric `source` separates `actor-native` from
`collector-internal`; target relabeling preserves identity on synthetic `up` samples as well as
native values. The Prometheus receiver maps `job` into resource metadata; queries use the
explicit source tag.

Sources are mutable; changing them rolls recording workloads and creates a capture
boundary. Disabling both logs and metrics removes the node collectors and their identities.
Recorder health remains enabled; collector self-metrics are recorded only while collectors
exist. Network identity, backend Secret reference and retention window are
immutable. Create a new recording to change them. Removing namespace enrollment
stops controller reconciliation; delete its telemetry resources before unenrolling.

## Sources and evidence limits

| Source | Retained facts | Boundary |
| --- | --- | --- |
| Kubernetes list/watch | Root, participant, genesis, Bitcoin execution, action and native fault objects; Pods and attributable Events | Snapshots are current state, not missed transitions. Missing optional APIs produce unavailable coverage. Events are attributed only after their object UID is known. |
| Container logs | Bounded stdout/stderr with Kubernetes-derived network, participant and Pod identity | Missing Pod metadata is filtered rather than guessed. Actor text remains an actor claim. |
| Native metrics | Named `metrics` ports on selected Pods, scraped once per node every 10 seconds | Scrape labels and scrape time come from the collector; metric values are actor reports. Unsupported images and unreachable endpoints do not produce native samples. |
| Collection health | Recorder heartbeats, watch/export gaps, collector session changes and failure counters | A gap indicates possible loss or duplicates, not an exact missing-record count. |

`WorkloadsReady=True` describes Kubernetes workload readiness only.
`status.recording.heartbeatAt` must be recent before trusting its source summaries;
`backendReady` describes the recorder's latest export acknowledgement, not every collector
pipeline. Heartbeats assert recent activity, not uninterrupted coverage. Missing ready
collectors on active actor nodes make collector coverage unavailable, including nodes excluded
by custom taints. The local profile tolerates only the control-plane taint. Source `observedAt`
is the latest source health observation, not a complete-history watermark. A quiet watch may
have an older timestamp until a bookmark or reconnect. Collector metrics provide queue/failure
detail.

The recorder re-lists after watch interruptions and records a conservative gap.
Recorder restart begins a new session with an explicit coverage boundary. Kubernetes
watches never guarantee every intermediate write, and Events for unknown UIDs can
be absent. Event attribution tracks at most 4,096 source UIDs per recorder process.
Capacity exhaustion emits a gap; no LRU eviction silently changes historical attribution.
No complete journal, causal ordering or automatic diagnosis is claimed.

Log collectors retain file offsets and request-count-bounded export queues under
`/var/lib/stacks-telemetry/<telemetry-uid>` on each node. They survive collector Pod
replacement on that node, not node destruction. Metrics-only collectors run non-root
without host mounts and use a 512 MiB ephemeral checkpoint volume; their queues do not
survive Pod replacement. Queue length is not a byte quota, and hostPath usage is not
covered by Pod ephemeral-storage limits. Export retry lasts at most 120 seconds;
prolonged outage, log rotation and capacity exhaustion can lose records. Source
configuration, scrape limits, recorder request deadlines and Pod resource limits
bound collection work. Observation failure never pauses network operation.

Structured observations omit arbitrary annotations, configuration bodies and private
field classes. Pod specs retain requested container images, not environment or volume
contents. Credential-bearing diagnostic/log lines matching the documented password,
private-key, authorization, auth-token or seed assignment patterns are redacted before
export. Raw records are capped at 64 KiB; oversized object bodies retain metadata only.
This is a fixed best-effort redaction profile, not a detector for every possible secret
encoding. Do not deliberately log credentials; review custom actor logging.

## Retention and investigation

`status.tablePrefix` identifies `<prefix>_objects` and `<prefix>_logs`.
`retention.window` selects their creation-time TTL (`1h`, `6h` or `24h`). Native
metric tables are shared and use the backend profile's fixed 24-hour retention.
Use a dedicated backend initialized with this profile; verify table TTLs before
reusing an existing database. TTL is neither a per-network byte quota nor immediate
physical reclamation. The PVC and resource limits bound the installation; monitor
storage pressure. Per-network byte quotas are not exposed in this slice.

Logs and object records promote `network_uid`, `participant_uid`, `pod_uid`, `object_uid`,
`event_type` and `source` into tags. Resource source IDs use `resource.group/version`, with
`core` for the empty group (for example `pods.core/v1`). Objects retain resource versions,
public status and configuration/image identities in redacted JSON. Repeated snapshots and retry
ambiguity can duplicate facts; the query examples group object UID/resourceVersion and event
type when counting distinct observed facts. Log replay can duplicate lines, but identical text
can also be legitimate repetition. Collector observation time changes on replay and is not a
deduplication key; raw log counts are not exact event counts. Log source timestamps and
collector observation timestamps are separate and do not establish a global causal clock.

The local profile serves HTTP at `http://127.0.0.1:14000`; no tunnel is needed.
Use the reader credential for `POST /v1/sql` with form field `sql`.
[Investigation queries](queries.sql) demonstrate bounded identity/time filters,
progress comparison, fault timelines and source gaps. Substitute the recorded table
prefix, UID and absolute experiment window. Start with a one-minute log window
and a participant filter; widen in bounded steps. `LIMIT` alone does not bound scan cost.
The first slice uses backend-native SQL; custom query services, `EvidenceExport`,
distributed tracing and additional native protocol polling remain future work.

## Cleanup

Stop and delete the experiment using the network lifecycle. Its namespace can then
be removed; retained data remains in the backend database. Query or
export selected records before their TTL expires. Deleting `NetworkTelemetry` removes
its workloads without deleting backend tables and without guaranteeing queue drain.

After collectors stop, remove only their UID-specific checkpoint directory on each
node when no longer needed. To uninstall the bundled operator/backend release:

```bash
make -C deploy/telemetry uninstall
```

This retains the database PVC. Delete the PVC or backend namespace separately only
when its retained telemetry is no longer needed. Leave the shared kind cluster running.

## Backend contract qualification

With the backend HTTP endpoint reachable and the protected credential file available, run the
opt-in ingestion/query test. It creates one TTL-limited test table and checks
identical-timestamp retention plus redaction; it does not mutate a network.

```bash
STACKS_TELEMETRY_LIVE=1 \
STACKS_TELEMETRY_ENDPOINT=http://127.0.0.1:14000 \
STACKS_TELEMETRY_AUTH_FILE=/path/to/private/passwd \
GOWORK=off go -C operators/observability test -tags=live -race \
  ./internal/recorder -run '^TestGreptimeAppendAndRedaction$' -count=1 -v
```

The query regression is separately opt-in: `TestDocumentedQueries` requires a populated
recording with a known scrape failure and recovery. Set `STACKS_TELEMETRY_QUERY_LIVE=1`,
the same endpoint/auth-file variables, and `STACKS_TELEMETRY_PREFIX`, `NETWORK_UID`,
`TELEMETRY_UID`, `PARTICIPANT_UID`, `FROM`, and `TO` (each prefixed
`STACKS_TELEMETRY_`). The absolute UTC window must be at most fifteen minutes. It
executes every documented template, requires nonempty results, verifies both `up=0`
and `up=1` for the selected target, and checks nonempty resource versions in the
deduplicated inventory. Synthetic canary results are ingestion evidence, not protocol
qualification. `make verify` checks component references and shared policy rendering;
`make docker-check` also validates all source combinations with the pinned collector.
