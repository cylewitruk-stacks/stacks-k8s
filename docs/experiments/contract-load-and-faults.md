# Contract load and signer communication faults

A defensive experiment on 2026-09-14 combined nested Clarity calls with bounded native
Chaos Mesh faults. The network stalled and then recovered after every fault. The tests identified a
configuration-dependent source of additional lag and several observability improvements;
they did not establish a new Stacks vulnerability.

## Configuration

- Shared three-node `stacks-k8s` kind cluster; fresh namespace `whitehat-20260914`.
- Network UID `c339632c-af13-4e5f-a9e8-33aba42c1335`: three Bitcoin nodes, one Stacks miner,
  two paired Stacks nodes/signers, two stackers, and production/contract/faucet workers.
- Bitcoin Core 31.1; Stacks reports `4.0.1-iteration2` at `f9b022bff5550d1e9938e1f40805ea354381a59e+`.
  The `+` denotes a modified build. Results do not establish behavior of an unmodified release.
- Managed genesis-to-PoX-5 initialization completed before stress. At the baseline,
  burn height was 303, active contract was `pox-5`, and the native cycle-15 signer set
  had two weights of 15. The observed signing threshold was 21 of 30.
- Five-second Bitcoin cadence during measurements; one-second cadence was used only
  before the initial Stacks readiness gate. Native metrics were scraped every ten seconds;
  Bitcoin observers polled every five seconds. Actor logs, object changes and external
  experiment-phase Kubernetes Events were sent to Greptime.

## Workload

The [three contract sources](../../examples/experiments/contract-load/README.md) implement
map storage, a relay, and a driver with nested contract calls inside `fold`. Three unused,
genesis-funded accounts submitted finite nonce-chained batches; managed workers did not
share those accounts. The external Go signer used `libs/stacks`. All calls used a fee of
1,000,000 µSTX and were submitted once with exact canonical TxID/result checks.

| Profile | Tuple fields | Buffer bytes | Writes / reads per call | Total calls |
| --- | --- | --- | --- | --- |
| Light | 2 | 16 | 1 / 4 | 3 |
| Medium | 8 | 256 | 8 / 64 | 12 |
| Wide | 16 | 1024 | 16 / 128 | 24 |
| Heavy | 16 | 4096 | 32 / 256 | 36 per batch |

The same heavy profile was used without faults, with delay, with partition, and in the
nonblocking-delivery comparison. Inclusion latency means time until the external poller
first observed exact canonical inclusion, not an exact block timestamp; five-second polling
and sequential RPC calls add observation delay.

## Evidence

Raw evidence, public fixture inputs, external driver sources, TxIDs, query pages and logs:
`/tmp/stacks-whitehat-20260914/`. This temporary directory is not durable archival storage.
Network/participant/Pod UIDs and fault UIDs bind observations; transaction hashes and
block identities are retained separately from Prometheus labels.

## Results

| Case | Calls | Median / maximum observed inclusion latency | Longest sampled flat Stacks tip during fault |
| --- | --- | --- | --- |
| Light | 3 | 5.35 / 5.43 s | — |
| Medium | 12 | 5.44 / 5.47 s | — |
| Wide | 24 | 5.79 / 10.99 s | — |
| Heavy, no fault | 36 | 27.88 / 48.86 s | — |
| Heavy + 500 ms delay | 36 | 116.91 / 132.93 s | 82.33 s |
| Heavy + partition | 36 | 128.80 / 144.72 s | 83.41 s |
| Heavy + delay, nonblocking callbacks | 36 | 126.74 / 152.89 s | 81.66 s |
| Delay, baseline traffic only | No new stress calls | Not measured | 71.12 s |

All **186 external transactions**—three deployments and 183 calls—were canonically
included with successful results. No ambiguous send was retried. Heavy calls built a
backlog of 36 external transactions in addition to normal network traffic. The baseline-only
reduction confirms that complex calls are unnecessary for a long pause in this profile;
they increase the backlog and time to inclusion.

