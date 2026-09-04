# Stacks Kubernetes operators

`stacks-k8s` contains independently deployable Kubernetes operators and Helm
charts for Stacks networks. The repository currently provides:

- `stacks-network-operator`, which compiles a declarative `StacksNetwork` into
  small actor resources and Kubernetes workloads; and
- `stacks-observability-operator`, which performs identity-bound, read-only
  observations of an admitted network topology.

The observability operator consumes the network operator's Kubernetes API and
the shared inventory wire contract. It does not import the network operator's
runtime implementation.

## Repository layout

| Path | Purpose |
| ---- | ------- |
| [`apis/`](apis/) | Independently versioned, types-only Kubernetes API modules. |
| [`charts/`](charts/) | Installable Helm charts and generated CRDs. |
| [`contracts/`](contracts/) | Versioned wire-contract fixtures shared across operators. |
| [`docs/`](docs/) | Architecture, development, operations, and release guidance. |
| [`examples/`](examples/) | Example topology and observation resources. |
| [`operators/`](operators/) | Independent controller-runtime modules and operator Dockerfiles. |

## Quick start

The chart defaults reference release images. Before the first published
release, build and load both operator and actor images as described in the
[network chart guide](charts/stacks-network-operator/README.md).

After the required images are available, install the topology operator and
create the minimal network:

```bash
helm upgrade --install stacks-network-operator \
  charts/stacks-network-operator \
  --namespace stacks-regtest \
  --create-namespace

kubectl --namespace stacks-regtest apply --filename examples/network/minimal.yaml
```

Install the observer after the network operator when trusted topology identity
is required:

```bash
helm upgrade --install stacks-observability-operator \
  charts/stacks-observability-operator \
  --namespace stacks-regtest

kubectl --namespace stacks-regtest apply \
  --filename examples/observability/identity.yaml
```

See the [documentation index](docs/README.md) for API, operational, and
development guidance.

## Verify

Go 1.27.1, Helm 3, and access to envtest assets are required.

```bash
make verify
make vuln
make docker-check
```

The complete verification preserves independent runtime modules, isolated
controller-tools dependency graphs, generated-CRD currency, exact rendered
RBAC, unit and race coverage, envtest lifecycle coverage, and Helm safety
checks.

## License

The operators, Helm charts, and documentation in this repository are licensed
under the [MIT License](LICENSE).
