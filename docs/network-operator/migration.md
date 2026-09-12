# Installation compatibility

The network and action operators serve the composable `v1alpha2` APIs. They do
not convert stored `v1alpha1` networks, ledgers or action requests. Namespaces cannot
isolate incompatible CRDs with the same cluster-wide name.

Before replacing an older installation, inventory custom resources, retained PVCs,
workloads and Helm releases. Collect required evidence, destroy disposable networks
through their owning controllers, then uninstall the old operators. Remove obsolete
CRDs only after confirming they have no state that must survive. Install the current
charts and create fresh network identities from current declarations.

## Preview chart rename

The development preview used `stacks-network-foundation` and
`stacks-action-foundation`. The final products are `stacks-network-operator` and
`stacks-action-operator`. Complete and delete preview experiments before changing
packaging: workload manager labels change with the product name. Do not run both
controller installations concurrently.

Helm installs files under `crds/` only when absent; it does not upgrade their schema
or remove them on uninstall. For existing compatible `v1alpha2` CRDs, explicitly
apply the new chart's generated CRDs before installing/upgrading the renamed chart:

```bash
kubectl apply --server-side -f charts/stacks-network-operator/crds/
kubectl apply --server-side -f charts/stacks-action-operator/crds/
```

Review schema conflicts instead of forcing ownership blindly. If CRDs carry Helm
ownership annotations, preserve or deliberately transfer them to the chosen release;
they must not point to two owners. Chart lookup guards reject incompatible served
versions but do not migrate data. Use fresh namespace/root identities for the
post-transition qualification.
