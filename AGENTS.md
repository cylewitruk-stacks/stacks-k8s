# Agent guidance

This file applies to the entire repository. Keep changes focused, idiomatic,
and consistent with the architecture documented under [`docs/`](docs/).

## Product purpose

Keep components modular and independently useful: `StacksNetwork` must support
ordinary regtest development and testing without requiring an agent, action
controllers, or observability tooling. Design for network reliability,
liveness, resilience, performance, and efficiency investigations alongside
defensive security testing. A primary use is investigation by trusted
cybersecurity agents in disposable, reasonably realistic networks, including
discovery of critical issues beyond conventional tests and static analysis and
verification of behavior from responsibly reported vulnerability reports.
Preserve bounded permissions, external-agent orchestration, and evidence
integrity. See the
[product goals](docs/design/README.md#product-goals).

## Architecture invariants

- Treat each operator as an independently deployable and versioned product.
- `StacksNetwork` declares baseline topology and supported ongoing behavior.
  The aggregate controller compiles owned resources; separate capability
  controllers perform initialization, production and protocol maintenance.
  Initialization is convergence toward declared network state, not an external
  experiment plan. Follow the [steady-state design](docs/design/steady-state-operation.md).
- Install the network operator independently of individual networks. The aggregate
  compiles capability resources; capability workload controllers provision scoped
  execution workloads in each network namespace. Keep signing keys out of the
  operator process and consensus signer administration outside signer Pods.
- `BitcoinNode` describes a Bitcoin Core instance without a mining role.
  `BitcoinBlockProduction` and bounded actions own block generation;
  `StacksNode` retains its mining role.
- The observability operator is read-only with respect to the environment it
  observes. It must not mutate network topology, action resources, or actor
  workloads, or import the network operator's runtime implementation. It may
  write its own resources and configured journal or evidence sinks.
- The agent is the orchestration and reasoning layer. Operators expose small,
  safe, composable capabilities; they must not become an in-cluster scenario
  planner, deterministic replay engine, reducer, or root-cause classifier.
- Observability records and exposes facts by collecting, correlating,
  retaining, querying, and exporting topology changes, action lifecycles,
  telemetry, capture gaps, and identity metadata. It must not replay journals
  or direct an experiment.
- Each action resource represents one small, bounded, independently
  observable action. Do not introduce an in-cluster scenario or playbook
  resource containing an ordered execution plan.
- Long-lived desired operation, such as `BitcoinBlockProduction`, is mutable
  and separate from the bounded-action lifecycle. It maintains one ongoing
  capability without terminal completion claims or action sequencing.
- Baseline timing and target selection are distinct. Temporary overrides
  resume the latest baseline; they do not restore an old spec snapshot.
- Validate each capability's required target identity independently of global
  health. Never treat stale or partial aggregate inventory as complete.
- Qualify protocol fault paths separately from production control access.
  Losing RPC control does not prove that an actor stopped mining. Client
  deadlines and Lease expiry do not establish server-side quiescence.
- Deterministic distributed-system outcomes are not a product goal. Preserve
  identity and evidence integrity so an agent can attempt best-effort replay,
  verify an issue semantically, and derive a confirmed reduced reproducer
  without claiming causal minimality or identical execution. Deterministic
  compilation and content digests remain required where they define API or
  evidence identity; they do not promise deterministic runtime behavior.
- Keep controllers small and resource-focused. Compose behavior through
  Kubernetes APIs and explicit interfaces rather than monolithic reconcilers.
- Put shared, versioned Kubernetes API types in `apis/`; never put controller
  runtime code there. Keep wire-contract fixtures in `contracts/`. Consumers
  that verify byte-level contracts must continue to use their own production
  decoding and digest implementations.
- Keep portable protocol libraries under `libs/`, without Kubernetes, API-module,
  or operator dependencies. Consumers use versioned requirements and local
  `replace` directives, as for `apis/network`; register library verification in
  the root module gates.
- Preserve independent API, runtime, and `tools` Go modules. Do not add a
  committed root `go.work` or otherwise unify their dependency graphs.
- Keep repository-wide verification logic under top-level `tools/` rather than
  duplicating it across independent operator runtime modules.
- Prefer structural OpenAPI schemas and CEL for static admission rules. Add a
  webhook only when an invariant requires live cluster state or cannot
  reasonably be expressed statically.

## Language boundary

- Go is the implementation language for operators and repository tooling.
  Keep Kubernetes APIs, reconciliation, admission, production nonce ownership,
  submission/recovery, and evidence accounting in Go.
- JavaScript is test-only, confined to `operators/network/transactions` as a pinned
  offline Stacks.js oracle for portable Go protocol implementation. Runtime images
  and workers must not invoke it. It must not query clusters, render configuration,
  submit transactions or sequence network operation. Funded genesis accounts do
  not imply managed participation.
- Keep repository wire-contract fixtures in `contracts/` and their production
  decoding/digest implementations in the respective Go consumers. Do not move
  or duplicate these contracts into JavaScript fixtures or modules.
- Do not infer permission for broader JavaScript/TypeScript use from the SDK
  package. Additional JavaScript/TypeScript packages or moving operator/tooling
  logic into those languages require explicit user direction to change this
  language boundary.

## Kubernetes and Go

- Follow idiomatic Go, SOLID/DRY principles, and `controller-runtime`
  conventions.
- Make reconciliation idempotent, level-based, ownership-aware, and safe under
  retries, conflicts, deletion, and eventually consistent caches.
- Use uncached API reads where correctness depends on current admitted
  identity. Fail closed when identity cannot be established.
- Keep RBAC least-privileged. Changes to permissions must update and pass the
  exact rendered-RBAC allowlist checks.
- Keep Kubernetes, controller-runtime, and controller-tools versions within
  their supported compatibility families. Generator dependencies stay in the
  isolated `tools` modules.

## Generated files

- Do not edit generated deepcopy files or chart CRDs by hand.
- Run `make generate` after changing API types or kubebuilder markers.
  Extracted shared APIs keep generators in the corresponding `apis/*/tools`
  module; operator-owned APIs may keep them under the operator module. CRDs
  are generated directly into their owning chart.
- Commit generated outputs with the source change that produced them.

## Validation

Run the narrowest relevant tests while iterating. Before handing off a complete
change, run:

```bash
make verify
```

Also run these when dependency, container, or supply-chain behavior changes:

```bash
make vuln
make docker-check
```

Use the existing `stacks-k8s` kind cluster for development and qualification by
default, and leave it running. Install, upgrade or uninstall charts as needed;
use fresh namespaces and environment identities for independent experiments.
Create another cluster only for cluster lifecycle tests or incompatible
cluster-wide configuration that requires isolation.

For now, do not preserve failed fixtures automatically. Capture the failure and
a small relevant set of logs/status, then clean up the test namespace in the
documented order. Retain a failed environment only for an active investigation
or when the user explicitly requests it; record any retained environments.

Do not weaken, skip, or make tests vacuous to obtain a passing result. Add unit
tests for controller logic and envtest coverage for API-server or lifecycle
semantics.

## Documentation and releases

- Update API, chart, example, operator, and operations documentation whenever
  their behavior or configuration changes.
- Keep Markdown concise, language-tag code fences, and use spaced table
  separators such as `| --- | --- |`.
- The repository is MIT-licensed. New source, charts, and documentation must be
  compatible with that license.
- Preserve independent operator image, chart, and release versioning.

## Git hygiene

- Do not change repository or global Git identity or signing configuration.
- Do not create commits unless the user explicitly requests one. Leave
  hardware-key signing to the user.
- Preserve unrelated local changes and never use destructive Git commands to
  discard them.
