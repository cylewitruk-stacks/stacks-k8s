# External agent interface

## Purpose

The external agent is the sole orchestration and reasoning layer. Kubernetes
resources and observation services give it small, composable capabilities; no
operator contains a scenario planner or decides what an observed result means.

## Supported interaction model

An agent may independently:

1. create or patch a `StacksNetwork` and wait for its observed generation;
2. declare Bitcoin production and transaction demand through `StacksNetwork`;
   standalone capability editing remains conditional on reviewed ownership,
   overlap, and authorization contracts;
3. inspect admitted actors and current protocol facts;
4. create one native Chaos Mesh or protocol-specific action resource;
5. watch that resource and surrounding telemetry;
6. create, overlap, update, or remove further resources within safety policy;
7. request an `EvidenceExport` for a useful time window; and
8. use the record and its own context to attempt reproduction and reduction.

These are client choices, not an in-cluster workflow.

## Discovery

The API must be discoverable through standard Kubernetes mechanisms:

- structural CRD schemas and `kubectl explain`;
- printer columns for phase, target, age, and key readiness facts;
- stable conditions and reason values;
- namespaced Roles for viewer, network editor, action user, and observer;
- versioned examples and API reference pages; and
- OpenAPI/CEL errors that identify the invalid field.

An optional CLI may improve ergonomics by listing supported kinds, creating
one resource, following status, or querying observations. It must be a thin
client: it does not hide a local scenario engine or invent a second API.

## Correlation and identity

Every agent-created custom or native action should carry the
`actions.stacks.org/correlation-id` label with a user-chosen Kubernetes label
value of at most 63 characters. It is a search hint, never an attribution,
authorization, deduplication, or idempotency key. Kubernetes object UID remains
the authoritative identity. Observation correlates:

- API audit identity and request;
- object UID/generation/spec;
- admitted target identity and action policy;
- controller lifecycle and mechanism facts; and
- surrounding telemetry and capture gaps.

Digests may identify compiled configuration, immutable images, requests, and
evidence objects. They verify identity and fulfillment; they are not expected
outcomes and do not imply deterministic execution.

## Waiting and decisions

Agents watch generation-aware status and conditions rather than sleeping for
fixed intervals. Custom actions use the normative `Pending`, `Admitted`,
`Active`, `Recovering`, `Completed`, `Recovered`, `Failed`, and `Inconclusive`
phases. The agent decides whether to wait, delete an action to request
cancellation, investigate, or apply another resource.

`BitcoinBlockProduction` and ongoing transaction producers are mutable desired
operation, not terminal actions. Their revised fields and status remain open.
Watch current policy generation, target availability, acknowledged effects,
skipped opportunities, and uncertainty as distinct facts. Whole-network
`Ready` is not the prerequisite for an independently valid target.

Temporary overrides resume the latest baseline after cleanup. A reservation
deadline or client timeout alone does not prove that a Bitcoin mutation has
stopped; clients must preserve any unresolved exclusion/uncertainty state.

`Inconclusive` is a first-class result. Clients must not translate it to
success or failure. Protocol correctness remains a conclusion drawn by the
agent from trusted and self-reported evidence.

## Query API

The planned v1 query service uses bounded, read-only HTTP GET requests through
the Kubernetes API-server Service proxy. Existing kubeconfig or projected
ServiceAccount credentials authorize that path. Bulk evidence uses its storage
destination's native client. See M0 Requirements 17–21 for the access contract.

The proxy does not authenticate direct connections to the backend. Deployment
qualification must establish its network/actor trust boundary or add direct
backend authentication before broader exposure.

The observation query service complements Kubernetes watches for high-volume
data. Agent clients need:

- bounded time-range and cursor queries;
- streaming journal follow;
- actor and action filters;
- source-health and gap queries;
- protocol-fact history;
- correlated log and metric retrieval; and
- export manifest retrieval.

Responses include schema version, pagination/cursor metadata, source and
identity provenance, timestamps, truncation, and known gaps. The service is
read-only and never returns an executable recommendation.

## Error model

APIs distinguish:

| Class | Examples | Agent response |
| --- | --- | --- |
| Invalid request | Schema, unsupported parameter, prohibited target | Correct or abandon the request. |
| Pending dependency | Network rollout, target reservation held, source startup | Watch status or cancel. |
| Definite mechanism failure | Rejected RPC, helper failure | Inspect facts; choose next action. |
| Identity divergence | Replaced target, changed network | Treat prior attribution as invalid; reassess. |
| Inconclusive effect | Ambiguous RPC, missing telemetry, cleanup unknown | Do not claim success; inspect gaps. |
| Capacity/policy refusal | Duration, concurrency, storage, role impact | Narrow request or seek administrator change. |

## Security and rate limits

Agent credentials are namespace-scoped and least-privileged. They can edit
aggregate topology, use approved action resources, and create
bounded `EvidenceExport` requests. They cannot edit compiled leaves,
workloads, status subresources, Secrets, safety policy, telemetry/storage/
destination profiles, or operator RBAC. Query and watch APIs apply bounded
pagination, concurrency, bytes, and time ranges. Allowing arbitrary actor images,
commands, or Secret references delegates workload authority through the network
operator; denying direct Pod/Secret API access alone does not contain it. See
[Security and safety](security-and-safety.md).

## Client tests

- Server-side dry-run and schema discovery for every example.
- Watch logic handles relist, duplicate events, generation changes, and
  disappearing objects.
- Query clients preserve `Inconclusive`, provenance, truncation, and gaps.
- RBAC tests prove each supported verb and reject privileged shortcuts.
- End-to-end tests use an agent-like client that creates independent resources
  without any scenario API.

## Alternatives

| Alternative | Disposition |
| --- | --- |
| One `ExplorationSession` execution CRD | Rejected; it would become an orchestration engine. |
| Submit a complete expected scenario | Rejected as the primary model; distributed exploration is adaptive. |
| Agent directly patches workloads | Rejected in supported Roles; bypasses ownership and safe action contracts. |
| Controller returns diagnosis/next step | Rejected; observations are facts, agent owns reasoning. |

## Definition of done

- An agent can discover, authorize, submit, observe, and cancel every supported
  primitive through stable Kubernetes and query APIs.
- No supported client needs Pod names, direct workload mutation, or operator
  implementation imports.
- Every failure/gap remains machine-distinguishable.
- An agent can obtain the record needed for best-effort reproduction without
  any operator offering replay.

## Open decisions

1. Qualification of direct-backend reachability and its trust boundary (R7).
2. Whether a maintained Go client library is justified beyond generated
   Kubernetes clients and query schemas.
