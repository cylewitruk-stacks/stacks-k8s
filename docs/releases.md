# Releases

The three operators are independently versioned products in one repository.
Each release updates its chart `version`, chart `appVersion`, image tag, and
operator release notes together.

Use component-prefixed product tags:

```text
stacks-network-operator/v0.1.0
stacks-observability-operator/v0.1.0
stacks-action-operator/v0.1.0
```

If a Go module is published for external import, its semantic-version tag must
match its repository subdirectory instead:

```text
apis/network/v0.1.0
operators/network/v0.1.0
operators/observability/v0.1.0
operators/action/v0.1.0
```

Publish Go module tags in dependency order: `apis/network`, then
`operators/action`, then `operators/network`. The network module’s paired tests
require the action module tag even though its production binary does not import
the action runtime. The observability module has no dependency on that pair.
Repository builds use a local `replace`, but published consumers ignore that
directive and must be able to resolve the declared API tag. The API module
depends only on Kubernetes API machinery and follows the network API's
compatibility lifecycle; it never includes controller implementations.

Operator binaries are distributed as signed container images through their
Helm charts. Version-qualified `go install` is not a supported distribution
path because repository builds intentionally use a local API-module
replacement.

Before tagging:

1. run `make verify`, `make vuln`, `make docker-check`, and `make docker-build`;
2. build and publish an immutable multi-architecture image;
3. set the chart image tag to the released image version;
4. install the packaged chart in a clean cluster and exercise its examples;
5. publish each chart independently as an OCI artifact.

Repository placement does not imply compatibility across arbitrary operator
versions. Document the network API and inventory-contract versions accepted by
each observability release.

## Worker artifacts

The network image contains its operator/resolver entrypoint and scoped Go worker
binary. Qualify controller and worker images with the same API/chart version and
selected actor profile. Bitcoin control and Stacks management have different restart
contracts; do not infer a safe management-worker restart from an operator rollout.

`operators/network/transactions` is an offline SDK oracle only. Include its pinned
test dependencies in repository auditing, but do not package Node.js in runtime
images. Publish `libs/stacks` and `libs/bitcoin` before consumers that require their module versions.

Unpublished incompatible chart/schema changes require fresh test installations.
See [installation compatibility](network-operator/migration.md) for explicit CRD
handling; Helm does not perform API conversion or schema upgrades automatically.

## Prepared 0.2.0 changes

Network and observability charts/appVersions are prepared as 0.2.0; the action operator
version is unchanged. Development controller image tags remain `dev` until publication.
The observer pin is declared once in the network chart
[values.yaml](../charts/stacks-network-operator/values.yaml). Local builds must load that
image or explicitly override `bitcoinObserverImage`, independently of the controller tag.

Network changes include optional read-only Bitcoin observer sidecars, independent image
selection and attributed burnchain diagnostic status. Observability changes include
mandatory actor resource samples, native fault sources, resilient watch reconnection and
the larger local query profile. Apply generated CRD schema updates explicitly before
upgrading; Helm does not upgrade bundled CRDs. These are prepared versions, not a claim
that images or charts have been published.
