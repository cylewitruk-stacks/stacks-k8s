# Native partition qualification

These are historical legacy-runtime results. The current profile additionally
requires exact network and participant UIDs and `role=actor` on both selectors.
The legacy fixture is incompatible with that profile and does not qualify the
replacement runtime. See the [current selector checks](../../charts/stacks-chaos-profile/README.md#replacement-actor-identities).

Tested 2026-09-07 on the fresh `kind-stacks-partition-20260907` cluster.
Native injection/cleanup and Bitcoin recovery passed. Stacks recovery passed an
initial run but failed a later reward-cycle transition; that limit remains open.
This extends the [delay matrix](qualification.md); M0.5 remains in progress.

## Inputs and boundary

The runtime is kind `kindest/node:v1.36.1`, arm64 Debian 13 / LinuxKit
`7.0.12-linuxkit`, containerd `2.3.1` and kindnet,
with external Chaos Mesh 2.8.4 using the same pinned chart/CRD hashes and
namespace-filtered values as the delay matrix. Profile chart 0.2.0 enables both
modes with `network-faults-v1` enrollment, one shared quota, and current-generation
CEL compilation without warnings. Native partitions use `mode: one` on both
sides, `direction: both`, exact actor labels, and no port filtering.

The network image `stacks-network-operator:partition-20260907` was built from
base `6c5632767005b741d95a7ba1638fa9422ec14eb3` plus this delivery; controller
runtime source is unchanged. The transaction worker uses the same source and a
Dockerfile correction to copy the action module required by its existing local
module replacements. Bitcoin remains the pinned `bitcoin/bitcoin:31.1` image.
The normal Stacks image is `stacks-core-attacknet:a12-normal`, exported from the
previously qualified local image; it has no enabled protocol fault control.

| Image | Local build/content ID |
| --- | --- |
| Network operator | `sha256:c951d8f7b03245715d6b005d3c8c1e6e52cfbfaa99b049e15ecebc340c254716` |
| Transaction worker | `sha256:667818bf65d9e7afee8fb22077184d3090f0147c659d4751fef1f5bc8b42b21a` |
| Normal Stacks image archive | `sha256:5b0283ee442bc02dd73807cc9b70ec94b6fd339b82d3de0d863a76c118272b81` |
| Test-only receipt-delay image | `sha256:6afefbb4aafe884d77472c194a7a905a1d00a934c3556dd1be246a7886399941` |

These are local build identities, not a new cross-platform release or upstream
image-lock guarantee. Fresh fixture credentials stay in private local manifests;
no credential values enter test output or this record.

## Bitcoin peer partitions

`TestLiveBitcoinPartition` passed in `partition-bitcoin`, network `chaos` with
weighted `1,1` production every 3 s. Before each fault it observes a common tip.
During injection, actor-to-actor observer RPC fails, both producer receipt
ledgers advance, and the independent chains acquire unequal work. Production
is paused before comparing tips. Native deletion or expiry restores actor RPC;
the nodes converge to the observed heavier branch and production resumes.
Restoring traffic does not roll back the experiment's chain changes.

| Recovery | Split heights | Target receipts during fault | Rejoined heavier tip |
| --- | --- | --- | --- |
| Explicit deletion | 133 / 132 | 78 → 80, 57 → 58 | `3447442aa3ee2c30201cba99c00c11f586b5c0bfe32ffe8616b9b67ddd250c49` |
| 60 s expiry | 135 / 136 | 80 → 81, 59 → 61 | `00b3986bb729471a838ecf88213e6cd2330bb900e1987e2b80d0186cc35007b3` |

Fault UIDs: `c8cb5b5c-c78e-4485-8e25-3b847bd7b1ff` (deletion),
`520f4e63-594f-4de1-b85d-ccfdd2b79f08` (expiry). Actor Pod UIDs:
`94581798-5628-4af8-8f42-0bfb599f19cc` and
`64ce8223-914f-4d50-887e-ac4abb2ed4c7`. A fresh delay request is rejected by the
shared existing-object quota while each partition exists.

## Productive Stacks recovery

The initial `TestLiveStacksBitcoinPartition` run passed in `partition-stacks`,
network `stacks`.
The fixture bootstraps PoX-4 signer enrollment and confirms actual STX transfers
before testing. Bitcoin cadence is 5 s; transfer demand is 10 s. The partition
selects only `miner` and `bitcoin`. During injection, the miner's reported burn
height falls behind while the unselected signer-node and Bitcoin producer make
progress. After cleanup, the miner catches up and a new transfer confirms.

| Recovery | Core / miner / signer-node burn heights during fault | Bitcoin receipts | Confirmed transfers after cleanup |
| --- | --- | --- | --- |
| Explicit deletion | 239 / 237 / 238 | 36 → 38 | 4 → 5 |
| 45 s expiry | 242 / 240 / 242 | 39 → 41 | 5 → 6 |

Fault UIDs: `6ee37693-3a75-45ba-91b9-32667c2f5a59` and
`16c2a7c8-818a-4163-ac5c-4db3d93e2dc8`. Pod UIDs:
Bitcoin `896a9b35-66b6-4b8c-b5e0-3e75ddc3d239`,
miner `bb39c245-9969-4517-8a40-61f0a8ff5465`,
signer-node `5c8809fc-53e1-4c05-8d88-ad249aaf696c`.
The suite does not assume immediate proposal cessation: a miner may still have
usable tenure state. Confirmed inclusion is observed success, not finality.

### Repeat-run recovery limit

The later run with container-incarnation assertions passed cancellation
(confirmed 56 → 57) but **failed** its 120 s post-expiry recovery assertion.
The native object had recovered and finalized deletion. At capture, the miner
still reported burn height 360 and the transfer at nonce 57 remained accepted
and pending; Core's production ledger continued advancing. Node logs repeatedly
reported a missing canonical PoX anchor while processing the next reward cycle.
This is an observed liveness failure in the local Stacks/fixture combination,
not a validated upstream defect or a causal diagnosis.

The expiry fault UID was `d1b4231e-be31-4744-8f4e-c7953944eeca`; the actor UIDs
were unchanged. Preserve the distinction between restored native networking and
protocol progress. The test remains strict and reports this failure; it was not
relaxed, skipped or rerun until green. The earlier successful run remains valid
evidence for its conditions, not a general recovery guarantee.

The fixture is retained with both desired production policies paused, preserving
the pending transaction and node state. Read-only status and recent node/signer
logs are under `/tmp/stacks-partition-stalled-*`; the failed run is
`/tmp/stacks-partition-final-live.log`. Investigating this fixture's reward-cycle
progress is a separate follow-up before broadening Stacks recovery claims.

## Separate control-path failure

The administrator-only receipt test uses its own fresh namespace, without the
public profile or agent grant. Its producer selector is explicitly rejected by
public-profile envtest. The existing test proxy delays one successful real Core
receipt for 15 s to create an observable in-flight window. This is not a runtime
execution adapter or a new recovery mechanism.

The test distinguishes read-only preflight loss from lost receipt knowledge,
checks actual Core height alongside the durable ledger, and normally deletes
the producer Pod while the response path is partitioned.
`TestLiveProducerControlLoss` passed in `partition-control-3`. Loss before dispatch
left height zero, no authorization and no receipt. The
`Unavailable` skip reason combines admission and RPC preflight failures; neither
it nor the unchanged status message isolates which check failed. The in-flight case captured
an exhausted 25 s drain with one outstanding collector. After native cleanup,
the replacement retained the same `Armed/Blocked` dispatch, Core stayed at one
block and the ledger at zero receipts for 12 s (four baseline intervals).
No status edit, reset, retry or automatic readmission was used.

Producer Pod UIDs were `e39ff707-f49d-4cfd-9f0c-68c3bcad4db5` and
`9492efe1-f8cc-427f-95fd-d6c2f91fb8fe`; Bitcoin Pod UID was
`45914864-d0c6-4fd1-bfed-9dacc3a011af`. Fault UIDs were
`263fb0c2-cac7-4474-8242-68cf39b6af46` (preflight) and
`6c32546d-a857-44b3-8d6e-c1a71e978217` (in-flight, requested 120 s,
explicitly deleted). Native finalized deletion and local observer RPC are
verified after removal; the test does not run a separate RPC probe from the
replacement producer container. Native `mode: one` selected the old producer
Pod; the replacement was not selected in this run. Its assertions establish
retained uncertainty, not operation under a continuing replacement-Pod partition.
Injection has a 20 s test bound while the proxy holds a receipt for 15 s; slow
injection can fail the test. Receipt-state and drain assertions prevent that
race from silently qualifying a receipt that arrived before the fault.

Earlier test iterations corrected two harness assumptions: unavailable preflight records a skipped
opportunity rather than `Blocked`; a replacement Pod can be Running before it
acquires the old leader lease. Neither correction changes runtime behavior.

## Delay regression under the combined profile

`TestLiveNativeDelay` passed in `partition-bitcoin` with the same profile. Actor
RPC medians were 100.29 ms before injection, 1103.77 ms during cancellation and
1124.35 ms during expiry, then 76.96 / 111.40 ms after recovery. Destination
receipts advanced 28 → 30 and 30 → 31. The expiry case also replaced the native
controller and observed the delay before recovery. This is a new combined-profile
run; the earlier delay matrix remains a separate dated qualification.

## Focused review follow-up

The follow-up reused this cluster with fresh `partition-followup-bitcoin` and
`partition-followup-stacks` namespaces and independent credentials. The worker
image `stacks-transaction-worker:partition-followup-20260907` built successfully
without the redundant action-source copy; its local image ID is
`sha256:943dada0cbc0e410be36b50a8b6fc11ee2fce2ae4a6ac21b996e9f572a755e96`.
The operator runtime, normal Stacks image and upstream fault components did not
change. The retained `partition-stacks` failure was not reset or modified.

The Bitcoin suite now checks RPC round trips from both actors before, during
and after injection. Both directions failed during each partition and recovered
after cancellation/expiry. TCP round trips need traffic in both directions,
so this is stronger reachability coverage, not independent packet-direction proof.

The fresh Stacks suite observed confirmations 5 → 6 before injecting anything.
It passed cancellation and expiry while collecting Core height and both nodes'
reported PoX cycle IDs, lengths, boundaries, countdowns and activity. Samples
are timestamped and sequential, not atomic. The derived phase label refers only
to the reporting node's height and boundaries. Missing/invalid context is logged
as a gap, without changing the strict recovery assertion.

| Case | Native cleanup: Core / miner / signer-node | Protocol recovery | New confirmation |
| --- | --- | --- | --- |
| Cancellation | 242 / 240 / 242 | Core and both nodes at 243; cycle 12, reward window | 6 → 7 |
| Expiry | 252 / 243 / 252 | Core and both nodes at 257; cycle 12, prepare window | 7 → 8 |

The expiry run reported prepare start 255 and reward start 260. At recovery,
`blocks_until_prepare_phase=-2` and `blocks_until_reward_phase=3` were retained
as reported. The test did not schedule around these boundaries. Fault UID for
expiry: `7bb7a9ad-5541-47a5-89bd-84953165c0de`. Miner Pod UID:
`833dc024-c255-4349-b3f8-2a032fde171b`; signer-node Pod UID:
`ee62871b-252b-43cc-a000-8eb62521c213`.

The retained paused fixture separately exercised the new baseline gate. Its
historical confirmation count did not authorize injection: the test timed out
before creating any fault. Timeout diagnostics showed Core height 406, both
Stacks nodes at 360, cycle 18, and next prepare/reward boundaries 375/380. This
expected negative check is recorded as an exit-1 test, not a successful recovery.
It used read-only observations; no new fault or production policy change occurred.

Follow-up live/build/check logs use `/tmp/stacks-partition-followup-*`, including
`stacks-live.log`, `stale-baseline.log` and `bitcoin-final-live.log` suffixes.
The successful follow-up fixtures are retired in documented order; the original
stalled fixture and control-3 ambiguous ledger remain retained and paused.

## Evidence scope and repetition

Admission tests run the common bounds, permissions, lifecycle, and legacy cleanup
matrix for delay-only, partition-only and combined modes. They also check
mode-specific enablement, partition directions, mixed mechanisms, and producer
selector denial. Envtest supplies real API/authorizer semantics, not native
injection or the quota/type-checking controllers. The live suites supply those
missing pieces and separate native lifecycle from observed protocol effects.

Tests pin observed actor Pod identities and check container identity/restart
stability; the native selector contract itself still does not pin UIDs. Broader
fault kinds, other CNI/runtime/architecture combinations, actor replacement,
daemon/node loss, dynamic inventory admission, and passive journal/divergence/gap
correlation remain open. Duration still depends on working native cleanup.

Use the [fixture and opt-in instructions](operations.md#partition-qualification-fixtures).
Task-local logs use `/tmp/stacks-partition-*.log`; they are review aids, not a
shipped evidence-retention service. Productive fixtures need a valid signer lock
when repeated. The control test always requires a fresh isolated environment.

The retained Bitcoin, Stacks and control-3 fixtures have desired production
paused and no native faults. Superseded control namespaces were removed; the user has removed older
qualification clusters independently. For orderly
teardown, delete each `StacksNetwork`, wait for its retained production ledgers,
then remove its namespace or operator release. Direct namespace deletion can
remove colocated controllers before their finalizers finish.
