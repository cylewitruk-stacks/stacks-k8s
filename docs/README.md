# Documentation

[Operator installation and execution workloads](design/operator-workloads.md)

## Operators

- [Stacks network operator](../charts/stacks-network-operator/README.md)
  - [API reference](network-operator/api.md)
  - [Architecture](network-operator/architecture.md)
  - [Operations](network-operator/operations.md)
  - [Bitcoin baseline production](network-operator/bitcoin-production.md)
  - [Custom Stacks actor images and mixed-version upgrades](network-operator/actor-images.md)
  - [Direct PoX-4/PoX-5 operation](network-operator/pox5.md)
  - [Stacks bootstrap and steady transfers](network-operator/stacks-production.md)
  - [Stacks qualification evidence](network-operator/stacks-qualification.md)
  - [Hacknet migration](network-operator/migration.md)
- [Stacks action operator](../charts/stacks-action-operator/README.md)
  - [Installation and operations](action-operator/operations.md)
- [Stacks observability operator](../charts/stacks-observability-operator/README.md)

## Native faults

- [Optional Chaos Mesh profile](../charts/stacks-chaos-profile/README.md)
- [Native network fault operations](chaos/operations.md)
- [Native delay qualification](chaos/qualification.md)
- [Native partition qualification](chaos/partition-qualification.md)

## Repository

- [Architecture](architecture.md)
- [Next-phase architecture design package](design/README.md)
- [Current capabilities and gaps](design/current-state.md)
- [Authoritative M0 remediation plan](design/m0-remediation-plan.md)
- [Steady-state operation and review gates](design/steady-state-operation.md)
- [Development](development.md)
- [Local Kubernetes 1.37 qualification](local-cluster-qualification.md)
- [Operations](operations.md)
- [Releases](releases.md)

See [network configuration and genesis](network-operator/configuration.md) for immutable shared inputs
and Go-owned provisioning/bootstrap.
