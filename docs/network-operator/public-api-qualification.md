# Composable runtime qualification

This record concerns the replacement `v1alpha2` runtime on the three-node
`stacks-k8s` kind cluster (Kubernetes 1.37.0, macOS/arm64, Docker Desktop).
The [live harness](../../operators/network/internal/publicintegration/README.md)
uses fresh namespaces and public declarations. Legacy runtime results do not qualify
this implementation.

## Profile

| Input | Pinned value |
| --- | --- |
| Bitcoin Core | `bitcoin/bitcoin:31.1` |
| Stacks node/signer | Core revision `f9b022bff5550d1e9938e1f40805ea354381a59e` |
| Independent upgrade image | Core revision `62e03cc5551bfc574223c2b78ce04ceca30cec37` |
| Contract set | Five unmodified sBTC contracts, revision `d31b0780cf1ae91b6ef4ed7ee89400c796aea913`, Clarity 3 |
| Protocol | Frozen `regtest-pox4-pox5-v1` gates; real sBTC deployment, explicit test registry initialization, direct PoX-5 managers |
| Reference cadence | Fixed 5 seconds; bounded scheduling/action overrides recorded separately |
| Full cohort | 10 Bitcoin nodes, 14 Stacks nodes (including 4 miners), 6 consensus signers, 6 stackers and 4 other capability participants |
| Native faults | Chaos Mesh 2.8.4; bounded actor-pair delay and partition profile |

The Stacks build enables `monitoring_prom,slog_json` with `release-lite`; the
primary diagnostic image adds curl without changing the Core binary. The alternate
image is a distinct source revision, not a retag of that image. Build logs, image
IDs, namespace/root/genesis identities and controller snapshots are recorded in the
iteration review ledger.

## Evidence boundaries

Initialization requires actual legacy enrollment, prepared signer sets, a canonical
Nakamoto tip, deployed contracts and registry state, PoX-5 participation and fresh
post-waterfall traffic. Readiness or height alone does not pass those gates.
Renewal observes continuous native lock coverage and subsequent protocol progress;
shared-account observations do not establish exclusive transaction authorship.

Bitcoin action exclusion is per target. The action test selects one producing target
and lets other outstanding requests settle before capturing a reorganization tip.
Confirmed external movement stops the action; compensation removes the captured
invalidity marker rather than restoring the old best chain.

Envtest validates API/SSA/lifecycle behavior with modeled termination, not a kubelet
or garbage collector. Native lifecycle evidence must separately demonstrate Pod,
StatefulSet, PVC and root deletion. Operator restart preserves worker processes;
bound Stacks-worker eviction intentionally fails the experiment without recovery.

Fault cleanup requires native completion and finalized deletion. Protocol recovery
additionally requires new canonical Stacks advancement and transaction inclusion.
Pairwise HTTP probes establish round-trip reachability, not independent one-way
packet behavior or deterministic chain divergence.

## Qualification outcomes

### Full-cohort lifecycle and image roll — 2026-09-13

`m3-lifecycle-20260913a` passed one combined run on source
`cc629ebe573492bc20ab859b0987e925fac05830`, using a freshly built
`stacks-network-operator:m3-20260913`. The full30 fixture used the reference
five-second cadence throughout. The harness additionally applied the existing
fresh-storage and canonical-tip predicates to both newly added followers.

