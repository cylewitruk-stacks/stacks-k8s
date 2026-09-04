# Passive observability design

## Purpose and boundary

The observability layer is the agent's passive recorder and query surface. It
collects topology mutations, action lifecycles, Kubernetes events, protocol
facts, logs, metrics, and capture gaps so an external agent can investigate an
issue and attempt a careful replay or reduction itself.

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

`NetworkObservation` is an implemented one-shot identity snapshot. It verifies
a `StacksNetwork` UID, generation, and inventory digest and reports admitted
actors. It does not continuously retain history or collect protocol telemetry.

Retain it as a lightweight compatibility API until the continuous recording
surface is proven. Do not overload it with session or export behavior.

## Proposed resources and services

| Component | Form | Purpose |
| --- | --- | --- |
| `NetworkTelemetry` | CRD | Declare passive collection and bounded retention for one network. |
| `EvidenceExport` | CRD | Request one bounded, immutable export from retained observations. |
| Observation query API | Service | Efficiently query retained facts without putting data in CRDs. |
| Journal/object store | External data plane | Store event streams, logs, snapshots, indexes, and exported bundles. |

Working API group remains `observation.stacks.org/v1alpha1`. Names are open
until API review.

## `NetworkTelemetry`

### Example

```yaml
apiVersion: observation.stacks.org/v1alpha1
kind: NetworkTelemetry
metadata:
  name: mixed-network-telemetry
spec:
  networkRef:
    name: mixed-network
    uid: 00000000-0000-0000-0000-000000000000
  retention:
    rollingWindow: 6h
    maximumBytes: 20Gi
  sources:
    kubernetesAudit: true
    kubernetesEvents: true
    actionResources: true
    logs: true
    metrics: true
    protocolFacts:
      bitcoinTips: true
      stacksTips: true
      signerParticipation: true
  storageRef:
    name: local-observation-store
```

### Spec and status

Spec selects exactly one network name and UID, source classes, rolling time/byte limits,
redaction profile, sampling limits, and an administrator-provisioned storage
backend reference. It does not contain experiment steps, expected outcomes,
capture triggers, or replay instructions.

Status contains observed generation, admitted network UID, current inventory
identity, source health/watermarks, oldest/newest retained timestamps, retained
byte estimate, gap summaries, storage health, and a canonical configuration
digest covering admitted spec/profile identities, plus phase and conditions.
It stores no event stream or log payload.

Initial phases are `Pending`, `Recording`, `Degraded`, and `Stopped`.
Conditions are `NetworkResolved`, `StorageReady`, `SourcesHealthy`,
`RetentionEnforced`, and `Ready`, each carrying `observedGeneration`. Stable
reason values distinguish absent APIs, identity replacement, source outage,
storage pressure, redaction failure, and policy pruning.

### Reconciliation, ownership, and mutability

The controller resolves the network and configures its own collectors. It
watches topology/action/Chaos resources, consumes the configured audit and
telemetry sources, and writes journal segments outside the Kubernetes API.
It never patches observed resources.

Spec is mutable except for network name/UID. Source or retention changes
affect future collection and may prune data according to the newly admitted
policy; status records the policy transition. Network identity replacement
stops collection and requires a new `NetworkTelemetry` object. Deletion stops
collectors and applies the declared storage-retention policy; it never deletes
the network.

Network recreation under the same name never silently resumes collection.
One controller owns status. Collector workers publish through an internal
bounded channel and durable writer interface; they do not independently patch
the CRD.

### Safety, RBAC, and privacy

- Read-only access to enrolled topology, action, Chaos Mesh, Pod, Event, and
  approved ConfigMap/status resources.
- Log/metric access is backend-scoped; no actor Secret reads.
- Audit ingestion uses an explicit cluster-admin integration and documents
  whether the source can drop records.
- Redact authorization headers, credentials, keys, and configured patterns
  before durable storage; record that redaction occurred.
