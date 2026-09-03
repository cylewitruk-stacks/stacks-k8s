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
operator is read-only and consumes published Kubernetes resources through
their wire format. It intentionally does not import the network operator's Go
implementation.

The versioned fixtures in [`contracts/`](../contracts/) pin the shared
admitted-inventory digest, leaf-specification digest, actor Service ports, and
immutable image-ID parsing contracts. Both operators decode the same vectors
into their own native types and verify them through their production
implementations.

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