| Check | Observed result |
| --- | --- |
| Empty-storage bootstrap | All six gates completed; `Initialized`, `Running` and `Operational` were true. Bitcoin execution records reported height 301; Stacks observers reported burn heights 300–301. |
| Operator restart | Deployment rolled; actor and bound worker process identities were preserved. |
| Fresh follower | New uncloned PVC/PV; native PoX-5 canonical agreement and further same-process advancement. |
| Independent image roll | `iteration2-f9b022-probe` → `iteration2-4.0.1`; same participant/PVC/PV, different Pod and runtime image, Stacks height 129 → 137. |
| Suspend/resume | Exact process termination acknowledged; new Pod resumed with the same participant, configuration and storage. |
| Removal and replacement | First runtime deleted with its PVC retained; second participant used distinct new storage and passed canonical catch-up. Second runtime and its non-retained claim were deleted. |
| Cohort isolation | Frozen genesis and unrelated actor/worker process identities remained unchanged during actor operations; subsequent native progress passed. |
| Continuing maintenance | All six stackers extended native lock coverage from end-cycle-exclusive 21 to 25 without replacing their workers; subsequent progress passed. |
| Faucet | Two included transfers and bounded no-resubmission observation after request deletion. |
| Shared Bitcoin definitions | Two added actors reused a definition and wallet with distinct runtime/storage identities; removal retained the reusable inputs and baseline progress. |
| Network pause/resume | Pause acknowledged with stable Bitcoin receipt counters; surviving workers resumed fresh protocol progress. |
| Teardown | Normal Stop, root deletion, reusable-declaration identity checks and namespace deletion passed; no qualification PV remained. |

The run took 2164 seconds and ended at **12:52:38 UTC** with exit 0. No runtime
fix or administrative finalizer removal was needed. The shared three-node cluster
remains running with the tested operator installed and no network fixtures.

Root UID: `1b77793d-73f4-492e-9412-725c9473ea6e`; genesis UID:
`988eb471-ed02-40ee-8977-2983f078c76a`. The local image artifacts were:

| Artifact | Docker image ID |
| --- | --- |
| Network operator | `sha256:0358a531f9236a99764ae588f18dd61b54786a1b91c5a75f790ad55da0386af8` |
| Baseline Stacks | `sha256:8f82a5eae354e3d6fc6a1a8da866f91c516ca2597e8e1c3310f46307765219b4` |
| Alternate Stacks | `sha256:d70390356696a3bd2a90d79fec4386b5f20b419090edc2c6c4fe29595223a4af` |

Both Stacks binaries report the revisions in the profile table with a trailing `+`;
this qualification binds those local artifacts and does not assert pristine upstream
builds. Runtime-reported image IDs are recorded separately from Docker image IDs.
Their recorded correspondence uses the repository tags in the all-node inventory
`/tmp/stacks-m3-images.json`, not equality between the two sets of digests.
Only the added follower was rolled to the alternate revision; this is not a miner,
consensus-signer, reverse-upgrade or arbitrary-version compatibility claim.
The image-roll height comparison uses the initial join observation (129), not an
immediate pre-roll sample. The new Pod reported height 137; this check does not
measure advancement between two observations of that new process.

Local evidence: `/tmp/stacks-m3-lifecycle-a/` contains stage snapshots and
`events.jsonl`; `/tmp/stacks-m3-live-a.log` records the pass and exit marker;
`/tmp/stacks-m3-images.json` records all-node image inventory. Build/load/install
logs use `/tmp/stacks-m3-{build,load,install}.log`. Full `make verify` passed in
`/tmp/stacks-m3-verify.log` before the result documentation was written. Final
documentation checks are recorded in `/tmp/stacks-m3-docs-checks.log`: Markdown lint,
relative file links and a separate check of the new section anchor. The repository
link test checks file paths only, not fragments. These local files are not a
published evidence archive.

This single successful sequence does not diagnose earlier intermittent bootstrap
or missing-anchor failures. It exercises early follower joins after initialization,
not catch-up after reorganization/fault experiments, repeated-run reliability,
slow enrollment at the 294 ceiling, or an additional Chaos/action qualification.

### Earlier runs

