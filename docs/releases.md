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

## Transaction worker artifact

The optional `stacks-transaction-worker` image is built from
`operators/network/transactions/Dockerfile` and configured independently through
`workers.sdkImage`. Qualify it with the network API/chart version and
selected actor image. Include its locked npm dependencies in release scanning
and provenance; the local [Stacks qualification](network-operator/stacks-qualification.md)
is not a published image support matrix.

## Unpublished network chart value changes

The local chart remains `0.1.0` before its first publication. The workload refactor
removes these previously accepted values; the strict schema rejects old values
files rather than silently ignoring them.

| Removed value | Replacement |
| --- | --- |
| `bitcoinProduction.credentialsSecret` | `StacksNetwork.spec.bitcoinBlockProduction.credentialsSecret` |
| `stacksTransactions.credentialsSecret` | `StacksNetwork.spec.stacksTransactionProduction.credentialsSecret` |
| `stacksTransactions.image` | `workers.sdkImage` |
| `stacksOperation.image` | `workers.sdkImage` |

The chart now installs one shared operator, while capabilities own their workers.
See the [upgrade procedure](../charts/stacks-network-operator/README.md#upgrading-execution-workers)
before replacing images in an existing installation. Published chart versions
must describe incompatible value changes in their release notes.
