# Agent guidance

This file applies to the entire repository. Keep changes focused, idiomatic,
and consistent with the architecture documented under [`docs/`](docs/).

## Architecture invariants

- Treat each operator as an independently deployable and versioned product.
- The network operator owns desired topology and its Kubernetes workloads.
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
- Preserve independent API, runtime, and `tools` Go modules. Do not add a
  committed root `go.work` or otherwise unify their dependency graphs.
- Keep repository-wide verification logic under top-level `tools/` rather than
  duplicating it across independent operator runtime modules.
- Prefer structural OpenAPI schemas and CEL for static admission rules. Add a
  webhook only when an invariant requires live cluster state or cannot
  reasonably be expressed statically.

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
