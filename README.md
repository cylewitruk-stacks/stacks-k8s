# Stacks Kubernetes operators

`stacks-k8s` provisions disposable Stacks regtest networks for development,
reliability, liveness, resilience, performance and defensive security investigations.
Trusted agents can introduce adversarial conditions and verify reported behavior;
operators expose capabilities and evidence rather than plan experiments.

Apply a `StacksNetwork` to resolve selected reusable definitions, freeze genesis,
and run its participants. The network reconciles Bitcoin production, Stacks nodes
and consensus signers, transaction demand, PoX enrollment/renewal and sBTC contract
initialization. Genesis funding does not require managed participation: additional
funded accounts can be used by later experiment actors.

Three independently installed products provide:

- [Network operation](charts/stacks-network-operator/README.md): reusable accounts
  and definitions, network-owned actors and scoped Go management workers.
- [Bounded Bitcoin actions](charts/stacks-action-operator/README.md): finite block
  generation and reorganization through the network's shared execution authority.
- [Read-only observation](charts/stacks-observability-operator/README.md): verified
  runtime identity and observations, without control of network behavior.

The network operates without actions or observability. The optional
[Chaos Mesh profile](docs/chaos/operations.md) supports bounded actor-to-actor
network delay and partition while keeping management traffic separate.

## Local use

The development target is Kubernetes 1.37.0. Create a three-node kind cluster
with Headlamp and Metrics Server:

```bash
make cluster-create
export KUBECONFIG="$PWD/tools/local-cluster/kubeconfig"
```

Build and load the operator and compatible Stacks actor images, then install the
[network chart](charts/stacks-network-operator/README.md). Images built from local
revisions are supported; see [actor images](docs/network-operator/actor-images.md).

The [30-actor example](docs/design/public-api/examples/30-actors.yaml) includes
accounts, definitions and a paused root. Set its image references and placement
for your cluster before applying it. It requires no external bootstrap command:

```bash
kubectl --context kind-stacks-k8s apply -f docs/design/public-api/examples/30-actors.yaml
kubectl --context kind-stacks-k8s -n lab-30 patch stacksnetwork network \
  --type=merge -p '{"spec":{"operation":"Running"}}'
kubectl --context kind-stacks-k8s -n lab-30 get stacksnetwork,stacksnetworkparticipant
```

Initialization waits for native protocol evidence, not merely Ready Pods or
Bitcoin heights. At a five-second Bitcoin cadence, reaching height 300 takes
at least 25 minutes, plus initialization holds. Follow
[operations](docs/network-operator/operations.md) for pause, terminal stop,
destructive removal and cleanup.

## Repository

| Path | Purpose |
| --- | --- |
| [apis/](apis/) | Independently versioned Kubernetes types and schemas |
| [libs/](libs/) | Portable Go protocol libraries without Kubernetes dependencies |
| [operators/](operators/) | Independent controller and worker Go modules |
| [charts/](charts/) | Helm products and generated CRDs |
| [contracts/](contracts/) | Versioned wire-contract fixtures |
| [docs/](docs/) | Public API, operation, design and qualification evidence |
| [tools/](tools/) | Repository checks and local cluster lifecycle helpers |

Go owns runtime configuration, signing, RPC, reconciliation and execution.
Stacks.js is a pinned, offline test oracle only; runtime images contain no Node.js.
Run `make verify` before handoff, and `make vuln` / `make docker-check` when
changing dependencies or containers. See [development](docs/development.md).