- Backpressure drops only according to declared priority and emits a durable
  capture-gap record. It never silently claims completeness.
- Resource limits and storage quotas prevent telemetry from exhausting the
  observed namespace or control plane.

### Administrator-owned profiles

`storageRef` names a chart-configured immutable storage profile, not an
arbitrary Kubernetes object. A profile specifies backend type, endpoint,
Secret name, allowed namespaces, encryption, retention capabilities, and a
stable configuration digest. Helm emits resource-name-restricted Secret RBAC
for configured profiles. Agents may read profile names but cannot create,
change, or select credentials.

Redaction profiles are likewise administrator-owned, versioned, and
content-digested. Unknown media types or malformed payloads are retained only
as metadata plus digest and marked `RedactionUnverified`; raw bytes are not
durably written. A policy update affects future collection and records a
transition—it never silently re-labels older data.

### Source coverage

Each source reports one coverage class per interval:

| Class | Meaning |
| --- | --- |
| `ContinuouslyAccounted` | A loss-detecting sequence/spool proves every accepted source record is present. |
| `NoKnownGap` | Collection was healthy, but the source cannot prove absence of silent loss. |
| `Unverifiable` | Records exist but continuity cannot be evaluated. |
| `Unavailable` | The source was configured but no trustworthy records were collected. |

Kubernetes audit uses an explicit policy with `Request` or `RequestResponse`
levels at appropriate stages for enrolled non-sensitive CRDs, metadata-only
treatment for Secrets and other sensitive resources, a dedicated webhook
identity, and a durable spooling receiver when `ContinuouslyAccounted` is
claimed. A normal audit
webhook without loss-detecting delivery can claim at most `NoKnownGap`.
Watches never claim every intermediate update; their resource-version relist
history is low-latency state evidence, not an audit log.

Status carries bounded counts and the latest gap/coverage summary. Full gap
intervals live in the journal. A topology mutation is called recorded only if
an audit record exists; otherwise the observer reports the state transition it
actually saw and its weaker coverage class.

### Retention semantics

Journal segments and payload objects are immutable. Pruning closes the current
segment, then removes the oldest closed segments by end time until both the
time and byte limits hold. Ties use segment digest byte order. Index tombstones
retain interval, reason, source watermarks, and removed-object digest summary.

Queries distinguish `Expired` (outside rolling window), `PolicyPruned`
(removed by a changed/stricter policy), `CaptureGap` (never collected), and
`Unavailable` (source/backend unavailable). None are converted to an empty
successful result.

### Tests and definition of done

- Unit-test source normalization, redaction, retention, watermarks, and gap
  formation.
- Envtest topology/action lifecycle correlation and strict read-only RBAC.
- Integration-test audit/webhook disconnect, Loki/Prometheus outage, storage
  full, controller restart, and clock skew.
- Live-test a rolling window through network rollout and native/custom action
  creation/deletion.
- Done when an operator can identify collection health and gaps from status,
  and high-volume data never enters etcd.

## `EvidenceExport`

### Example

```yaml
apiVersion: observation.stacks.org/v1alpha1
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
  destinationRef:
    name: investigation-bucket
```

### Spec and status

The immutable spec pins one telemetry source name, UID, and admitted
configuration digest; it also contains an absolute bounded time window,
included source classes, optional selectors, and an
administrator-approved destination. It is a collection/export action, not an
environment mutation and not a replay plan.

Status records source watermarks, export object URI or opaque key, manifest
digest, byte/object counts, completeness by source, known gaps, start/end
times, phase, and conditions. Credentials and payloads never enter status.
Phases are `Pending`, `Exporting`, `Complete`, `Incomplete`, and `Failed`.
Conditions are `SourceResolved`, `DestinationReady`, `ManifestVerified`, and
`Ready`, with stable reasons for expiry, pruning, gaps, and backend failure.

### Reconciliation, ownership, mutability, and safety

