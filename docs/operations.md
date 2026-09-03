# Operations

Install the network operator before the observability operator. Both charts
watch only their release namespace, so install them in each namespace that
contains managed resources.

- [Network operator installation and usage](../charts/stacks-network-operator/README.md)
- [Network API](network-operator/api.md)
- [Network operations](network-operator/operations.md)
- [Observability operator usage](../charts/stacks-observability-operator/README.md)

The observability chart reads network resources but does not install the
network CRDs. Its installation therefore does not replace the topology chart.

Helm installs files under `crds/` only on first installation and does not
upgrade them. Follow the owning operator's documented CRD upgrade procedure
before upgrading an existing release.
