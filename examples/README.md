# Examples

- [`network/`](network/) contains `StacksNetwork` topology examples.
- [`observability/`](observability/) contains one-shot trusted-observation
  examples.

Install the owning chart before applying an example.

For a Bitcoin 31.1 network that generates blocks automatically, follow the
[baseline guide](../docs/network-operator/bitcoin-production.md). Its generator
creates the network plus fresh credentials and configuration; credentials are
not committed as static example manifests.

The examples are source-checkout templates, not image-distribution manifests.
`network/minimal.yaml` expects `stacks-core-network:local`; build that actor
image and load it into the target cluster, or replace it with an accessible
image reference. Apply the examples only after every referenced image is
available to the cluster.
