# Operations

Install the network operator once; it watches networks across namespaces.
Install optional action and observation releases in the namespaces they serve.
Neither optional chart provisions the network operator or substitutes for its CRDs.

- [Network installation](../charts/stacks-network-operator/README.md)
- [Network controls and cleanup](network-operator/operations.md)
- [Bounded action installation](../charts/stacks-action-operator/README.md)
- [Read-only observation](../charts/stacks-observability-operator/README.md)
- [Native faults](chaos/operations.md)
- [Local cluster lifecycle](development.md#local-kind-cluster)

Use fresh namespaces/root identities for independent experiments and leave the
shared development cluster running. Helm does not upgrade or remove bundled CRDs.
Review [installation compatibility](network-operator/migration.md) before replacing
schemas, images or chart products.
