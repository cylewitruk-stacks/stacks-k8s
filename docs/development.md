# Development

## Prerequisites

- Go 1.27.1
- Helm 3
- Docker for container validation and local images
- Network access when envtest assets or vulnerability data are not cached

No committed `go.work` is required. The modules are intentionally verified in
isolation so local workspace state cannot hide a missing dependency or combine
the API/runtime Kubernetes 0.36 graphs with controller-tools' Kubernetes 0.37
graph.

## Common commands

```bash
make fmt
make generate
make test
make test-race
make test-integration
make helm-verify
make rbac-verify
make modules-verify
make verify
```

`make generate` runs the isolated generator under `apis/network/tools`, updates
generated deepcopy code, and writes network CRDs directly to the owning chart.
Generated code and CRDs must be committed with their API source changes; CI
rejects drift.

`make helm-verify` validates values schemas, renders supported configurations,
and structurally checks the manager Deployment's security posture. It also
executes negative renders for unsafe replica settings and malformed image-pull
secret or service-account names. The shared structural validator lives under
`tools/chart-policy` and is not part of either operator binary.

`make docker-check` validates both Dockerfiles, while `make docker-build`
compiles both operator images from the repository's default-deny root build
context. CI runs both so sibling-module changes cannot silently break image
builds.

The network runtime consumes the types-only `apis/network` module through a
repository-local replacement. Published releases tag the API module first so
that the runtime module's declared version remains resolvable without that
replacement.

Changes to admitted inventory, leaf specifications, actor Service ports, or
kubelet image-ID parsing must update the relevant fixture under
[`contracts/`](../contracts/). Each operator verifies those fixtures through
its own production code; do not replace that boundary with a shared runtime
package.

Run the opt-in checks separately when their external services are available:

```bash
make vuln
make docker-check
```

Keep controller packages aligned with the Kubernetes resource they reconcile.
Small cohesive packages are valid boundaries; merge them only when measured
coupling or duplication demonstrates a clearer design.
