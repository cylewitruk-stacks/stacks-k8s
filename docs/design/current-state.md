# Current state and gap inventory

This inventory separates implemented functionality from historical Attacknet
features. Historical code is feature research only; its orchestration model is
not a migration target.

## Action deployment

The independent `stacks-action-operator` owns `BitcoinBlockGeneration` and
optional `BitcoinReorganization` status/finalizers. The network Bitcoin worker
reads actions and owns the reservation ledger and all mutation RPCs. See the
[action guide](../action-operator/operations.md).

## Implemented topology

The `network.stacks.org/v1alpha1` API provides:

| Resource | Implemented behavior | Important limit |
| --- | --- | --- |
| `StacksNetwork` | Compiles topology and optional Bitcoin/STX production; publishes declarations and admitted actor identity. | Does not bootstrap protocol state or issue mining RPCs itself. |
| `BitcoinNode` | Runs one Bitcoin Core instance with explicit peers. | Block generation requires production or action resources. |
| `StacksNode` | Runs one miner, follower, or signer-node bound to one Bitcoin node. | Protocol readiness is not independently established. |
| `StacksSigner` | Runs one signer bound to one signer-node with index and weight. | Registration and reward-cycle participation are external. |

The separate `bitcoin.stacks.org/v1alpha1` `BitcoinBlockProduction` API now
supports an aggregate-owned, single-target fixed-cadence baseline. Its
[implemented profile](../network-operator/bitcoin-production.md) defines static
credential separation, admission, durable dispatch accounting, and the
fail-closed availability limit after a lost receipt. Multi-target production remains unavailable.

The separate `stacks.stacks.org/v1alpha1` `StacksTransactionProduction` API
supports one exclusive account, fixed-interval tiny STX transfers, bounded
pending work, and exact native inclusion accounting. The
[initial Stacks profile](../network-operator/stacks-production.md) provides
external Bitcoin funding, PoX-4 enrollment and renewal helpers. A local normal
Stacks image is qualified; this is not a general image compatibility claim.

The aggregate accepts 1–32 Bitcoin nodes and up to 100 Stacks nodes and 100
signers. Multiple Bitcoin nodes and Stacks miners are structurally possible.
Current live qualification does not establish every multi-miner or
mixed-version combination.

Actor templates support per-actor images, raw or generated configuration,
container command overrides, storage, CPU and memory, placement, and
suspension. Aggregate-owned leaf resources must be changed through the parent.
Standalone leaf resources remain an advanced API.

The [steady-state operation design](steady-state-operation.md) separates actor
topology from baseline capabilities. Bitcoin nodes describe Core instances;
production resources select where and when blocks are generated.

The operator supports persistent volumes and retention, but refuses unsafe
in-place changes to immutable StatefulSet service identity, selectors, and
volume-claim templates. A replacement logical actor is required for those
changes.

The optional [`BitcoinBlockGeneration`](../network-operator/bitcoin-generation.md)
API shares the baseline executor with durable reservation, bounded immutable
requests, cancellation, and receipt attribution. The optional
[`BitcoinReorganization`](../network-operator/bitcoin-reorganization.md) profile
adds bounded suffix replacement and explicit compensation obligations.

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
| Bitcoin lifecycle | Multi-target/jitter policies and broader branch observation. Single-target baseline, finite generation, and constrained local reorganization are implemented. |
| Steady transaction demand | Multi-account/ingress profiles, overrides and wider signing lifecycle support. One-account fixed-interval demand is implemented. |
| Protocol bootstrap | General image/epoch compatibility, PoX-5 transitions and reusable bounded APIs. External PoX-4 bootstrap and renewal are implemented for one local profile. |
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
- Bitcoin topology and block production are separate concerns: neutral nodes
  host chain state; `BitcoinBlockProduction` declares timing and selection
  among referenced nodes. Stacks nodes retain miner configuration.
- Baseline production needs current target admission independent of unrelated
  actor health; whole-network readiness can itself depend on chain progress.
- Supported protocol faults must distinguish actor traffic from centralized
  production control access; arbitrary isolation has no continuity guarantee.
- Safe independent upgrades require persistent-data compatibility guidance and
  explicit identity transitions, not an in-cluster rollout plan.

## Open inventory questions

1. Which Stacks images and additional Bitcoin/platform combinations extend the
   initial Bitcoin Core 31.1 Linux/arm64 baseline qualification?
2. Which signer-registration and stacking actions belong in this repository
   rather than an external bootstrap tool?
3. Is sBTC in the first actor-expansion release or a later extension?
4. Which managed Kubernetes providers expose usable audit streams?
5. Which additional transaction profiles merit support? Initial Bitcoin and STX
   production use separate Deployments and ServiceAccounts in the network chart.