| Run | Automated results | Boundary |
| --- | --- | --- |
| `iteration2-full-20260911` | Full cohort initialization, six renewals, faucet, schedules and generation modes | Reorganization expired without sending; competing producer moved the captured tip. Cleanup then required explicit administrative disposal after node-level termination inspection. |
| `iteration2-full2-20260912` | Full cohort initialization, six renewals, faucet, schedules, four generation modes, reorganization, normal Stop/root/namespace deletion | Historical late-follower catch-up timed out; remaining actor/fault/repeat checks did not run. |
| `iteration2-final-20260912` | Initialization, early join, image roll, suspension/resume and distinct replacement | Final progress gate rejected stale `Initialized.observedGeneration`; native traffic progressed. Normal Stop/root/namespace disposal passed. |
| `iteration2-final4-20260912` | Initialization and early actor operations; current-generation completion status confirmed | Harness reused a briefly unavailable signer-node observation from operator restart as its final progress baseline. Normal cleanup passed. |
| `iteration2-final5-20260912` | Deliberately cancelled during preparation; normal disposal | No protocol qualification claim. |
| `iteration2-final6-20260912` | Initialization and early actor operations | Traffic status stopped refreshing with an accepted transaction pending. Cause unproven; diagnostic termination is not normal worker-cleanup evidence. |
| `iteration2-final7-20260912` | Initialization, operator restart, late join, image roll, suspension/resume and removal | Replacement follower stalled on a missing cycle-13 anchor; normal Stop/root/namespace disposal passed. |
| `iteration2-final8-20260912` | Initialization, native progress and operator restart | Harness required an immediately settled renewal cohort; one stacker reported unavailable observations. Replaced with a bounded prerequisite wait; normal disposal passed. |
| `iteration2-final9-20260912` | Renewal, faucet, timing, all finite actions and shared Bitcoin actor isolation/removal | Subsequent progress failed: first canonical recheck failure produced an invalid traffic timestamp, stranding publication. Normal disposal passed. |
| `iteration2-final10-20260912` | Failed during native delay control check | Initialization, renewal, faucet, timing, actions and shared-definition disposal/progress passed. Delay injected and increased peer latency; unavailable canonical evidence aborted the next check. Normal disposal passed. |
| `iteration2-final11-20260912` | Stopped at height 250 after an accounting stall | A retained generation receipt outlived its selected opportunity and was never accounted. No Chaos injected; normal Stop/root/namespace disposal passed. |
| `iteration2-final12-20260912` | Cancelled during preparation | No protocol qualification claim; normal disposal to include the replacement-producer accounting correction. |
| `iteration2-final13-20260912` | Passed: two initialized incarnations, operator restart, delay/partition recovery, pause/resume, fresh runtime/storage, terminal eviction and normal disposal | Full30/fixed5 remaining-capabilities profile; optional renewal, faucet, action and actor exercises were not rerun. |

The correction run `iteration2-followup1-20260912` used the full30/fixed-5s profile
and image `iteration2-followup1`. Its first incarnation passed initialization,
operator restart, native fee-refusal recovery in the same process, faucet execution,
delay/partition cleanup and canonical recovery, pause/resume and direct foreground
root deletion with reusable declarations retained. The repeated incarnation failed
`PreparePoX5` with `BootstrapObservationDeadline`; subsequent fresh-runtime comparison
and deliberate eviction assertions did not execute. Normal Stop, background root
deletion and namespace cleanup completed without
administrative intervention. This run is not an overall pass.

The read-only observer separately returned `Ready/IdentityVerified` for all 30 actors
in full2. Its native identity reads were executed; this is not a telemetry-retention
qualification.

### Remaining-profile fault evidence

Final13 used the same full30/fixed-5s profile and network image `iteration2-final8`.
Both faults targeted `follower-01` and `follower-02`. Delay median round-trip latency
was 6.419 ms before injection, 1508.222 ms during injection and 8.284 ms after expiry.
Partition probes were unreachable in both directions and reachable after cancellation.

| Fault | Bitcoin heights during the injected control hold | Post-cleanup traffic inclusion count | Cleanup |
| --- | --- | --- | --- |
| Delay | 306–307 → 312 | 60 → 61 | Native duration expiry |
| Partition | 325 → 330–331 | 64 → 66 | Explicit cancellation |

The harness also required unchanged worker identities, current control observations and
new canonical Stacks progress. Temporary unavailable observations were recorded and
resampled within the existing deadlines; the result establishes sampled control success,
not uninterrupted availability. Stage snapshots and round-trip events are in
`/tmp/stacks-iteration2-final13-evidence/`. The second-network, final-eviction and
normal-disposal checks also passed.
This run omits actor, renewal, faucet and action exercises recorded separately above.

