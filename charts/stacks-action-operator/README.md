# Stacks action operator

Optional, independently installed `actions.stacks.org/v1alpha2` lifecycle controllers
for finite Bitcoin generation and bounded local reorganization. The controllers read
public network/execution records and write action status/finalizers. The existing
Bitcoin control worker remains the sole RPC sender.

Install in the network namespace after enabling the matching network chart values:
`bitcoinActions.generationEnabled` and, explicitly, `bitcoinActions.reorganizationEnabled`.
Both network-side values default to false. This chart enables generation by default;
set `bitcoinReorganization.enabled=true` to add the reorganization lifecycle controller.
Installing lifecycle controllers alone grants no Bitcoin mutation authority.

```bash
helm upgrade --install actions charts/stacks-action-operator \
  --namespace lab --set image.tag=dev
```

The two CRDs replace incompatible legacy action schemas. The chart rejects observed
non-v1alpha2 CRDs; it never deletes or converts old resources. Helm does not upgrade
CRDs automatically. Review schema updates before applying them explicitly.

Requests pin `spec.networkUID` and a logical BitcoinNode participant. Specs are
immutable. Generation sends one block at a time; reorganization admits depth 1–6,
sends exactly depth+1 replacement blocks, and compensates only its captured invalidity
marker. Cleanup does not restore an old best chain. Every generation dispatch obeys
the network's current immutable initialization ceiling.

A namespace quota allows 64 requests per kind. The worker sorts eligible requests by
creation time and UID, then reserves its existing execution record with a versioned
write. A lost Armed acknowledgement never causes replay. Known receipts retain the
reservation until the lifecycle controller publishes their terminal acknowledgement.
Ambiguous execution retains exclusion; deleting an individual action does not clear it.
Explicit environment disposal permits removal of the action retention finalizer.

Each Role is namespaced. Lifecycle controllers receive no Secret access and cannot
write execution records, actor workloads, or network status. Workers receive public
request reads only for enabled kinds. Exact rendered permissions are tested in the
independent action module.

Change installation enablement flags only after action reservations settle or the
environment is disposed. Disabling a kind removes its read permission; it is not
cancellation and can strand retained uncertainty or cleanup. Use action deletion or
network pause for runtime cancellation while the controller and worker retain access.
