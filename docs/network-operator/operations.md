# Operations guide

## Readiness

Start with the aggregate status:

```bash
kubectl get stacksnetwork NETWORK
kubectl describe stacksnetwork NETWORK
kubectl get bitcoinnodes,stacksnodes,stackssigners
```

The aggregate is `Ready` only when every leaf reports one Ready Pod at the
current StatefulSet revision with an immutable runtime image ID. Inspect the
leaf condition, StatefulSet, Pod events, init-container logs, and actor logs in
that order when an actor remains `Progressing`.

```bash
kubectl describe stacksnode NETWORK-ACTOR
kubectl get statefulset,pod -l network.stacks.org/actor=ACTOR
kubectl logs NETWORK-ACTOR-0 --all-containers
```

A generated Stacks node may need Bitcoin advancement before its RPC listener
becomes ready. Use an external client or the separately enabled
[Bitcoin baseline controller](bitcoin-production.md). The topology reconciler
does not issue mining RPCs. Bitcoin advancement alone does not supply Stacks
bootstrap inputs or transaction demand.

## Safe changes

Edit only the `StacksNetwork` for aggregate-owned actors. The controller
restores direct edits to its leaf resources.

- Actor image, process, resource, placement, and configuration changes roll
  only that actor.
- Changing a referenced actor rolls only consumers that list it as a direct
  dependency or in `serviceRefs`.
- Adding an actor creates a new leaf and workload.
- Removing an actor foreground-deletes its leaf; Kubernetes garbage collection
  removes leaf-owned resources.
- `spec.suspended: true` scales every actor to zero and withdraws the admitted
  inventory.

The inventory digest is intentionally unavailable during rollout. Consumers
must wait for `status.inventoryReady: true` after every topology change.

Actor-derived resource names remain readable (`NETWORK-ACTOR`). Two networks
whose names and actor names produce the same derived name cannot share a
namespace. The second network becomes `Degraded` with a derived-name-collision
diagnostic; choose distinct names or namespaces.

StatefulSet service identity, selector identity, and volume-claim shape are
immutable. The operator reports `Degraded` instead of deleting persistent
data. Introduce a replacement actor under a new logical name when an immutable
change is intentional.

The leaf controller restores mutable drift in an owned Service. If an owned
Service was allocated a normal ClusterIP instead of the required headless
identity, the controller deletes it with a UID precondition and recreates it.
It never adopts or replaces an object owned by another controller.

## Configuration updates

Inline and generated configuration carries its byte digest in the Pod template
and rolls automatically. For a referenced ConfigMap or Secret, update
`expectedDigest` with the object contents. The operator does not read Secret
bytes. Its init container verifies the mounted file before the actor starts.
Stacks node and signer processes render their effective configuration into a
2 MiB memory-backed volume, preventing Secret-derived values from being copied
to ordinary node ephemeral storage. Memory-backed `emptyDir` pages count
against Pod memory, so include at least this 2 MiB bound when sizing the actor's
memory request and limit.

If an external reference omits `expectedDigest`, the inventory records only a
declaration digest. Editing the referenced object does not trigger a rollout.

## Network policy

The chart does not create NetworkPolicy resources. Peer graphs describe actor
configuration, not cluster firewall policy. Apply namespace-appropriate
ingress, egress, DNS, API-server, and telemetry rules separately when the CNI
enforces network policy. A testing control plane may temporarily alter those
rules, but the topology operator remains their non-owner.

## Actor security context

Actor Pods disable service-account token mounting, privilege escalation, and
Linux capabilities, and use the runtime-default seccomp profile. The qualified
Bitcoin Core and Stacks images run as root, so actor containers do not set
`runAsNonRoot` and do not satisfy the Kubernetes Pod Security Standards
Restricted profile. Use a namespace policy compatible with those images. A
future non-root image profile requires separate live qualification before this
chart can claim Restricted admission.

Stacks node and signer images must include `/bin/bash`; the operator uses Bash
to render service and Pod-IP placeholders atomically before starting the actor.

## Controller availability

One replica is the default and leader election is enabled. Helm rejects an
unsafe multi-replica deployment without leader election. When election is
explicitly disabled for one replica, the manager Deployment uses `Recreate`;
an upgrade has brief control-plane downtime instead of overlapping writers.
Actor workloads continue running while the manager is unavailable. When it
returns, level-based reconciliation converges from current API state without
depending on missed watch events.

Invalid declarations become `Degraded` and are not hot-looped. Correct the
resource specification to advance its generation. Routine resource-version
conflicts, API throttling, API-server timeouts, and stale-cache create races
retry without replacing the last authoritative status. Direct-observation
failures withdraw stale readiness and inventory because admitted identity can
no longer be established.

## CRD upgrades

Helm installs CRDs on the first release but does not upgrade existing CRDs.
Before upgrading a pre-release installation, apply the chart's `crds/`
directory with the same reviewed chart version, then upgrade the release:

```bash
kubectl apply --server-side --force-conflicts \
  --filename charts/stacks-network-operator/crds
helm upgrade stacks-network-operator charts/stacks-network-operator \
  --namespace YOUR_NAMESPACE
```

Do not remove a served version until stored objects have been migrated.
The force flag deliberately transfers the CRD schema fields from Helm's
first-install field manager to this reviewed chart version; inspect the diff
before using it with CRDs managed by another release process.

## Teardown

Delete every `StacksNetwork` and wait for its leaf resources and workloads to
disappear before uninstalling the manager. A retained PVC is intentional when
`storage.retainOnDelete` is true and must be removed separately.

## Release qualification

The development [README](../../operators/network/README.md) documents the opt-in Go live
suite. It uses lightweight actor processes to isolate controller and
Kubernetes lifecycle behavior. A release candidate must additionally run the
documented minimal example with real Bitcoin Core and Stacks node images and
advance the external regtest burnchain until both actors are Ready.

## Stacks transfer profile

For productive miner/signer operation, use the
[bootstrap and transfer guide](stacks-production.md). Kubernetes readiness and
`TransactionsConfigured` do not establish signer participation or block progress.
Inspect exact transfer inclusion and protocol tips separately. Delete both
production ledgers through parent disposal before removing their controllers.