### Reusable declarations and fresh runtime

Final13 deleted the first Running root directly, retained the reusable declarations,
and created a second root without reapplying them. Both networks reached Initialized
and Operational with subsequent canonical progress. They shared the same chain digest
but had distinct root/genesis UIDs, all 40 participant UIDs, 49 Pod UIDs, 30 PVC UIDs
and 30 backing volumes. The comparison rejected any runtime or storage reuse.

### Bound-worker eviction

After the repeated network produced new canonical traffic, final13 evicted its exact
bound transaction-worker Pod through `policy/v1` with a UID precondition. The root
latched Failed, retained the original binding and confirmed process termination.
The subsequent 22-second observation window showed no replacement Pod, process
restart or change to the settled execution report. This qualifies terminal failure
reporting, not management-worker recovery. Normal Stop, root deletion and namespace
deletion then completed without administrative intervention; the shared cluster
remained running.

### Historical catch-up failure

Full2's new follower retained 14 inbound and 14 outbound peers and downloaded burn
headers through 478, but its native sortition processing stopped at burn height 379
and Stacks height 208. Core reported a missing canonical PoX anchor for cycle 19
(height 380). The existing cohort continued advancing. The failing follower's last
Stacks block matches the earlier fixed-schedule snapshot; the subsequent weighted
snapshot had advanced beyond the boundary.

This is evidence of a historical catch-up failure on the tested sequence, not proof
of its cause. Peer connectivity was present; configuration/protocol interactions
and the significance of earlier scheduling/reorganization remain unisolated. The
fixture was disposed normally after bounded logs, public neighbors and status were
captured. No Core patch or recovery is claimed.

Final7 exercised the same actor assertions before bounded actions. Its first late
follower caught up, but the distinct replacement stopped at burn260/Stacks50 with a
missing cycle-13 anchor. The original cohort and traffic remained healthy. Bounded
actions are therefore not necessary for this missing-anchor symptom; the mechanism
and conditions producing it remain unisolated.

The original after-actions ordering remains available. A remaining-capabilities run
explicitly omits actor lifecycle checks; its outcomes cannot establish reliable late
catch-up or success of either failed combined sequence.

### Initial-cohort cycle-13 stall

In followup1's repeated incarnation, Bitcoin reached the frozen ceiling of 281 at
17:56:59 UTC on 2026-09-12. The sBTC and traffic workers kept publishing fresh status,
but each retained its fourth accepted transaction after three exact inclusions;
the last inclusion was observed at 17:55:01. At 17:59:01 the root failed the gate deadline.
Miner-04 repeatedly reported a missing canonical PoX anchor for cycle 13 while
processing burn block 261. This actor belonged to the initial cohort; no fault or
bounded action was introduced into that incarnation.

The symptom resembles the catch-up failures above, but it is not confined to late
actor creation. The shared genesis digest matched the first incarnation. Cause and
operator/configuration contribution remain unisolated; no Core fix is claimed.
Bounded miner/signer/worker logs and public status are retained under
`/tmp/stacks-iteration2-followup1-repeat-diagnostics/`. This observation does not
invalidate the first incarnation's measured stages or establish reliable repetition.

### Accelerated prepared-set failure

`iteration2-followup2-20260912` used the final corrected image `iteration2-followup2`
with `minimal14` and a requested 1-second cadence. Both holders had fresh exact PoX-4
enrollment observations, but no prepared signer-set observation appeared. Native
signer logs reported `PoXAnchorBlockRequired` for cycle 12. Bitcoin reached the frozen
ceiling 251 at 18:17:02 UTC; the gate failed at 18:19:03. The run exited 1 after
743 seconds; normal Stop, background root deletion and namespace cleanup completed.

This does not qualify accelerated initialization. It is not evidence of a false
cached gate decision: the required native prepared-set evidence was absent. Timing,
configuration and protocol causes remain unisolated. Last-active public snapshots
and bounded logs are in `/tmp/stacks-iteration2-followup2-evidence/` and
`/tmp/stacks-iteration2-followup2-diagnostics/`.

