# Native Chaos Mesh profile

Optional admission and agent-access chart for direct native `NetworkChaos` use.
It deploys no operator, scenario wrapper, or upstream CRDs. The network, action,
and observability charts remain independently deployable.

| Value | Default | Meaning |
| --- | --- | --- |
| `networkDelay.enabled` | `false` | Render the initial bounded delay profile. |
| `chaosMesh.externalVersion` | `""` | Must explicitly equal `2.8.4` when enabled. Administrator assertion, not runtime discovery. |
| `agent.serviceAccountName` | `stacks-chaos-agent` | Namespaced identity receiving native-fault access; token automount disabled. |

Install once per experiment namespace, after the pinned external Chaos Mesh
installation. See [installation and operation](../../docs/chaos/operations.md)
and [qualification](../../docs/chaos/qualification.md).

The enabled chart renders one ValidatingAdmissionPolicy, its namespace-bound
Deny binding, a ServiceAccount, Role, RoleBinding, and ResourceQuota. Admission
requires the namespace label `network.stacks.org/chaos-profile=network-delay-v1`
and annotation `chaos-mesh.org/inject=enabled` for creation. The binding uses
the namespace's immutable `kubernetes.io/metadata.name` label, so removing
enrollment does not bypass admission. Existing controller updates and deletion
remain possible after de-enrollment.

The profile permits one source actor and one distinct remote actor in the same
network and namespace, selected only by exact network/actor labels. It requires
`action: delay`, `mode: one`, target `mode: one`, `direction: to`, 1–1000 ms
latency, and an integer 1–120 s duration. Jitter, reordering, other fault modes,
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

## Verification

The CI chart-policy job and `make verify` include rendered permissions, admission
against the checksum-verified upstream CRD in envtest, and Helm checks. The
upstream CRD is downloaded into a temporary cache, never modified or vendored.
Set `STACKS_CHAOS_CRD_FILE` to the identical file for offline testing. Live
qualification is separately opt-in and requires the documented disposable fixture.
