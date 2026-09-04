# Architecture design package

This package defines the next `stacks-k8s` architecture phase. It is a design
input, not an implemented API contract. Existing behavior remains documented
under [`docs/`](../README.md).

## Status vocabulary

| Label | Meaning |
| --- | --- |
| Implemented | Present in the committed operators. |
| Direction | Agreed architectural constraint. |
| Recommended | Proposed implementation awaiting API review. |
| Open | Decision required before implementation. |

## Non-negotiable boundaries

- The external agent is the orchestration and reasoning layer.
- One action resource represents one bounded action.
- No operator plans scenarios, schedules experiments, replays journals,
  reduces failures, diagnoses root cause, or creates regression cases.
- The observability operator is passive toward the observed environment.
- Distributed execution is not deterministic and is not claimed reproducible.
- New public APIs say Bitcoin rather than burnchain, except when quoting an
  existing Stacks configuration or protocol field.
- Source checkout and image construction happen outside Kubernetes operators.
  Operators consume OCI image references.
- Generic infrastructure faults use native Chaos Mesh resources.
- Controllers are idempotent, level-based, independently understandable, and
  have one logical writer for every field and external side effect.

## Documents

| Document | Scope |
| --- | --- |
| [Current state](current-state.md) | Implemented capabilities, gaps, and historical feature inventory. |
| [Topology](topology.md) | Mutable `StacksNetwork`, actor lifecycle, configuration, storage, and upgrades. |
| [Bitcoin lifecycle](bitcoin-lifecycle.md) | Mining policy, bounded block generation, reorganization, and peer state. |
| [Atomic actions](actions.md) | Normative shared action lifecycle, targeting, status, cleanup, and extension rules. |
| [Chaos Mesh](chaos-mesh.md) | Direct native fault usage, selection, policy, correlation, and packaging. |
| [Protocol actions](protocol-actions.md) | Non-Chaos-Mesh signer, miner, clock, storage, and input behaviors. |
| [Observability](observability.md) | Passive collection, audit history, telemetry, retention, queries, and export. |
| [Agent interface](agent-interface.md) | Capability discovery, resource conventions, queries, and agent workflow. |
| [Security and safety](security-and-safety.md) | Trust boundaries, RBAC, admission, identity, secrets, and blast-radius controls. |
| [Packaging](packaging.md) | Operator/chart boundaries, optional dependencies, versioning, and qualification. |
| [Roadmap](roadmap.md) | Ordered implementation phases and definitions of done. |
| [M0 remediation plan](m0-remediation-plan.md) | Authoritative review remediation, API-module spike, and revised implementation order. |

The M0 remediation plan takes precedence where the original design documents
still describe a superseded proposal. Those documents will be reconciled as
part of M0 rather than silently treated as implemented decisions.

## Recommended component map

```text
external agent
  ├─ patches StacksNetwork or creates standalone actor resources
  ├─ creates native Chaos Mesh fault resources
  ├─ creates one protocol-specific action resource per action
  ├─ queries observability APIs and telemetry stores
  └─ investigates, retries, reduces, and reports issues

stacks-network-operator
  └─ desired topology and actor workloads

stacks-action-operator (recommended)
  └─ small independent controllers for Bitcoin and Stacks-specific actions

Chaos Mesh
  └─ generic Pod, network, DNS, I/O, time, and stress faults

stacks-observability-operator
  └─ passive identity, mutation history, telemetry correlation, and export
```

One action-operator binary may host multiple small controllers initially. This
does not permit a shared scenario reconciler. Split deployment units later
when permissions, dependencies, or failure domains materially differ.

## API inventory

| API kind | Status | Design owner |
| --- | --- | --- |
| `StacksNetwork`, `BitcoinNode`, `StacksNode`, `StacksSigner` | Implemented; extend | [Topology](topology.md) |
| `NetworkObservation` | Implemented compatibility API | [Current state](current-state.md) |
| `ActionSafetyPolicy` | Recommended for custom actions | [Atomic actions](actions.md) |
| `BitcoinMiningWindow`, `BitcoinBlockRequest`, `BitcoinReorganization` | Recommended | [Bitcoin lifecycle](bitcoin-lifecycle.md) |
| `ApplicationClockOffset`, `SignerBehavior`, `MinerBehavior` | Recommended | [Protocol actions](protocol-actions.md) |
| `ProtocolInputInjection` | Recommended after endpoint decision | [Protocol actions](protocol-actions.md) |
| `ActorDiskPressure` | Conditional fallback | [Protocol actions](protocol-actions.md) |
| `NetworkTelemetry`, `EvidenceExport` | Recommended; names open | [Observability](observability.md) |
| Native Chaos Mesh fault kinds | Upstream APIs; qualify directly | [Chaos Mesh](chaos-mesh.md) |
| `SbtcSigner` | Open, not yet proposed | [Topology](topology.md) |

## Decision register

| Decision | Status | Resolution |
| --- | --- | --- |
| Orchestration owner | Direction | External agent only. |
| Generic faults | Direction | Use native Chaos Mesh CRDs directly. |
| Forced Bitcoin reorganization | Recommended | Bounded action resource, not `StacksNetwork` state. |
| Natural Bitcoin reorganization | Direction | Emergent behavior to observe; no action resource required. |
| Cadenced Bitcoin mining | Recommended | Bounded `BitcoinMiningWindow`. |
| Bounded block generation | Recommended | One `BitcoinBlockRequest` per request. |
| Observation storage | Direction | External telemetry/evidence store; never bulk data in CRD status. |
| Replay and reduction | Direction | Agent responsibility outside operators. |
| Build from Git revision | Direction | External build tooling produces an OCI image. |
| Action deployment boundary | Recommended | One modular operator initially; revisit when privileges diverge. |
| Custom-action API contract | Direction | `actions.stacks.org/v1alpha1`, immutable specs, shared lifecycle fixture, and typed same-namespace references. |
| Action correlation | Direction | `actions.stacks.org/correlation-id` is a search hint; object UID is authoritative. |
| Observation API names | Open | `NetworkTelemetry` and `EvidenceExport` are working names. |
| Aggregate action admission | Open | Not claimed initially; preserve direct native CRDs and avoid side-effecting admission. |

## Design acceptance

Implementation proposals must show that they preserve the boundaries above,
name the single owner of every field and side effect, and provide a negative
test demonstrating that the controller cannot expand into scenario
orchestration. Unresolved decisions remain explicit; they must not be silently
chosen during implementation.
