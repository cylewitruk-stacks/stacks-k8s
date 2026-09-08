# Native network faults

The profile supports directed delay and bidirectional partition between two
logical actors using Chaos Mesh directly. It adds no action CRD or controller
to stacks-k8s. Other native kinds and mechanisms remain unqualified and ungranted.

## Install

Use a disposable Linux Kubernetes cluster. The qualified setup is kind with
containerd and kindnet; it requires network netem support and the privileged
Chaos Daemon. Keep the daemon's authority separate from all stacks-k8s operators.
The checked-in values disable the dashboard and DNS service and enable upstream
namespace filtering. Version and runtime assumptions are recorded in the
[qualification matrix](qualification.md).

For the repository's running `stacks-k8s` cluster, use:

```bash
make cluster-chaos-install
```

This verifies and installs the pinned chart with the values below using
`tools/local-cluster/kubeconfig` and context `kind-stacks-k8s`. It does not enroll
workload namespaces or install their fault profiles. Repeated calls reconcile
the same release to the repository's values. The [1.37 live qualification](../local-cluster-qualification.md)
records passing native-fault checks and an unresolved Stacks protocol-recovery failure.

For another explicitly selected cluster, download and verify the pinned upstream
chart before installation:

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
  label namespace chaos-live network.stacks.org/chaos-profile=network-faults-v1
kubectl --kubeconfig "$task_kubeconfig" --context "$task_context" \
  annotate namespace chaos-live chaos-mesh.org/inject=enabled
helm install profile charts/stacks-chaos-profile \
  --kubeconfig "$task_kubeconfig" --kube-context "$task_context" \
  --namespace chaos-live --set networkDelay.enabled=true \
  --set networkPartition.enabled=true \
  --set chaosMesh.externalVersion=2.8.4
```

The external-version value records an administrator prerequisite; it does not
install or discover Chaos Mesh. Only the profile chart is disabled by default.
See its [values and RBAC contract](../../charts/stacks-chaos-profile/README.md).
Issue short-lived credentials for `chaos-live/stacks-chaos-agent` through your
Kubernetes access mechanism. Do not distribute the administrator kubeconfig to
an experiment agent.

## Submit and observe

Submit either the [delay](../../examples/chaos/network-delay.yaml) or
[partition](../../examples/chaos/network-partition.yaml) example through the
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
`direction: both` partitions both directions between the same two selected Pods.
The [partition qualification](partition-qualification.md) also covers Bitcoin
peer chain separation and miner-to-Bitcoin disruption, cancellation/expiry and
an initial productive reconnection. A repeat Stacks case failed to regain
protocol progress after native cleanup; see the recorded recovery limit.
This is a kind/containerd/kindnet result, not a claim
about every Service-routing or CNI combination.

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

## Partition qualification fixtures

Reuse an explicitly selected qualification cluster with fresh namespaces and
credentials for independent environments. Reserve separate clusters for changes
to cluster components or faults that require cluster isolation. The opt-in
partition tests default to `partition-bitcoin` / network `chaos` (two Bitcoin actors,
weights `1,1`, fixed 3 s cadence) and `partition-stacks` / network `stacks` (the
[productive Stacks fixture](../network-operator/stacks-production.md), normal
node image, fixed 5 s Bitcoin cadence and 10 s transfer demand). Bootstrap and
confirm Stacks production before testing. Install the combined profile and
`network-faults-v1` enrollment in both namespaces. Neither suite needs the action
operator. The tests use administrator observer/proxy access for measurements;
that authority is not part of the agent Role. Set `STACKS_CHAOS_BITCOIN_NAMESPACE`
and `STACKS_CHAOS_STACKS_NAMESPACE` to qualify fresh namespaces without replacing
the preserved fixtures or changing the default kubeconfig context.

```bash
STACKS_CHAOS_PARTITION_LIVE=1 \
STACKS_CHAOS_KUBECONFIG="$task_kubeconfig" \
STACKS_CHAOS_CONTEXT="$task_context" \
GOWORK=off go -C tools/chart-policy test -tags=live \
  -run 'TestLive(BitcoinPartition|StacksBitcoinPartition)$' -count=1 -v ./internal/integration
```

The Bitcoin test pauses/resumes aggregate production to compare stable chain
tips and leaves it paused. The Stacks test requires a still-active signer lock,
requires a new confirmation after the test starts, leaves production running,
and does not claim immediate proposal stoppage during
a partition. Pause both desired production policies after collecting evidence,
or maintain the fixture's bounded signer enrollment while continuing work.

Both Bitcoin RPC directions are probed before, during and after injection.
These are round-trip reachability checks; they do not independently establish
packet-level blocking in each direction. Stacks tests log Core height and both
nodes' public PoX cycle IDs, lengths, boundaries, countdowns and activity before
injection, after injection, after native cleanup, after protocol recovery and on
wait timeout. The phase label is derived from each node's reported boundaries;
node heights can lag Core. Samples are sequential, not atomic. Missing telemetry
is logged as a gap and does not override the test result. No reward-cycle phases
are avoided to obtain a passing qualification.

The separate administrator-only control-loss test uses a fresh namespace named
by `STACKS_CHAOS_CONTROL_NAMESPACE` (default `partition-control`), network
`control`, one Bitcoin actor, and the existing `bitcoin-receipt-delay` fixture
image. Generate isolated credentials with the Bitcoin helper, set desired
production paused before creation, install the network chart with Bitcoin
production, and enable only the upstream namespace injection annotation.
Do **not** enroll this namespace in the public fault profile. The test selects
the producer directly, which public admission rejects. It first proves preflight
loss does not authorize a mutation, then interrupts a held real receipt and
normally deletes the producer Pod to exhaust its drain. Run it with the same
opt-in variables and `-run '^TestLiveProducerControlLoss$'`. It requires an unused
height-zero fixture and leaves the unresolved target closed. Each repeat needs
a new namespace and fresh credentials; restoring connectivity is not readmission.
The 15 s receipt hold belongs solely to the test image, not production behavior.

## Repeat qualification

The delay live test requires this exact disposable fixture and refuses implicit
kubeconfig/context selection or a namespace already containing native faults:

```bash
STACKS_CHAOS_LIVE=1 \
STACKS_CHAOS_KUBECONFIG="$task_kubeconfig" \
STACKS_CHAOS_CONTEXT="$task_context" \
GOWORK=off go -C tools/chart-policy test -tags=live \
  -run TestLiveNativeDelay -count=1 -v ./internal/integration
```

Set `STACKS_CHAOS_DELAY_NAMESPACE` to use a different namespace containing the
same `chaos` network and actors.

It reads the observer Secret as the qualification administrator, passes the
password to Bitcoin CLI through stdin, and never logs credential bytes.
Normal experiment access does not receive that Secret or Pod-exec permission.
The suite creates and deletes its bounded native faults and restarts the
isolated Chaos Mesh controller during the expiry case. It leaves actor workloads
and production policy unchanged.
