# Repository architecture

The repository is a monorepo of independently deployable operators. Each
runtime remains an independent Go module, container image, Helm chart, and
release unit.

```text
StacksNetwork
  -> network aggregate controller
  -> BitcoinNode / StacksNode / StacksSigner controllers
  -> Services, StatefulSets, ConfigMaps, and Pods

NetworkObservation
  -> observability controller
  -> uncached reads of network and Kubernetes resources
  -> identity-bound observation status
```

The network operator owns desired topology and workloads. The observability
operator is read-only with respect to the environment it observes and consumes
published Kubernetes resources through their wire format. It may write its own
resources and configured journal or evidence sinks, but never topology,
actions, or actor workloads. It intentionally does not import the network
operator's Go implementation.

The versioned fixtures in [`contracts/`](../contracts/) pin the shared
admitted-inventory digest, leaf-specification digest, actor Service ports, and
immutable image-ID parsing contracts. Both operators decode the same vectors
into their own native types and verify them through their production
implementations.

## Agent and operator boundary

The agent is the orchestration and reasoning layer. This repository provides
the small, safe capabilities and trustworthy observations an agent needs; it
does not replace the agent with an in-cluster experiment engine.

| Component | Responsibility |
| --- | --- |
| Network operator | Reconcile declarative topology and actor workloads. |
| Action controllers | Execute one small, bounded, independently observable action represented by one resource. |
| Observability operator | Passively collect, correlate, retain, query, and export facts and telemetry. |
| Agent | Orchestrate, adapt, investigate, attempt replay, reduce, diagnose, and construct regression cases. |

The network and initial observability controllers exist today. Action
controllers and the external agent are target-architecture participants, not
implementations currently provided by this repository. Defining their
boundaries now prevents future action APIs from growing into an in-cluster
scenario or playbook engine.

The observability operator must not create or alter networks, apply actions or
faults, replay a recorded journal, reduce a scenario, or classify root cause.
It may retain a strict history of observed topology changes, action lifecycles,
actor identities, and surrounding telemetry so an agent can investigate an
issue and attempt to reproduce it in another environment.

The target observability surface provides passive capabilities to:

- configure continuous recording and retention for a network;
- journal topology changes and action-resource lifecycles;
- collect Bitcoin, Stacks, signer, Kubernetes, log, metric, and resource
  telemetry;
- query snapshots, deltas, timelines, and recent windows;
- report capture gaps and unavailable sources;
- seal or export a selected time range; and
- preserve the identity and integrity metadata needed to correlate facts.

Future APIs may use resource shapes such as a recording session and an
evidence export, but their names and schemas require separate API design. Such
resources describe collection and export only; they never describe execution.

Deterministic replay of a distributed system is explicitly out of scope.
Hardware, scheduling, network timing, implementation randomness, and external
conditions may change every outcome. Digests identify observed inputs and
artifacts and protect attribution and integrity; they are not promises of an
identical execution. Deterministic compilation and canonical content digests
remain required where they form an API contract; distributed execution is not
deterministic. The useful result is a semantically verified, reduced reproducer
found by the agent within its chosen trial budget, not a claim of causal
minimality.

## Dependency boundaries

- Runtime modules use controller-runtime's supported Kubernetes minor.
- Generator dependencies remain in isolated `tools` modules because
  controller-tools may use a newer Kubernetes dependency family.
- Repository-wide chart policy is implemented once under `tools/chart-policy`;
  it is verification tooling and is not linked into either operator runtime.
- Generation runs with `GOWORK=off`; no workspace may silently unify runtime
  and generator dependency graphs.
- CRDs are generated directly into their owning chart. There is no second
  generated CRD copy or synchronization script.
- Helm RBAC is checked against each operator's exact normalized allowlist.

Static API validation belongs in structural OpenAPI schemas and CEL rules.
Admission webhooks are added only when a required invariant depends on live
cluster state or cannot reasonably be expressed in those mechanisms.
