# Native Chaos Mesh integration

Agents use Chaos Mesh CRDs directly for infrastructure faults. Chaos Mesh is an
optional upstream installation; network, action and observability operators work
without it. stacks-k8s supplies local installation helpers, targeting examples
and passive observation, but no fault wrapper, admission profile, experiment-agent
Role or scenario engine.

## Authority and selection

Kubernetes credentials define the agent's authority. The dedicated local
`stacks-k8s` cluster is disposable; a user may grant an agent namespace-wide or
cluster-admin access. stacks-k8s does not narrow that grant. The pinned upstream
installation filters injection by namespace, so an experiment namespace needs
`chaos-mesh.org/inject=enabled` before faults can affect its Pods.

For an actor-specific intervention, prefer the current network UID,
participant UID and `network.stacks.org/role: actor` labels. Resolve current
Pod UIDs and inspect native selections before treating the intervention as
applied. Labels can select replacement Pods; they do not fence a Pod UID.
An agent may intentionally select management, observation or node targets,
accepting that production, submission certainty or evidence capture may then
be lost. Cross-kind combinations, `Schedule` and `Workflow` remain native Chaos
Mesh APIs; the external agent decides whether and how to use them.

Put `network.stacks.org/network-uid` and
`actions.stacks.org/correlation-id` on each fault object's **metadata** for
network-scoped journal attribution. Selector labels alone do not associate a
fault record with a network. A missing match is a valid native object with no
measured target effect.

## Lifecycle and evidence

Chaos Mesh owns injection, recovery and finalizers. Fault objects are
user-owned, not children of `StacksNetwork`; root pause/deletion does not
cancel them. Delete faults and verify native cleanup before removing the
namespace opt-in or upstream controller. Duration expiry requests recovery,
but does not guarantee a deadline if the controller, daemon or node is
unavailable. Network reconnection does not undo chain changes or establish
protocol recovery.

The observability operator watches an explicit namespaced Chaos Mesh 2.8.4
resource list and records attributable objects, Events and source gaps without
mutating faults. It omits opaque fault payloads from recorded object bodies.
Native `AllInjected`/`AllRecovered` conditions establish controller action,
not packet-level effect or protocol recovery. An agent needs independent
before/during/after measurements, Pod identities and any original manifests
preserved outside the network namespace. See the
[operations guide](../chaos/operations.md) and
[observability guide](../observability/README.md).

## Qualification boundary

The [delay](../chaos/qualification.md) and
[partition](../chaos/partition-qualification.md) records establish specific
legacy-runtime outcomes on their recorded kind/containerd/kindnet platform.
Current actors, other Chaos kinds, CNIs and control-path failures need their
own effect and recovery checks. A protocol partition that is meant to preserve
baseline production must leave the producer's control path available; a fault
that deliberately removes that path tests a different hypothesis. No native
fault is assumed to stop mining merely because an RPC client times out.

The platform matrix for a new qualification should record Kubernetes, OS,
architecture, kernel, container runtime, CNI, Chaos Mesh version, selected Pod
UIDs, native status and measured effect. IO and time faults also depend on the
storage and clock mechanisms of that platform.
