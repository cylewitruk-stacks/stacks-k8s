# Native network delay

The first native profile delays traffic from one logical actor to another using
Chaos Mesh directly. It adds no action CRD or controller to stacks-k8s. Other
native kinds and network mechanisms remain unqualified and ungranted.

## Install

Use a disposable Linux Kubernetes cluster. The qualified setup is kind with
containerd and kindnet; it requires network netem support and the privileged
Chaos Daemon. Keep the daemon's authority separate from all stacks-k8s operators.
The checked-in values disable the dashboard and DNS service and enable upstream
namespace filtering. Version and runtime assumptions are recorded in the
[qualification matrix](qualification.md).

Download and verify the pinned upstream chart before installation:

```bash
helm pull chaos-mesh --repo https://charts.chaos-mesh.org \
  --version 2.8.4 --destination /tmp
printf '%s  %s\n' \
  ae4abd385649771300e4d33a44627c0df3618be0780c385bf30cc2fdf2ad93fa \
  /tmp/chaos-mesh-2.8.4.tgz | shasum -a 256 -c -

task_kubeconfig=/tmp/stacks-chaos-20260906.kubeconfig
task_context=kind-stacks-chaos-20260906
helm install chaos-mesh /tmp/chaos-mesh-2.8.4.tgz \
  --kubeconfig "$task_kubeconfig" --kube-context "$task_context" \
  --namespace chaos-mesh --create-namespace \
  --values examples/chaos/upstream-values.yaml --wait --timeout 5m
```

Create the two-target `chaos` Bitcoin network in `chaos-live` using the
[Bitcoin environment helper](../network-operator/bitcoin-production.md#start-a-disposable-environment).
Use `--namespace chaos-live --name chaos --target-weights 1,1 --interval-seconds 3`.
Install the network chart with Bitcoin production enabled and wait for both
actors and their target ledgers to advance. No Stacks transaction producer or
action operator is required for this narrow fixture.

Inspect and finish existing native faults before beginning a profile-qualified
experiment. Installation preserves cleanup of pre-existing nonconforming faults:
creation constraints do not block their status/metadata writes or finalized
deletion, but their specs become immutable too. Existing faults retain their
original behavior and count toward the quota; installation does not cancel them.

Enroll the namespace and install its profile:

```bash
kubectl --kubeconfig "$task_kubeconfig" --context "$task_context" \
  label namespace chaos-live network.stacks.org/chaos-profile=network-delay-v1
kubectl --kubeconfig "$task_kubeconfig" --context "$task_context" \
  annotate namespace chaos-live chaos-mesh.org/inject=enabled
helm install profile charts/stacks-chaos-profile \
  --kubeconfig "$task_kubeconfig" --kube-context "$task_context" \
  --namespace chaos-live --set networkDelay.enabled=true \
  --set chaosMesh.externalVersion=2.8.4
```

The external-version value records an administrator prerequisite; it does not
install or discover Chaos Mesh. Only the profile chart is disabled by default.
See its [values and RBAC contract](../../charts/stacks-chaos-profile/README.md).
Issue short-lived credentials for `chaos-live/stacks-chaos-agent` through your
Kubernetes access mechanism. Do not distribute the administrator kubeconfig to
an experiment agent.

## Submit and observe

Submit [the native example](../../examples/chaos/network-delay.yaml) through the
agent's Kubernetes identity. Both selectors use logical actor labels; the
qualified example has source `bitcoin` and destination `bitcoin-2`.

```bash
kubectl --kubeconfig "$task_kubeconfig" --context "$task_context" \
  create -f examples/chaos/network-delay.yaml
kubectl --kubeconfig "$task_kubeconfig" --context "$task_context" \
  -n chaos-live get networkchaos actor-delay -o yaml
kubectl --kubeconfig "$task_kubeconfig" --context "$task_context" \
  -n chaos-live get bitcoinproductiontargets
```

These commands use the explicit qualification kubeconfig; production agent
access should use its restricted credentials. Required correlation labels are
search hints; native resource UID is the fault identity.

`direction: to` delays actor-to-actor traffic, including their RPC traffic; it
is not a port-specific P2P filter. The producer is an unselected source Pod, so
its Bitcoin RPC path remains outside this fault. Live qualification measures
slower actor RPCs and continued destination production receipts during injection.
It does not establish every Stacks-to-Bitcoin, partition, Service-routing, or
CNI combination.

Inspect native `Selected`, `AllInjected`, and `AllRecovered` conditions and
container records alongside actual before/during/after measurements. Upstream
records identify selected namespace/Pod names; capture current Pod UIDs and
runtime identity separately. Static admission validates selector shape, not
live inventory membership. Empty selectors may match no Pods; replacement Pods
may receive the fault under the same labels. Neither successful admission nor
native conditions alone establish the intended protocol effect.

The passive journal, automatic UID-divergence correlation, and admission-gap
capture described in M0.7 remain design work. Preserve native objects/events,
Pod identity snapshots and measurements externally for now; do not claim a
complete observation history from these tests.

## Recovery and removal

Deletion requests native cleanup; duration expiry also triggers native recovery.
No chain rollback is implied by restoring network traffic.

```bash
kubectl --kubeconfig "$task_kubeconfig" --context "$task_context" \
  -n chaos-live delete networkchaos actor-delay --wait=true --timeout=90s
```

Verify traffic recovery as well as finalized deletion. An expired object still
consumes the one-object quota. Do not force-remove native finalizers or uninstall
the upstream controller/daemon while faults remain: cleanup requires their
control path. A controller outage can extend the requested duration. Node loss,
daemon loss, and unreachable runtime cleanup are not qualified by this profile.

Remove the stacks-k8s enrollment label to stop new admissions, but retain the
upstream `chaos-mesh.org/inject=enabled` annotation until recovery completes.
Admission permits existing cleanup writes; it cannot make an upstream controller
process a namespace excluded by its own filter.

For complete removal, delete all native faults, confirm recovery, uninstall the
`profile` release, and then uninstall `chaos-mesh`. Network and action operators
have no Chaos Mesh dependency. Follow the
[Bitcoin teardown](../network-operator/bitcoin-production.md#dispatch-state-and-recovery) for the
fixture, including retained target ledgers; deleting this task's entire kind
cluster is also valid for its disposable environment.

## Repeat qualification

The live test requires this exact disposable fixture and refuses implicit
kubeconfig/context selection or a namespace already containing native faults:

```bash
STACKS_CHAOS_LIVE=1 \
STACKS_CHAOS_KUBECONFIG="$task_kubeconfig" \
STACKS_CHAOS_CONTEXT="$task_context" \
GOWORK=off go -C tools/chart-policy test -tags=live \
  -run TestLiveNativeDelay -count=1 -v ./internal/integration
```

It reads the observer Secret as the qualification administrator, passes the
password to Bitcoin CLI through stdin, and never logs credential bytes.
Normal experiment access does not receive that Secret or Pod-exec permission.
The suite creates and deletes its bounded native faults and restarts the
isolated Chaos Mesh controller during the expiry case. It leaves actor workloads
and production policy unchanged.