Each native fault requested 90 seconds, selected exact network/participant identities at
both endpoints, and targeted `signer-01` ↔ `signer-node-01`. Delay requested 500 ms in the
signer-to-node direction with zero jitter; partition was bidirectional. Chaos Mesh recorded
successful Apply and Recover operations, `AllRecovered=True`, and finalized deletion.

| Fault UID | Apply / recover (UTC, 2026-09-14) |
| --- | --- |
| `62866eed-d742-48e0-8036-9dc2a28028d5` | 21:05:02 / 21:06:32 |
| `0692d054-93a9-43ae-93b0-2810691dc46a` | 21:07:18 / 21:08:48 |
| `5d13d28d-2371-4465-9c86-04e28b3d1553` | 21:13:29 / 21:14:59 |
| `ba4fc428-61e6-48e0-8df8-853a4b6474d3` | 21:17:24 / 21:18:53 |

## Diagnosis

1. **The load executes real work.** Native miner logs contain the submitted TxIDs and
   increasing `cost_before`/`cost_after` values, including soft-limit observations.
   Exact inclusion results match the expected fold sums. This is workload pressure,
   not a queue of invalid contract calls. Finite batches do not establish sustainable TPS.
2. **Bitcoin control continues.** Greptime's two Bitcoin observers both show advancing
   heights and successful polls throughout every fault window. For example, their sampled
   heights move 359→375 and 360→376 during the first delay. Stacks nodes remain scrapeable;
   the affected signer's metrics scrape briefly fails during partition.
3. **Signing cannot reliably reach the threshold.** Miner logs record `15/30` rejection
   weight, `weight_threshold: 21`, `SortitionViewMismatch`, and signature-gathering failures.
   The affected node records repeated callback response timeouts. Losing effective access
   to one equal-weight signer is enough to prevent this cohort from confirming proposals.
4. **Synchronous callbacks amplify the fault.** The generated profile sets
   `event_dispatcher_blocking=true` with queue size 1000. In the selected Core implementation,
   blocking mode makes the effective queue size **zero**. During the first delay, the paired
   node's burn view stops at 361 while peers reach 377; during partition it remains at 387.
   Repeated callback retries are consistent with blocking the node's processing path.
5. **The configuration comparison separates the effects.** After recovery, an explicit
   `StacksNode.spec.config.overrides.node.event_dispatcher_blocking=false` update rolled
   only that actor. The mounted setting was verified. Under the same heavy load and delay,
   all three burn views advance 462→478 together, while Stacks remains at 342. Nonblocking
   delivery removes the observed burn-view lag; it does not restore missing quorum.

No actor or management worker crashed. The five bound management Pod UIDs stayed unchanged;
the only actor replacement was the deliberate configuration comparison. Recovery required
no manual nonce repair, chainstate reset or worker restart.

These are single-run comparisons on a modified local image. Reward cycles are accelerated
and the faults span cycle boundaries; boundary-specific amplification is not isolated.
The nonblocking experiment does not qualify a new global default or guarantee recovery
under other durations, workloads or signer distributions. `Operational` uses observation
and progress windows, so it is not an instantaneous stall detector.

## Observability findings and next work

See the [findings ledger](findings-20260914.md) for priorities and dispositions. The main
follow-ups are preserving watch continuity, qualifying nonblocking event delivery, and making
agent queries respect backend memory limits. Native cost units and sampling limits must be
explicit in dashboards. New recordings always retain Metrics API CPU/memory samples for
exact actor identities to improve resource-bottleneck attribution.

Greptime retained metrics, actor logs, phase Events and native fault state/cleanup. Coverage
gaps were reported: one fault first appeared as an active snapshot, which retained its
embedded Apply timestamp but not the earlier watch transitions. Exact external submission
and inclusion timings remain in the experiment ledger; this is not a complete transaction trace.

## Cleanup and validation

The namespace fault profile was uninstalled after all native faults recovered and were
deleted. The network was stopped and deleted, its participants and recording were removed,
and the namespace was deleted. A post-deletion Greptime query still returned Bitcoin
observation records. The shared cluster remains running; observability enrollment is reset
to `watchNamespaces: []`.

Raw logs are not archived in this repository. The dated measurements and deployed
contract sources are retained; temporary files and backend TTL do not provide durable
evidence storage.