### Reference-cadence PoX-5 enrollment failure

`iteration2-followup3-20260912` used the same final image, `minimal14` and fixed
5-second cadence. It completed Bitcoin preparation, PoX-4 enrollment, Nakamoto
preparation and the sBTC contract gate. At ceiling 284, both stackers reported
`PoXObservationUnavailable` with an accepted transaction pending. Traffic also
retained an accepted transaction; its last inclusion was observed at 18:46:01 UTC.
`EnrollPoX5` failed with `BootstrapObservationDeadline` at 18:48:27.

The bounded miner log contains repeated `MaxFeeRateExceeded` block-commit warnings
while Bitcoin was held at the ceiling. These symptoms do not establish the cause;
no fault or action had been injected. The rejection, faucet, operator-restart,
pause/resume and eviction checks scheduled after initialization did not execute.
Public snapshots and bounded logs are under `/tmp/stacks-iteration2-followup3-evidence/`
and `/tmp/stacks-iteration2-followup3-diagnostics/`. That correction pass ended
without a successful end-to-end native run on its
final image. Earlier measured stages
remain evidence for their recorded image and profile only. The run exited 1 after
1699 seconds; normal Stop, background root deletion and namespace cleanup completed
at 18:50:10 UTC without administrative intervention.

### Bootstrap convergence investigation

The unchanged `iteration2-followup2` image subsequently initialized the same
`minimal14`/fixed-5-second profile without intervention in
`bootstrap-diagnosis-20260912`. It passed native progress and pause/resume; normal
cleanup completed at 19:46:18 UTC on 2026-09-12 (exit 0, 1690 seconds). Captured
PoX-5 RPC/source were available at Bitcoin height 283 and Stacks height 91. The
prior enrollment failure was not reproduced; its exact cause remains unisolated.

Source inspection identified an avoidable premature hold: epoch activation plus
two Bitcoin blocks does not establish that a Stacks tenure has instantiated the
new epoch. Newly compiled genesis now permits normal confirmation opportunities
through height 294, immediately before the first PoX-5 enrollment cutoff at 295.
Existing frozen artifacts retain their recorded ceilings. Tests exercise both
limits with unavailable protocol observations, exact receipt accounting and no
funding replay. This correction does not establish the cause of the earlier run.

The corrected image `stacks-network-operator:bootstrap-convergence1`
(`sha256:134f688dd809e15b89f33cea709623f71db773707ce6e986bd34fa5ecc960fc2`)
was tested in `bootstrap-convergence-20260912` on the shared three-node kind cluster.
It used `minimal14`, fixed 5-second Bitcoin cadence, the pinned Core/sBTC artifacts
above and only the focused fresh-follower exercise. No manual initialization,
extra block generation or protocol intervention was used.

| Stage | Observed result on 2026-09-12 UTC |
| --- | --- |
| Initialization | 20:17:08: `Initialized`, `Running` and `Operational` True; cohort Bitcoin 303 / Stacks 108, PoX-5 |
| Fresh follower | New bound PVC/PV identities, no clone/snapshot source; first synchronization at 20:17:41 |
| Continued canonical progress | 20:17:53: follower and all cohort nodes at Bitcoin 312 / Stacks 119 with identical index block ID |
| Pause/resume | Cooperative pause held; fresh progress after resume at 20:18:32 |
| Disposal | Stop acknowledged 20:19:34; root deletion retained reusable declarations; namespace removed 20:19:51 |

Enrollment completed around Bitcoin height 285, before the new ceiling was reached.
This run did not exercise a hold at 294, its 120-second deadline expiry, or native
enrollment at height 294. The extended-window boundary has source and unit-test
evidence only; no additional live boundary qualification is claimed.

