# Current state and gap inventory

This inventory separates implemented functionality from historical Attacknet
features. Historical code is feature research only; its orchestration model is
not a migration target.

## Implemented topology

The `network.stacks.org/v1alpha1` API provides:

| Resource | Implemented behavior | Important limit |
| --- | --- | --- |
| `StacksNetwork` | Compiles an aggregate topology and publishes admitted actor identity. | Does not bootstrap protocol state or produce Bitcoin blocks. |
| `BitcoinNode` | Runs one Bitcoin Core miner or follower with explicit peers. | The miner role does not invoke mining RPCs. |
| `StacksNode` | Runs one miner, follower, or signer-node bound to one Bitcoin node. | Protocol readiness is not independently established. |
| `StacksSigner` | Runs one signer bound to one signer-node with index and weight. | Registration and reward-cycle participation are external. |

The aggregate accepts 1–32 Bitcoin nodes and up to 100 Stacks nodes and 100
signers. Multiple Bitcoin and Stacks miners are therefore structurally
possible. Current live qualification does not establish every multi-miner or
mixed-version combination.

Actor templates support per-actor images, raw or generated configuration,
container command overrides, storage, CPU and memory, placement, and
suspension. Aggregate-owned leaf resources must be changed through the parent.
Standalone leaf resources remain an advanced API.

The operator supports persistent volumes and retention, but refuses unsafe
in-place changes to immutable StatefulSet service identity, selectors, and
volume-claim templates. A replacement logical actor is required for those
changes.

## Implemented observation

`observation.stacks.org/v1alpha1` provides `NetworkObservation`, a one-shot,
read-only verification of:

- topology UID, observed generation, and admitted inventory digest;
- leaf ownership and specification identity;
- Service and StatefulSet identity;
- Pod UID, readiness, controller revision, and immutable runtime image ID; and
- configuration declaration or content identity published by the network
  operator.

It does not collect protocol state, logs, metrics, audit events, resource
usage, or rolling history. It does not export evidence.

## Existing live-update behavior

Patching `StacksNetwork` can add or remove actors; alter images, configuration,
peers, resources, placement, or process overrides; and suspend individual
actors or the complete network. The admitted inventory is withdrawn while the
new generation converges.

Changing a generated or inline configuration rolls the affected actor. A
referenced ConfigMap or Secret changes only when its reference or optional
expected digest changes; the controller does not read Secret bytes.

Changing an actor image consumes an existing OCI artifact. No operator clones
Git, runs repository build logic, or constructs an image.

## Functional gaps

| Area | Missing capability |
| --- | --- |
| Bitcoin lifecycle | Bootstrap mining, continuous cadence, bounded/flash block generation, controlled reorganization, and RPC-observed peer/branch state. |
| Protocol bootstrap | Signer registration, stacking/reward-set activation, wallet funding, and explicit readiness beyond Kubernetes health. |
| Generic faults | Native Chaos Mesh installation guidance, safe direct targeting, correlation, and observation. |
| Protocol actions | Signer/miner behavior controls, application clock offset, bounded input behaviors, and portable storage pressure. |
| Continuous observation | Mutation history, protocol telemetry, logs, metrics, resource telemetry, rolling retention, and capture-gap reporting. |
| Agent access | Machine-readable capability discovery, efficient query API, and consistent cross-resource status vocabulary. |
| Evidence | Passive time-range export with completeness and source metadata. |
| Safety | Per-resource administrative bounds, direct-leaf edit RBAC, action identity pinning, and explicit cross-kind aggregate limitations. |
| Packaging | Optional Chaos Mesh and telemetry dependencies, published images/charts, SBOMs, signing, and a support matrix. |
| Actor coverage | sBTC signers and other materially distinct executables. |

## Historical features worth preserving semantically

The historical Attacknet demonstrated useful behavior that should be recast as
small resources or passive observations:

- Pod/container failure, network, DNS, I/O, time, and stress faults;
- application-level clock offset and portable disk pressure;
- Bitcoin mining pause, flash blocks, reorganizations, multiple Bitcoin views,
  and follower binding;
- bounded signer behaviors such as withholding, delay, and peer-response
  suppression;
- mixed actor images and configuration escape hatches;
- actor and topology identity checks before mutation;
- explicit effect, recovery, and capture-gap facts; and
- centralized log and telemetry export.

The following historical structures must not return:

- `AttacknetRun`-style scenario schedules;
- multi-stage `FaultCampaign` execution plans;
- in-cluster replay, fuzz planning, minimization, or diagnosis;
- outcome claims based only on a mutation object's successful lifecycle; or
- deterministic distributed-execution claims.

## Requirements newly identified

- Authoritative mutation history requires Kubernetes audit input when the
  cluster exposes it. Informer watches alone cannot guarantee every
  intermediate write.
- High-volume telemetry and evidence must not be stored in Kubernetes CRDs.
- A forced reorganization is an action even though natural reorganizations are
  normal Bitcoin behavior.
- Multi-miner topology and mining control are separate concerns: topology
  declares miners; mining policy decides when each mines.
- Safe independent upgrades require persistent-data compatibility guidance and
  explicit identity transitions, not an in-cluster rollout plan.

## Open inventory questions

1. Which real Bitcoin Core and Stacks image versions form the initial support
   matrix?
2. Which signer-registration and stacking actions belong in this repository
   rather than an external bootstrap tool?
3. Is sBTC in the first actor-expansion release or a later extension?
4. Which managed Kubernetes providers expose usable audit streams?
5. Does the first action operator include all protocol actions or only Bitcoin
   lifecycle controllers?
