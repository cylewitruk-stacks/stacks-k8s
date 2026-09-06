# Stacks Kubernetes operators

`stacks-k8s` contains independently deployable Kubernetes operators and Helm
charts for Stacks networks. Intended uses include general regtest development
and testing; network reliability, liveness, resilience, performance, and
efficiency investigations; defensive security testing; and attempts to verify
behavior described in responsibly reported vulnerability reports.
`StacksNetwork` is usable independently of agents, action controllers, and
observability tooling.

A primary product goal is defensive security testing: enable trusted
cybersecurity agents to simulate adversarial conditions in
disposable, reasonably realistic `StacksNetwork`s and identify critical issues
that are difficult to expose through unit tests, integration tests, or static
analysis alone.

In the target architecture, the external agent directs investigations within
granted permissions. Operators will provide bounded action controls and
expanded passive, identity-bound evidence so the agent can investigate and
verify suspected issues. See the
[product goals](docs/design/README.md#product-goals) for the intended direction.

The [steady-state design](docs/design/steady-state-operation.md) makes
`StacksNetwork` the declaration of baseline topology and operation: neutral
Bitcoin nodes, a separate block-production policy with timing and target
selection, Stacks miner configuration, and ongoing transaction demand.
The first [Bitcoin baseline profile](docs/network-operator/bitcoin-production.md)
is implemented: weighted targets, a fixed policy cadence, pause, and independent
durable execution ledgers.
The initial [Stacks transfer profile](docs/network-operator/stacks-production.md)
adds isolated, fixed-interval STX demand and external bootstrap/signing helpers.
Timing jitter and broader production profiles remain design work.

The repository currently provides:

- `stacks-network-operator`, which compiles a declarative `StacksNetwork` into
  small actor resources and Kubernetes workloads;
- `stacks-observability-operator`, which performs identity-bound, read-only
  observations of an admitted network topology; and
- `stacks-action-operator`, which owns bounded Bitcoin generation and optional
  reorganization lifecycles through the network worker’s shared executor.

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

The topology-only quick start requires an external Bitcoin mining client.
For automatic blocks, use the separately enabled
[Bitcoin production guide](docs/network-operator/bitcoin-production.md).
For a productive miner/signer network with tiny STX transfers, follow the
[Stacks bootstrap and transfer guide](docs/network-operator/stacks-production.md).
Stacks RPC readiness may depend on advancing the Bitcoin chain. See the
[readiness guidance](docs/network-operator/operations.md#readiness).

For finite block generation, install the separate
[action chart](charts/stacks-action-operator/README.md) and enable the network
executor’s matching capability.

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

Go 1.27.1, Node.js 24 or newer, npm, Helm 3, and access to envtest assets are required.
`make verify` runs `npm ci`; it needs npm registry access or a fully populated
npm cache. See [offline verification](docs/development.md#stacks-transfer-profile).

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
