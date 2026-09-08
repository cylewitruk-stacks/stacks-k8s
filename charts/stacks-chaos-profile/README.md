# Native Chaos Mesh profile

Optional admission and agent-access chart for direct native `NetworkChaos` use.
It deploys no operator, scenario wrapper, or upstream CRDs. The network, action,
and observability charts remain independently deployable.

| Value | Default | Meaning |
| --- | --- | --- |
| `networkDelay.enabled` | `false` | Enable bounded directed delay. |
| `networkPartition.enabled` | `false` | Enable bounded bidirectional partition. |
| `chaosMesh.externalVersion` | `""` | Must explicitly equal `2.8.4` when enabled. Administrator assertion, not runtime discovery. |
| `agent.serviceAccountName` | `stacks-chaos-agent` | Namespaced identity receiving native-fault access; token automount disabled. |

Install once per experiment namespace, after the pinned external Chaos Mesh
installation. See [installation and operation](../../docs/chaos/operations.md)
and [delay](../../docs/chaos/qualification.md) /
[partition qualification](../../docs/chaos/partition-qualification.md).

Either enabled mode renders the same six resources; enabling both retains one
shared quota and agent Role. The enabled chart renders one ValidatingAdmissionPolicy, its namespace-bound
Deny binding, a ServiceAccount, Role, RoleBinding, and ResourceQuota. Admission
requires the namespace label `network.stacks.org/chaos-profile=network-faults-v1`
and annotation `chaos-mesh.org/inject=enabled` for creation. The binding uses
the namespace's immutable `kubernetes.io/metadata.name` label, so removing
enrollment does not bypass admission. Existing controller updates and deletion
remain possible after de-enrollment.

The profile permits one source actor and one distinct remote actor in the same
network and namespace, selected only by exact network/actor labels. It requires
`mode: one` on both sides and an integer 1–120 s duration. Enabled delay uses
`action: delay`, `direction: to`, and 1–1000 ms latency. Enabled partition uses
`action: partition`, `direction: both`, and no delay parameters. Both affect all
traffic between the selected Pods, including actor RPC; the producer is unselected.
Jitter, reordering, other fault modes,
raw Pod/IP selectors, remote clusters, and device overrides are excluded.
The complete spec is immutable. Lifecycle state, owner references, finalizers,
managed-by labels, and pause annotations cannot be supplied at creation; the
upstream empty `status.experiment` default is accepted.

Shape and bounds are checked on creation. Faults that predate profile installation
retain their original specs and can still receive controller status/metadata
updates and finalize deletion, even when outside this profile. Spec changes are
rejected for both existing and newly admitted faults. Installing the profile
does not make an existing fault conform or cancel it.

The agent Role permits only get/list/watch/create/delete of `NetworkChaos` in
this namespace. It grants no status writes, Secret access, workload mutation,
Schedule, Workflow, or other fault kinds. Kubernetes RBAC is additive: do not
combine this identity with broader mutation grants. Administrator access to
install cluster-scoped admission is separate from experiment access.

The quota allows **one existing NetworkChaos object**, including recovered or
terminating objects. Delete and await cleanup before creating the next fault.
This is an object-count bound, not a cross-kind scheduling mechanism.

Kubernetes 1.30 is the minimum admission API version; the tested runtime matrix
is narrower. Native cleanup needs a functioning upstream controller and daemon;
`duration` bounds the request, not recovery during infrastructure failure.

## Upgrade from 0.1.0

Version 0.2.0 uses `network-faults-v1` enrollment and shared resource names
`stacks-network-faults-<namespace>`. Finish and delete all native faults before
upgrading. Remove the old enrollment label, upgrade the existing Helm release,
and set the new enrollment value only after verifying its Deny binding and quota.
Keep `chaos-mesh.org/inject=enabled` throughout cleanup. Do not install a second
profile release over the old one. The delay/partition enablement values choose
allowed new faults; disabling a mode does not cancel an existing object.

## Verification

The CI chart-policy job and `make verify` include rendered permissions, admission
against the checksum-verified upstream CRD in envtest, and Helm checks. The
upstream CRD is downloaded into a temporary cache, never modified or vendored.
Set `STACKS_CHAOS_CRD_FILE` to the identical file for offline testing. Live
qualification is separately opt-in and requires the documented disposable fixture.
