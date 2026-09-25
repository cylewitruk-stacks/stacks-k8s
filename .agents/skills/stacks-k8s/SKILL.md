---
name: stacks-k8s
description: Investigate a live stacks-k8s network with bounded workloads, native faults, telemetry, protocol snapshots, and portable evidence. Use for agent-led experiments and diagnosis, not ordinary repository edits.
---

# Investigate with stacks-k8s

The agent coordinates the experiment. `StacksNetwork` provides steady-state network
operation; no operator plans a scenario or promises a deterministic outcome. Confirm
cluster authority and experiment scope before mutation. Use a fresh
namespace and preserve each run's inputs, UIDs, receipts, observations and findings
outside tracked product source. Keep credentials in Kubernetes Secrets or process
stdin; strip `authorization` and other credential fields before saving tool requests.
Reuse the dedicated local cluster when appropriate.

1. **Establish identity and readiness.** Read the current
   [network operations](../../../docs/network-operator/operations.md) and
   [telemetry setup](../../../docs/observability/README.md). Capture the network,
   participant and Pod UIDs, runtime image/container IDs, operator rollout, and
   root conditions. `Initialized`, `Running` and `Operational` are controller
   reports; compare native protocol progress separately. Use
   [`stacks-preflight`](../../../tools/stacks-preflight/README.md) to check the
   selected recorder, sources, rollout, enrollment and recent backend rows.
   Its pass does not prove native fault admission or uninterrupted capture.
2. **Record a control.** Emit a scoped Kubernetes phase Event; take spaced
   [`stacks-inspect`](../../../tools/stacks-inspect/README.md) captures through
   Pod-specific endpoints. Save participant/Pod identities before and after
   each forward. A partial capture is evidence of an unavailable read, not an
   empty protocol fact. Choose external, experiment-specific outcome checks.
3. **Apply a measured intervention.** Follow the
   [native Chaos Mesh guide](../../../docs/chaos/operations.md) with the agent's
   granted cluster authority. With upstream namespace filtering, annotate the
   experiment namespace `chaos-mesh.org/inject=enabled` and keep it enrolled
   until fault recovery finishes. Start with the
   [partition example](../../../examples/chaos/network-partition.yaml), or use
   another native kind that tests the hypothesis. For actor-only faults, select
   the exact network and participant UIDs plus `network.stacks.org/role: actor`;
   support Pods share the UID labels. Target workers, collectors or
   nodes deliberately when needed, accounting for lost operation or evidence.
   A `direction: to` delay can also affect actor RPC; an unselected peer can
   bridge a pairwise partition. Actor-wide faults can interrupt metric scrapes
   and the in-Pod Bitcoin observer; faults against `stacks-observation-system`
   can disrupt recording for every network. Put the network UID on the *fault
   object's metadata labels* for journal attribution in the experiment namespace;
   `actions.stacks.org/correlation-id` is a search hint. Selector labels alone
   do not attribute a fault. Server-dry-run the manifest, observe native status,
   and measure actual actor behavior before, during and after. Use
   [`stacks-workload`](../../../tools/stacks-workload/README.md) for finite,
   seeded input variation and exact TxID receipts. Keep one nonce writer per
   funded sender; never retry an uncertain submission automatically.
4. **Compare and preserve.** Check control, fault and recovery against fresh
   protocol facts, object/log/metric rows, recorder heartbeat and source gaps.
   A fault condition proves controller action, not packet effect; a healthy
   collector does not prove protocol health. Export a settled, bounded window
   with [`stacks-evidence`](../../../tools/stacks-evidence/README.md), keep the
   manifest and workload receipts, and verify checksums after teardown. Read
   `coverage`, gaps, truncation and missing context before making claims.
5. **Clean up deliberately.** Recover/delete native faults, stop and delete the
   network root, remove its telemetry, then delete
   the experiment namespace. Check retained PVCs and unenroll deleted namespaces
   from any shared observer release. Leave a shared local cluster running.

Distinguish reproducible inputs from best-effort dispatch and uncontrolled
distributed behavior. Report observed effects, counterexamples and uncertainty;
do not claim deterministic replay, causal minimality or a vulnerability from one
run. See the [qualification evidence](../../../docs/observability/investigation-tools-qualification.md)
for demonstrated limits.
