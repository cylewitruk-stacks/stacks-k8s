# Repository architecture

Each operator is independently deployable and versioned, with its own Go module,
image and chart. Shared Kubernetes types live in `apis/network`; portable protocol
code lives in `libs/stacks`. Neither contains controller implementations.

| Component | Responsibility |
| --- | --- |
| Network aggregate | Resolve explicit composition and publish whole-policy admission |
| Network domain controllers | Provision participant-owned actor and management workloads |
| Scoped workers | Execute declared initialization/production and publish protocol facts |
| Action operator | Project one bounded request's lifecycle; network executor sends Bitcoin RPCs |
| Observation operator | Verify public actor identity through read-only API access |
| External user/agent | Choose experiments, changes, evidence collection and conclusions |

See [network architecture](network-operator/architecture.md) and the
[public resource model](design/public-api/README.md) for ownership and lifecycle.
No operator acts as an in-cluster scenario planner, replay engine or classifier.

## Dependency boundaries

Operators consume versioned APIs and portable libraries through independent module
requirements and local replacements. The observer does not import the network's
runtime. Paired network/action tests may exercise both implementations; that does
not create a production runtime dependency. Repository wire fixtures under
[contracts/](../contracts/) are verified through each consumer's own implementation.

Go owns configuration, RPC, signing and execution. The pinned Stacks.js package is
an offline test oracle only. Generated schemas/deepcopies come from isolated tooling
modules; no committed root `go.work` unifies the dependency graphs.
