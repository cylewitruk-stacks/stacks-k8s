# Architecture design package

This package defines the next `stacks-k8s` architecture phase, including
reviewed contracts and proposed capabilities. Reviewed contracts define an
implementation baseline; they do not imply an installed CRD, served API, or
implemented controller. Existing behavior remains documented under
[`docs/`](../README.md).

## Product goals

Intended uses include ordinary regtest development and testing; network
reliability, liveness, resilience, performance, and efficiency investigations;
defensive security testing; and attempts to verify behavior described in
responsibly reported vulnerability reports. Components must remain modular
and independently useful: running a `StacksNetwork` must not require an agent,
action controllers, or observability tooling. Users compose those capabilities
as their testing or investigation needs grow.

A primary use is defensive security investigation by trusted cybersecurity
agents with authorized access to disposable `StacksNetwork`s. These networks
provide reasonably realistic multi-actor environments for simulating
adversarial conditions and identifying potentially critical issues that unit
tests, integration tests, and static analysis may not expose.

In the target architecture, the agent combines topology changes, bounded
protocol actions, native infrastructure faults, and available instrumented-actor
capabilities within administrator-granted permissions. Operators expose
composable controls and passive evidence; the agent selects experiments,
interprets results, and
verifies suspected issues. Trust in the agent does not replace the
[RBAC, admission, and identity boundaries](security-and-safety.md).

For investigations, success means evidence-backed conclusions and, where
feasible, semantically verified reduced reproducers. Reported behavior is a
claim to assess; an unsuccessful reproduction attempt is not proof that an
issue is absent. Verification does not require identical distributed
execution or proof of causal minimality. Capability and platform qualification
must distinguish implemented behavior from proposed or unavailable features.

## Status vocabulary

| Label | Meaning |
| --- | --- |
| Implemented | Present in the committed operators. |
| Contract complete | Reviewed design and contract baseline is complete; runtime and served API availability require separate implementation. |
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
| [Bitcoin lifecycle](bitcoin-lifecycle.md) | Continuous block production, bounded generation, reorganization, and shared serialization. |
| [Atomic actions](actions.md) | Normative shared action lifecycle, targeting, status, cleanup, and extension rules. |
| [Chaos Mesh](chaos-mesh.md) | Direct native fault usage, selection, policy, correlation, and packaging. |
| [Protocol actions](protocol-actions.md) | Non-Chaos-Mesh signer, miner, clock, storage, and input behaviors. |
| [Observability](observability.md) | Passive collection, audit history, telemetry, retention, queries, and export. |
| [Agent interface](agent-interface.md) | Capability discovery, resource conventions, queries, and agent workflow. |
| [Security and safety](security-and-safety.md) | Trust boundaries, RBAC, admission, identity, secrets, and blast-radius controls. |
| [Packaging](packaging.md) | Operator/chart boundaries, optional dependencies, versioning, and qualification. |
| [Roadmap](roadmap.md) | Ordered implementation phases and definitions of done. |
| [M0 remediation plan](m0-remediation-plan.md) | Authoritative ordered M0.x delivery ledger, stable remediation requirements, and acceptance checklist. |

The M0 remediation plan takes precedence where the original design documents
still describe a superseded proposal. Those documents will be reconciled as
part of M0 rather than silently treated as implemented decisions.

## Recommended component map

```text
external agent
  ├─ patches StacksNetwork or creates standalone actor resources
  ├─ creates or updates BitcoinBlockProduction for baseline chain progress
  ├─ creates native Chaos Mesh fault resources
  ├─ creates one protocol-specific action resource per action
  ├─ queries observability APIs and telemetry stores
  └─ investigates, retries, reduces, and reports issues

stacks-network-operator
  └─ desired topology and actor workloads

stacks-action-operator (recommended)
  └─ small independent controllers for Bitcoin production and protocol actions

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
| `BitcoinBlockProduction` | M0.4 desired-operation contract complete; not implemented | [Bitcoin lifecycle](bitcoin-lifecycle.md) |
| `BitcoinBlockGeneration`, `BitcoinReorganization` | M0.4 action contract complete; not implemented | [Bitcoin lifecycle](bitcoin-lifecycle.md) |
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
| Baseline Bitcoin production | Direction | One mutable `BitcoinBlockProduction` per Bitcoin node, outside the bounded-action lifecycle. |
| Finite Bitcoin generation | Direction | One bounded `BitcoinBlockGeneration` using immediate, fixed, uniform-random, or explicit-sequence cadence. |
| Observation storage | Direction | External telemetry/evidence store; never bulk data in CRD status. |
| Replay and reduction | Direction | Agent responsibility outside operators. |
| Build from Git revision | Direction | External build tooling produces an OCI image. |
| Action deployment boundary | Recommended | One modular operator initially; revisit when privileges diverge. |
| Custom-action API contract | Direction | `actions.stacks.org/v1alpha1`, immutable specs, shared lifecycle fixture, and typed same-namespace references. |
| Action correlation | Direction | `actions.stacks.org/correlation-id` is a search hint; object UID is authoritative. |
| Observation API names | Open | `NetworkTelemetry` and `EvidenceExport` are working names. |
| Aggregate action admission | Open | Not claimed initially; preserve direct native CRDs and avoid side-effecting admission. |
| Bitcoin mutation serialization | Direction | One target-UID-derived Lease across production and action kinds, renewed by a leader-gated manager; ambiguity remains explicit. |
| Bitcoin RPC credentials | Direction | `spec.bitcoinRPCAuth`, v2 profiles, fixed immutable Secret, and expected digest; never selected by an action spec. |

## Design acceptance

Implementation proposals must show that they preserve the boundaries above,
name the single owner of every field and side effect, and provide a negative
test demonstrating that the controller cannot expand into scenario
orchestration. Unresolved decisions remain explicit; they must not be silently
chosen during implementation.