The follower was added after initialization, without a restored chain snapshot.
Its participant/Pod/container identities remained unchanged after first successful
synchronization; the frozen genesis and unrelated actor processes were unchanged.
This proves one fresh-storage catch-up on this local profile, not uninterrupted
startup, reliable repetition, arbitrary storage-provider behavior or all previous
failed sequences. The small topology is a qualification fixture, not a product mode
or an external bootstrap prerequisite. The correction's full isolated-snapshot
`make verify` and generated-drift checks passed. Evidence is under
`/tmp/stacks-bootstrap-convergence-evidence/` and
`/tmp/stacks-bootstrap-convergence-rpc/`; the live log is
`/tmp/stacks-bootstrap-convergence-live.log` (exit 0, 1741 seconds). The shared
cluster remains running; both investigation namespaces were removed.

### Bootstrap receipt accounting

Final11 exposed an accounting race: an expired selection was replaced before its
retained receipt was accounted. The worker withheld new generation until the cursor
advanced. Accounting now consumes the exact retained request independently of current
selection or producer, with once-only funding and cursor publication. Tests cover
replacement/cleared selections, producer replacement, write loss, native-worker
continuation and API-server status ordering.

### Traffic observation stall

Final6's traffic worker remained Running while its execution status stopped refreshing
with one accepted transaction pending. Other actors and Bitcoin continued advancing.
No stack trace was retained. Direct Kubernetes requests now have a 10s timeout and
worker reconciliation errors are logged at a bounded rate; informer watches use a
separate client. These changes address I/O and diagnostic gaps, not a proven cause
of the stall.

Final9 then exposed a specific failure with logs: after inclusion, an unsuccessful
first canonical recheck left a zero timestamp that serialized as null. Schema rejection
stranded the report. Traffic now omits unobserved canonical evidence while retaining
inclusion receipts, and publishes it after a successful read. Step/drain unit tests
and API-server SSA regressions cover failed first reads and recovery without replay.
This explains final9; attribution to final6 remains unproven.

### Fault-control observation boundary

Control qualification requires fresh Bitcoin receipts, unchanged management sessions,
API heartbeats and coherent Stacks observations while the native fault is injected.
An unavailable canonical observation can reflect either RPC failure or a changing
chain tip; it does not establish transport loss alone. The harness records such gaps
and resamples within the existing bounded stage. Persistent unavailability or an
identity mismatch fails the stage. Successful samples and receipt advancement across
the hold window do not claim uninterrupted RPC availability. Native fault cleanup
must still be followed by newly observed canonical progress.

## Official 4.0.3 image qualification — 2026-09-15

The `minimal14` public lifecycle ran in `followup-final-20260915` with the
official `ghcr.io/stacks-network/stacks-core:4.0.3` and
`ghcr.io/stacks-network/stacks-signer:4.0.3` images. The three node Pods reported
image digest `sha256:9c45dbf25dbe6a2b5051657f914b98abccfbec1a9f84d1df45233d627446227d`;
the two signer Pods reported
`sha256:6cb6f31a6fdaf6e002816ec75691d7d44b654046fc433b253591f239742e1d58`.
All five became ready without restarts.

The run used an explicit five-second qualification cadence, not the product's
60-second default. Network UID `3aeea8a6-4adf-4454-84fa-65f76aa1a854` reached
`Initialized=True` and `Operational=True`, then produced fresh native progress.
Pause held, resume produced new progress, stop completed, reusable declarations
survived root deletion and the namespace was removed. The lifecycle passed in
1,724 seconds. Evidence is under `/tmp/stacks-followup-qualified-20260915/`.

An attempted one-second acceleration did not generate blocks: initialization
offers were replaced before the target control loop admitted them. That failed
setup does not qualify one-second cadence support. The later
[fast-bootstrap qualification](bitcoin-observer-qualification.md#fast-bootstrap-and-stable-diagnostics--2026-09-15)
records the correction and its validation.

## Limitations

This is a local profile qualification, not a compatibility matrix for arbitrary
images, storage drivers, managed clusters or custom epoch schedules. It does not
qualify management-worker crash recovery, real sBTC signer orchestration, arbitrary
network faults, or account coordination between independent writers sharing a key.
Observability is optional; its identity snapshot is not a complete forensic archive.
