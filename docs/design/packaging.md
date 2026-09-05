# Packaging and release design

## Independent products

| Package | Responsibility | Dependency direction |
| --- | --- | --- |
| `apis/network` | Versioned network Kubernetes API types | Kubernetes API machinery only |
| `stacks-network-operator` | Reusable Stacks regtest topology | Kubernetes only |
| `stacks-observability-operator` | Passive identity and telemetry collection | Reads network/action/Chaos APIs; does not import their runtime code |
| `stacks-action-operator` | Bitcoin production, protocol-specific atomic actions, and safety policy | Uses versioned network wire APIs |
| Chaos Mesh | Generic infrastructure faults | External optional dependency |
| Telemetry backend bundle | Loki/Prometheus/OpenTelemetry/object storage profile | External optional dependencies |
| Development bundle | Pins compatible chart versions for local use | Depends on products; does not merge their release lifecycles |

The network chart must remain useful alone. Observation and action charts are
independently installable and versioned. A bundle is convenience packaging,
not a new monolithic controller.

The observability chart treats network APIs as required only when a
`NetworkTelemetry` targets one, and action/Chaos APIs as optional discovery
sources. Optional integrations use dynamic discovery/informers so an absent or
removed CRD degrades that source and records a gap rather than preventing
manager startup.

## Repository layout

Recommended additions preserve the current pattern:

```text
charts/
  stacks-network-operator/
  stacks-observability-operator/
  stacks-action-operator/
  stacks-k8s-dev/                 # optional dependency bundle
operators/
  network/
  observability/
  action/
apis/
  network/
contracts/
  action-lifecycle-v1.json
  actor-ports-v1.json
  bitcoin-actions-v1.json
  bitcoin-block-production-v1.json
  bitcoin-reservation-v1.json
  image-id-v1.json
  inventory-v1.json
  leaf-spec-v1.json
docs/
  design/
  ...
tools/
```

Each operator retains its own runtime Go module and release version. Shared
network API types live in a separately versioned, types-only module. Wire
fixtures remain under `contracts/`; byte-sensitive consumers continue to
verify them through their own production implementations. Avoid a shared
runtime library that forces operators onto one controller-runtime dependency
graph or release cadence.

## CRD ownership and installation

- Each chart owns only its CRDs and namespaced/cluster-scoped RBAC.
- The action chart may own both `actions.stacks.org` and
  `bitcoin.stacks.org` CRDs; API lifecycle remains resource-specific even when
  controllers share one binary and reservation manager.
- CRDs are generated from API types; generated files are committed.
- Helm's CRD upgrade limitations require documented manual or release-tool
  steps before a served/storage version changes.
- Initially serve one alpha version per new API. Do not claim external skew
  compatibility until a compatibility matrix and conversion plan exist.
- Native Chaos Mesh CRDs are installed by its upstream chart, never copied.

## Image and Git-source boundary

Operators accept immutable OCI image references. They never clone repositories
or execute selected build scripts. An external agent or CI system:

1. checks out the chosen repository/revision;
2. builds and scans the image outside operator trust;
3. pushes it to an accessible registry;
4. resolves an immutable digest; and
5. patches the relevant actor image and configuration reference.

The observation journal may record declared source metadata and image digest,
but operators do not attest that metadata matches image contents unless a
separate verified supply-chain provenance system establishes it.

## Dependencies and compatibility

Publish a tested matrix covering:

- Kubernetes, Helm, controller-runtime, and Go toolchain;
- Bitcoin Core and Stacks actor images/config profiles;
- Chaos Mesh, CNI, container runtime, OS/architecture, and kernel;
- storage driver/filesystem; and
- telemetry backends and query schemas.

Version pins use supported upstream compatibility families. Renovation is
tested per module and does not force unrelated operator releases.

## Release artifacts

Every product release provides:

- signed OCI image and Helm chart;
- SBOM and vulnerability scan result;
- generated CRDs and API reference;
- exact RBAC verification;
- examples validated against schemas;
- compatibility/qualification statement;
- upgrade and rollback notes; and
- checksums/provenance for published artifacts.

Behavioral qualification records what was observed on a concrete platform; it
does not claim deterministic replay.

## Development and verification

Top-level verification orchestrates independent module checks without a
committed root `go.work`. Each chart can render/lint alone. Contract tests
verify import boundaries, CRD ownership, exact RBAC, Markdown links, example
schemas, generated-file currency, and the absence of scenario resources.

Integration profiles:

| Profile | Contents |
| --- | --- |
| Topology | Network operator and real actor images |
| Observation | Topology plus observation and configured data plane |
| Native faults | Topology, observation, and Chaos Mesh |
| Protocol actions | Topology, observation, and action operator |
| Full development | All compatible pinned charts |

## Alternatives

| Alternative | Disposition |
| --- | --- |
| One chart/operator binary | Rejected; combines privileges, failures, and release cadences. |
| Fork/vendor Chaos Mesh | Rejected initially; use upstream APIs and chart. |
| Build Git revisions in a controller | Rejected; executes untrusted source inside the control plane. |
| Shared controller/runtime Go module | Rejected; share only the versioned API types. |
| Public external-controller compatibility immediately | Deferred until a second consumer and skew policy exist. |

## Definition of done

- Every chart installs, upgrades within its supported alpha contract, and
  verifies independently.
- The network operator has no action, Chaos Mesh, or telemetry dependency.
- No operator clones or builds Git source.
- Bundle installation proves compatible pins without coupling releases.
- Published artifacts are signed, scanned, documented, and reproducible as
  build artifacts without promising repeatable distributed outcomes.

## Open decisions

1. Registry and chart publication locations.
2. Initial development-bundle dependencies and whether it belongs in this
   repository.
3. Release/version policy before `v1beta1` APIs.
4. Telemetry backend defaults for local and managed clusters.
