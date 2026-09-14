# Passive observability design

## Purpose and boundary

The observability layer is the agent's passive recorder and query surface. It
collects topology mutations, desired-operation and action lifecycles,
Kubernetes events, protocol facts, logs, metrics, and capture gaps so an
external agent can investigate an issue and attempt a careful replay or
reduction itself.

It is read-only with respect to observed networks, actions, and workloads. It
may write its own CRDs, status, journal, object-store artifacts, and telemetry
backend. “Passive” permits administrator-configured polling and subscriptions
at fixed, bounded rates; it forbids action-driven capture escalation and every
mutation of the observed environment. The operator never schedules an
experiment, replay, diagnosis, or follow-up action. Before/during/after views
are query-time correlation over continuously collected facts.

Distributed execution is not claimed deterministic. The record answers what
was requested and observed, with provenance and gaps—not what must happen on a
future run.

## Current implementation

`NetworkObservation` is a one-shot identity snapshot against the public participant
API. `NetworkTelemetry` in `observation.stacks.org/v1alpha2` continuously records
public Kubernetes facts, logs, native metrics and collection health in GreptimeDB.
Neither grants mutation authority over observed networks or protocol actors.

See the [continuous telemetry guide](../observability/README.md) for the served
resource, installation, storage schema, source bounds and query examples. The
[public API package](public-api/resources.md#optional-action-and-observation-resources)
defines how these optional resources relate to network instances.

## Resources and services

| Component | Form | Delivery |
| --- | --- | --- |
| `NetworkObservation` | CRD | One-shot identity verification. |
| `NetworkTelemetry` | CRD | Exact-UID continuous recording and bounded status. |
| GreptimeDB | Independently installed backend | Retained logs, public object observations and native metrics; read-only HTTP SQL. |
| OTel collectors | Telemetry-owned DaemonSet | Node-local file collection, scraping and persistent bounded export queues. |
| Go recorder | Telemetry-owned Deployment | Allowlisted list/watch sources, redaction, explicit gaps and recorder/collector health. |
| `EvidenceExport` and query Service | Future resources | Bounded export and a narrower agent query interface, if needed beyond backend-native SQL. |

## `NetworkTelemetry`

### Example

```yaml
apiVersion: observation.stacks.org/v1alpha2
kind: NetworkTelemetry
metadata:
  name: capture
  namespace: my-experiment
spec:
  networkName: network
  networkUID: 00000000-0000-0000-0000-000000000000
  storageSecretRef: telemetry-storage
  retention:
    window: 24h
  sources:
    objects: true
    logs: true
    metrics: true
```

### Spec and status

Network name/UID, backend Secret reference and log/object TTL are immutable. Sources
are mutable and change future capture through a collector rollout. The initial
profile supports log/object TTLs of 1, 6 or 24 hours; native metrics use shared tables
with a fixed 24-hour backend retention. Per-network byte quotas are not served.

The controller owns `status.admitted`, `tablePrefix` and conditions. The recorder
owns only `status.recording`, including its Pod UID, admitted generation, heartbeat,
latest backend acknowledgement and bounded source summaries. Both use minimal
server-side apply payloads with fixed field managers. No data stream is kept in CRDs.

`NetworkResolved` binds initial admission. `WorkloadsReady` describes the recorder
and collectors, independently of protocol health. Source reasons distinguish missing
optional APIs, access denial, failed reads and interrupted watches. Freshness is
explicit; old status is never evidence of current collection health.

### Reconciliation, ownership, and mutability

Install one observation operator with an administrator-selected namespace list.
It creates recording-owned workloads and scoped Roles in enrolled namespaces only.
It does not patch source Pods, StatefulSets, networks, participants or actions.

The root must exist with the requested UID before initial admission. The recording
has no network owner reference: root deletion does not delete retained data or stop
recording surviving resources. Same-name root replacement is excluded by UID.
Telemetry deletion removes collectors without deleting backend tables or promising
queue drain. Namespace deletion removes recording workloads; the independent
backend retains already acknowledged records until its retention policy applies.

### Safety, RBAC, and privacy

Managers have workload/RBAC management only in explicitly enrolled namespaces and
check ownership before every write. Recorder identities have allowlisted public
resource reads and name-scoped telemetry status writes. Node collectors read Pod
metadata only. No component reads actor Secrets; backend credentials are mounted by
the kubelet from administrator-provisioned Secrets.

Public-object projection removes annotations, configuration payloads, Pod environment
and private field classes. A fixed text policy redacts recognized credential-bearing
lines before export. Oversized object bodies retain metadata only. Native metrics
and log text remain actor-reported values; collector-assigned identity is separate.
This policy is not a universal secret detector. Additional redaction profiles require
an explicit contract before arbitrary raw payloads become eligible for storage.

### Administrator-owned profiles

The first backend is a pinned GreptimeDB standalone installation with its own PVC,
resource/query limits and separate administration, ingestion and query credentials.
`storageSecretRef` selects only its endpoint and write-only authorization header.
Agents query through the read-only database identity. Existing databases must have
compatible schemas/TTLs; ingestion hints do not update existing table options.

### Source coverage

The first slice reports conservative capture boundaries, not a lossless journal.
Each recorder start, watch reconnect and observed export failure can introduce a gap.
Collector restarts/failure counters and missing node coverage provide additional
source evidence. Heartbeats record recent activity, never a coverage classification. A source gap
may have an unknown start and does not quantify missing records. Current-state
re-lists cannot reconstruct intermediate writes. Absent optional APIs do not prevent
independent configured sources from recording.

The richer coverage vocabulary for future export is:

| Class | Meaning |
| --- | --- |
| `ContinuouslyAccounted` | A loss-detecting source proves every accepted record is retained; not claimed by this slice. |
| `NoKnownGap` | Collection was observed healthy without proof against silent loss. |
| `Unverifiable` | Records exist but continuity cannot be evaluated. |
| `Unavailable` | A configured source has no trustworthy current observation. |

### Retention semantics

Time-based retention is enforced by the backend; it is not an immediate erasure
promise. The database PVC, collection queues, record sizes and workload limits are
bounded separately. A full disk or prolonged outage can lose telemetry, but must not
stop baseline network operation. Retained history outlives its source namespace.

### Tests and definition of done

Verify exact-UID admission, non-adoption, independent deletion, status-writer
coexistence, recorder RBAC, redaction and export failure handling. Qualify actual
metric listeners and identity columns, native fault correlation, collector/backend
restart, source gaps and queries after network deletion. Wider journal/export
contracts below remain future work and do not describe complete source coverage.

## `EvidenceExport`

This resource is a future contract; it is not served by the initial telemetry slice.

### Example

```yaml
apiVersion: observation.stacks.org/v1alpha2
kind: EvidenceExport
metadata:
  name: suspected-liveness-loss
spec:
  telemetryRef:
    name: mixed-network-telemetry
    uid: 22222222-2222-2222-2222-222222222222
    configurationDigest: sha256:example
  window:
    start: "2026-09-03T11:45:00Z"
    end: "2026-09-03T12:15:00Z"
  include:
    - MutationJournal
    - ActionLifecycles
    - KubernetesEvents
    - Logs
    - Metrics
    - ProtocolFacts
  redactionProfileRef:
    name: local-redaction-v1
  destinationRef:
    name: investigation-bucket
status:
  phase: Exported
  completeness: Incomplete
  coverage: NoKnownGap
  redactionProfileDigest: sha256:example
```

The export inherits source sampling limits and records them in its manifest.
This example succeeds operationally while its source cannot prove complete
capture. Its redaction profile must match the processing already applied.

### Spec and status

The immutable spec pins one telemetry source name, UID, and admitted
configuration digest; it also contains an absolute bounded time window,
included source classes, optional selectors, and an
administrator-approved destination. It is a collection/export action, not an
environment mutation and not a replay plan.

Status records source watermarks, export object URI or opaque key, manifest
digest, byte/object counts, completeness by source, known gaps, start/end
times, phase, and conditions. Credentials and payloads never enter status.
Operation phases are `Pending`, `Exporting`, `Exported`, and `Failed`.
`completeness` is separately `Complete` or `Incomplete`, and `coverage` uses
the source coverage vocabulary.
Conditions are `SourceResolved`, `DestinationReady`, `ManifestVerified`, and
`Ready`, with stable reasons for expiry, pruning, gaps, and backend failure.

### Reconciliation, ownership, mutability, and safety

Every retained object records the redaction-profile version/digest that
produced its bytes. V1 exports only already-redacted records. Refuse an export
whose requested policy requires different or stronger processing; requesters
cannot weaken redaction. Export-time re-redaction and custom tombstone
machinery are deferred under M0 Requirement 21.

`destinationRef` names a chart-configured immutable destination profile with
backend, restricted credential Secret, namespace allowlist, retention, and
configuration digest. The controller takes a consistent best-effort snapshot at known watermarks,
writes content-addressed objects and a manifest, verifies them, then publishes
the locator. It owns only export metadata and objects under an export-specific
prefix. A finalizer may clean incomplete temporary objects but must not remove
a completed export unless retention explicitly allows it.

Spec is immutable from creation. Deletion cancels pending work; completed
evidence follows destination retention. The controller may read only the
selected `NetworkTelemetry` data and write only approved destinations.
Maximum window, bytes, concurrency, and selectors are policy bounded.
The `evidence-export-requester` Role may create/read/watch/delete this CRD but
cannot change telemetry, profile, destination, status, or storage resources.

The controller rechecks telemetry UID and configuration digest before reading,
at every frozen watermark, and before publishing status. Replacement or drift
fails the operation with reason `SourceIdentityChanged` and marks evidence
`Incomplete`; it never combines two telemetry configurations under one request.

`Complete` requires successful transfer plus `ContinuouslyAccounted` coverage
for every requested source and interval, with no capture gap, truncation,
quarantine, policy pruning, or missing object. `NoKnownGap` is still exported
and described in the manifest, but completeness is `Incomplete` because capture
completeness is unprovable. Backend failure is `Failed`. No export is called
complete solely because an upload finished.

### Manifest contract

The versioned canonical manifest includes:

- export/window/network UID and inventory transitions;
- every object's SHA-256 digest, byte size, media type, schema version, and
  relative storage key;
- source instances, watermarks, coverage classes, sampling, truncation, gaps,
  and redaction profile version/digest;
- relevant CRD/API versions, action policies, controller images, actor
  requested/runtime images, configuration/fixture digests and availability;
- Kubernetes/platform/Chaos Mesh/telemetry component versions; and
- manifest canonicalization/signature metadata.

Signing and encryption are deployment policy decisions, but the manifest
states explicitly when it is unsigned. A digest provides content integrity,
not author authenticity. The package is a self-describing investigation
record, not an executable replay description.

### Tests and definition of done

- Test window bounds, watermark consistency, manifest verification, redaction,
  partial backend failure, restart, collision, and cancellation.
- Envtest immutable spec, status ownership, destination policy, and RBAC.
- Live-test export during an active rolling window and after source failure.
- Done when a fresh client can verify every manifest object and distinguish
  source absence, capture gaps, and export failure.

## Journal event model

This expanded model is a future contract. The initial slice stores identity-tagged
OTLP observations and explicit capture markers as documented in the operating guide.

Use a versioned append-only envelope outside etcd:

```yaml
schemaVersion: observation.stacks.org/journal/v1alpha1
eventID: 01JEXAMPLE
ingestedAt: "2026-09-03T12:00:01.456Z"
observedAt: "2026-09-03T12:00:01.123Z"
effectiveAt: "2026-09-03T12:00:00.900Z"
clock:
  domain: kubernetes-apiserver
  uncertainty: unknown
source: kubernetes-audit
sourceIdentity:
  cluster: local-kind
  instanceID: audit-receiver-01JEXAMPLE
  recordID: audit-event-uuid
  sequence: 124892
network:
  namespace: stacks-regtest
  name: mixed-network
  uid: 00000000-0000-0000-0000-000000000000
subject:
  apiVersion: chaos-mesh.org/v1alpha1
  kind: NetworkChaos
  name: follower-delay
  uid: 11111111-1111-1111-1111-111111111111
eventType: ResourceCreated
evidenceClass: kubernetes-audit
payloadRef: objects/sha256/example
payloadDigest: sha256:example
payloadBytes: 2048
payloadMediaType: application/json
payloadSchema: audit.k8s.io/v1
```

Ordering is only by `(source instance, sequence)` when that source supplies a
loss-detecting sequence; otherwise records are partially ordered by source
time and ingestion time. There is no global total-order claim. Deduplication
uses `(source type, instanceID, recordID)` and never correlation labels.
Sequence discontinuity, instance replacement, disconnect, sampling,
truncation, retention, and clock uncertainty become explicit coverage/gap
records according to the source's capabilities.

For custom actions and native Chaos Mesh objects, each resource event snapshots
the current `actions.stacks.org/correlation-id` label when present. The label
supports search only; event and object UIDs remain the identity and
deduplication inputs, and metadata changes remain separate journal facts.

## Protocol facts

Collectors expose typed, low-cardinality facts such as:

- Bitcoin height, tip hash, chainwork, peer count, and synchronization state;
- Stacks Bitcoin height, Stacks block height, canonical tip, tenure, peer
  count, and run-loop state;
- signer registration, reward cycle, state, response counts, and weight; and
- Kubernetes workload readiness, restarts, image identity, and resource use.

Every fact carries source, actor identity, collection time, and confidence
class (`trusted-control-plane`, `actor-self-reported`, or
`externally-observed`). Conflicting facts coexist; the observer does not pick a
protocol truth or diagnose root cause.

## Query service

The initial delivery uses read-only backend-native HTTP SQL. This dedicated service
is deferred until investigation needs justify an additional API. MCP is low priority.

Provide read-only HTTP GET endpoints over retained data. Initial operations:

- list mutations/actions over a time range;
- stream journal entries from a cursor;
- query latest and historical actor/protocol facts;
- fetch logs/metrics around a correlation ID;
- enumerate capture gaps and source health; and
- resolve an export manifest and objects.

Queries are bounded, paginated, and time-limited. V1 uses the Kubernetes
API-server Service proxy with existing kubeconfig or projected ServiceAccount
credentials and exact Service `get` permission on `services/proxy`. Permission
covers every GET endpoint on that Service; there is no per-query or per-network
authorization claim within it. Isolate data/Service scope accordingly.

The backend is ClusterIP-only and has no application authentication in the
proposed local profile. Direct connections bypass proxy authentication. R7
requires network/actor trust qualification or a reviewed authenticated backend
before supporting broader exposure. TokenReview, SubjectAccessReview, and
direct OIDC/mTLS remain deferred until that deployment need is established.
The service never creates resources or recommends actions.

Prefer backend-native bounded pagination. Document snapshot/follow semantics,
duplicate boundaries, and cursor expiry. Expired or pruned state must not appear
as an empty complete result. Signed custom cursors are deferred. Follow clients
must reconnect because intermediaries can interrupt streams.

Bulk evidence archives use their destination's native client rather than the
API-server proxy; the proxy serves bounded queries, manifests, and previews.

## Optional API discovery

The served profile requires the network API for initial recording admission. Optional
action/Chaos sources use namespace-scoped dynamic list/watch requests against the
pinned GVR allowlist. Missing or forbidden APIs report `APINotInstalled` or
`AccessDenied` and retry once per minute; other interruptions re-list after five
seconds with a capture gap. Neither requires optional CRDs to start the manager.
This is bounded polling of known APIs, not arbitrary version discovery. Supporting
another API version requires a reviewed source and decoding change.

## Data-plane alternatives

| Alternative | Disposition |
| --- | --- |
| Store events/logs in CRD status | Rejected; high volume and update churn do not belong in etcd. |
| Loki/Prometheus only | Insufficient; useful data planes, but do not replace mutation/audit journal or export manifest. |
| Kubernetes Events as history | Rejected; lossy, rate-limited, and retention-dependent. |
| Observer-driven capture triggers | Rejected; the agent decides when it needs an export. |
| Observer replay/diagnosis API | Rejected; orchestration and reasoning belong to the agent. |

## Deferred decisions

- Loss-detecting audit ingestion and durable recorder spooling.
- Evidence export manifests, narrower query authorization and direct-backend trust
  beyond the disposable local profile.
- Per-network byte quotas and managed-cluster retention profiles.
- Additional protocol polling and tracing sources.
- Greptime MCP integration, after backend-native agent queries prove insufficient.

## References

- [Kubernetes custom resources](https://kubernetes.io/docs/concepts/extend-kubernetes/api-extension/custom-resources/)
- [Kubernetes audit](https://kubernetes.io/docs/tasks/debug/debug-cluster/audit/)
