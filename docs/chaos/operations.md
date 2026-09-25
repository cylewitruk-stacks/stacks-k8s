# Native Chaos Mesh faults

Chaos Mesh is optional. Agents with authority over a disposable experiment namespace
can create native Chaos Mesh faults directly; stacks-k8s does not install a fault
profile, impose a fault quota, or grant experiment-agent permissions. The network
and action operators do not depend on Chaos Mesh.

## Install and enroll

On the repository's `stacks-k8s` kind cluster, run:

```bash
make cluster-chaos-install
```

This installs the pinned upstream Chaos Mesh chart with
[`upstream-values.yaml`](../../examples/chaos/upstream-values.yaml). Its controller
filters namespaces. Enable injection on each experiment namespace before creating
faults:

```bash
kubectl --kubeconfig tools/local-cluster/kubeconfig --context kind-stacks-k8s \
  annotate namespace chaos-live chaos-mesh.org/inject=enabled
```

Other clusters need a compatible Chaos Mesh installation and the same namespace
opt-in when upstream namespace filtering is enabled. The pinned version and
platform evidence are in the [qualification matrix](qualification.md). Select
Kubernetes credentials appropriate to the agent's granted authority; stacks-k8s
does not provision an experiment-agent ServiceAccount. Confirm the context and
namespace before creating or deleting a fault.

## Select and observe

Start with the [delay](../../examples/chaos/network-delay.yaml) or
[partition](../../examples/chaos/network-partition.yaml) example. Substitute the
current root and participant UIDs, inspect the selected Pods and server-dry-run
the resulting manifest. Replace the example's namespace and actor names too.
For actor-specific faults, prefer selectors containing
`network.stacks.org/network-uid`, `network.stacks.org/participant-uid` and
`network.stacks.org/role: actor`. Support workers and configuration Jobs may
share the first two labels, so a selector without the role also matches them.
These are targeting conventions, not admission constraints. An agent may
intentionally target workers, observers, other Pods or nodes when its experiment
calls for it. A worker fault can end managed operation
and leave submission uncertainty; an observer fault can create evidence gaps.
Faulting a node or a shared host can affect more than the selected network.

Label the **fault object** with `network.stacks.org/network-uid` for recorder
attribution in the network namespace. Add `actions.stacks.org/correlation-id`
as a search hint. Selector labels alone do not establish journal attribution. The
recorder watches the namespaced Chaos Mesh 2.8.4 fault and orchestration kinds
listed in [observability](../observability/README.md); a missing CRD or failed
watch is reported as a source gap. Kubernetes Events, native status and recorded
objects are observations, not proof that packets, CPU, disk or protocol behavior
changed as intended. Capture the selected Pod UIDs and measure before, during
and after the intervention. Replacements can match the same labels.

### Side effects

- `direction: to` on a delay can affect the selected actor's RPC traffic as
  well as peer traffic. An unselected peer can bridge a pairwise partition.
- An actor-wide partition can interrupt external metric scrapes and the in-Pod
  Bitcoin observer sidecar. The observer and Greptime run in
  `stacks-observation-system`; faulting that namespace can disrupt recording
  for every network.
- The recorder journals attributable fault objects in its enrolled network
  namespace. Fault objects created elsewhere are outside that network's journal.

For example:

```bash
# Set these from the current StacksNetwork and StacksNetworkParticipant objects.
export NETWORK_UID SOURCE_PARTICIPANT_UID TARGET_PARTICIPANT_UID
envsubst '${NETWORK_UID} ${SOURCE_PARTICIPANT_UID} ${TARGET_PARTICIPANT_UID}' \
  < examples/chaos/network-delay.yaml > /tmp/current-network-delay.yaml
kubectl --kubeconfig tools/local-cluster/kubeconfig --context kind-stacks-k8s \
  create --dry-run=server -f /tmp/current-network-delay.yaml
kubectl --kubeconfig tools/local-cluster/kubeconfig --context kind-stacks-k8s \
  create -f /tmp/current-network-delay.yaml
kubectl --kubeconfig tools/local-cluster/kubeconfig --context kind-stacks-k8s \
  -n chaos-live get networkchaos actor-delay -o yaml
```

The examples are scoped to one actor pair. Native Chaos Mesh supports other
fault kinds and selectors; their effects and cleanup behavior need separate
measurement. Existing [delay](qualification.md) and
[partition](partition-qualification.md) results were obtained with an earlier
restricted profile and legacy runtime. They do not qualify every native fault
kind or current actor workload.

## Recovery and cleanup

Delete faults and wait for finalized deletion before removing the namespace
annotation, controller or daemon. Duration expiry requests recovery, but it is
not a hard deadline when the upstream cleanup path is unavailable. Check actual
traffic or protocol recovery independently; restoring a network link does not
roll back chain changes. Preserve the fault UID, status, selected Pod identities
and measurements externally before deleting the network or namespace.

```bash
kubectl --kubeconfig tools/local-cluster/kubeconfig --context kind-stacks-k8s \
  -n chaos-live delete networkchaos actor-delay --wait=true --timeout=90s
```

For Bitcoin network teardown, follow the
[network cleanup guide](../network-operator/operations.md#removal-and-cleanup).

## Opt-in live checks

`tools/chart-policy/internal/integration` includes
`TestLiveCurrentActorSelectors`, which checks current v1alpha2 actor identities
with a native selector dry-run and does not inject a fault. The three injection
tests—`TestLiveBitcoinPartition`, `TestLiveStacksBitcoinPartition` and
`TestLiveProducerControlLoss`—use earlier-runtime fixtures. They do not qualify
fault behavior in the current public API runtime. Use a compatible disposable
fixture for each check. Set `STACKS_CHAOS_KUBECONFIG` and
`STACKS_CHAOS_CONTEXT`; selector dry-run also
needs `STACKS_CHAOS_IDENTITY_LIVE=1`, `STACKS_CHAOS_NAMESPACE`,
`STACKS_CHAOS_SOURCE` and `STACKS_CHAOS_TARGET`. The three injection tests need
`STACKS_CHAOS_PARTITION_LIVE=1`; their namespace overrides are
`STACKS_CHAOS_BITCOIN_NAMESPACE`, `STACKS_CHAOS_STACKS_NAMESPACE` and
`STACKS_CHAOS_CONTROL_NAMESPACE`. See the [delay](qualification.md) and
[partition](partition-qualification.md) records for the historical evidence.