Every retained object records the redaction-profile version/digest that
produced its bytes. An export uses the currently admitted profile and
re-redacts objects produced under older profiles; it never copies weaker old
bytes directly. If stronger processing cannot safely decode an object, that
object is quarantined/omitted and the export is `Incomplete`. An administrator
may mark older profile digests quarantined or policy-pruned; tombstones and
affected intervals remain visible. Requesters cannot weaken redaction.
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
makes the export `Incomplete` with reason `SourceIdentityChanged`; it never
combines two telemetry configurations under one request.

`Complete` requires successful transfer plus `ContinuouslyAccounted` coverage
for every requested source and interval, with no capture gap, truncation,
quarantine, policy pruning, or missing object. `NoKnownGap` is still exported
and described in the manifest, but the phase is `Incomplete` because capture
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

Provide a read-only HTTP or gRPC API over retained data. Initial operations:

- list mutations/actions over a time range;
- stream journal entries from a cursor;
- query latest and historical actor/protocol facts;
- fetch logs/metrics around a correlation ID;
- enumerate capture gaps and source health; and
- resolve an export manifest and objects.

Queries are bounded, paginated, time-limited, and authorized by namespace and
network. The API returns stable machine-readable schemas and opaque cursors.
It never creates Kubernetes resources or recommends the next action.

In-cluster authentication uses projected ServiceAccount tokens. The service
performs TokenReview and SubjectAccessReview, requiring `get` permission on
the referenced `NetworkTelemetry` in its namespace for data reads and on the
specific `EvidenceExport` for export objects. External deployments terminate
OIDC/mTLS at a documented authenticating proxy before the same authorization
check.

A cursor is signed/opaque and binds query schema version, authenticated
principal scope, normalized filters, snapshot upper watermark, last returned
sort key, and expiry. Resume is exclusive of the last record and may repeat
only a documented boundary record after failover; clients deduplicate by
event ID. New appends beyond the snapshot watermark appear only in a new/follow
query. Pruned cursor state returns `410 Gone` with `Expired` or
`PolicyPruned`, never an apparently complete empty page.

## Optional API discovery

`network.stacks.org` is required for a configured `NetworkTelemetry`; absence
keeps it `Pending`/`SourceUnavailable`. Action and Chaos Mesh APIs are optional.
The operator uses discovery plus dynamic informers for optional groups rather
than registering static controller-runtime watches that fail manager startup.
It starts/stops informers on CRD appearance/disappearance, records source
instance transitions and gaps, and periodically refreshes discovery with a
bounded rate. Typed adapters decode supported versions at the journal boundary;
unknown versions are metadata-only and `Unverifiable`.

Unit tests cover absent-at-startup, later installation, CRD removal, discovery
failure, informer relist, duplicate delivery, and version change. Removing
Chaos Mesh or the action operator cannot stop topology observation.

## Data-plane alternatives

| Alternative | Disposition |
| --- | --- |
| Store events/logs in CRD status | Rejected; high volume and update churn do not belong in etcd. |
| Loki/Prometheus only | Insufficient; useful data planes, but do not replace mutation/audit journal or export manifest. |
| Kubernetes Events as history | Rejected; lossy, rate-limited, and retention-dependent. |
| Observer-driven capture triggers | Rejected; the agent decides when it needs an export. |
| Observer replay/diagnosis API | Rejected; orchestration and reasoning belong to the agent. |

## Open decisions

1. Initial journal backend: object segments, an embedded durable log, or a
   dedicated event store.
2. Loki/Prometheus query integration versus OpenTelemetry-first collection.
3. Whether the initial local profile can support loss-detecting audit spooling
   or must honestly use `NoKnownGap`.
4. Stable fact schema and HTTP versus gRPC query transport.
5. Default rolling time/byte windows for local and managed clusters.

## References

- [Kubernetes custom resources](https://kubernetes.io/docs/concepts/extend-kubernetes/api-extension/custom-resources/)
- [Kubernetes audit](https://kubernetes.io/docs/tasks/debug/debug-cluster/audit/)
