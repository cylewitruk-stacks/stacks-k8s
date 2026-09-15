# Bitcoin split-view and persistent Stacks burnchain stall

A defensive experiment on 2026-09-15 combined contract load, actor stress, Bitcoin
split views, a bounded reorganization and peer churn. The split/reorganization case
left all Stacks nodes processing one tenure while their Bitcoin burnchain views stopped
advancing. This is a candidate upstream defect, not an established vulnerability.

## Configuration

- Shared three-node `stacks-k8s` kind cluster; fresh namespace
  `adversarial-20260915`.
- Network UID `fe2e4cc2-5597-4f22-883e-8f672f7845e1`: three Bitcoin nodes, one
  Stacks miner, two paired Stacks nodes/signers, two stackers and four capability
  workers.
- Bitcoin Core 31.1 and official Stacks node/signer 4.0.3 images. The observed node
  image digest was
  `sha256:9c45dbf25dbe6a2b5051657f914b98abccfbec1a9f84d1df45233d627446227d`.
- Managed initialization reached PoX-5, burn height 307, Stacks height 113 and
  reward cycle 15 before faults. A 60-second Bitcoin cadence was active during the
  split experiment.
- NetworkTelemetry UID `d9812c9f-0b29-4f76-a4b0-bf8b03406c03` wrote exact-identity
  objects, logs, native metrics and container resources to tables prefixed
  `stacks_2b47a34a6cbf429a9b36c2441e56bd82`.

Raw local evidence is under `/tmp/stacks-adversarial-20260915/`. It is temporary
working evidence, not a portable signed export. Credential files are removed during
cleanup.

## Workload and fault cases

The contract workload used map entries with wide tuples, buffers, repeated reads and
writes, and nested contract calls inside `fold`. A 64-write, 512-read, 2,048-byte
payload profile was accepted; the larger 64-write, 1,024-read, 4,096-byte profile was
rejected before it could burden the chain. Sixteen accepted calls ran while the miner
had 95% CPU stress and one signer-node path had 1,000 ms one-way latency for 120
seconds. All 16 calls were included successfully; observed inclusion latency was
3–87 seconds with a 27-second median, and the sampled mempool peaked at 16.

The persistent failure used this sequence:

1. Partition `btc-01` and `btc-02` for 120 seconds and fail the `btc-07` Pod.
2. Generate six blocks on `btc-01` and two blocks on `btc-02`. Before the
   reorganization, their heights were 357 and 354 on distinct tips.
3. While the partition remained active, run a depth-two `BitcoinReorganization` on
   `btc-01`. Action UID `c20a9f4b-120e-4048-9aab-b01cf8617797` completed with cleanup
   acknowledged at 03:34:29 UTC.
4. Let the partition expire and the third node restart, then observe all chains without
   further mutation.

The Bitcoin nodes converged at height 360 on tip
`719640f7d79719b01647e2fa6191bd71e54922426adcc12e7653c638df741fd3`.
They later agreed at height 394 on another common tip. All three Stacks nodes remained
at processed burn height 359 while agreeing at Stacks height 308 and tip
`7fcb7ebf691b84f016552b535298616418e7c434753b7feebcbd3d9105767b58`.
The resulting 35-block gap persisted after the Bitcoin fault objects were gone.

Greptime retained more than 3,000 matching missing-anchor records from each Stacks
node over the sampled interval. Repeated messages included `Missing canonical anchor
block`, `No PoX anchor block known yet for cycle 18`, and failure to find the prepare
phase start ancestor at height 356. Signer logs also reported unavailable cycle-18
reward-set state. Stacks height and baseline transfers continued to advance within the
existing tenure, so Pod readiness, transaction inclusion and Stacks-tip movement alone
did not establish current burnchain integration.

A separate peer-churn case deleted `signer-node-01`. Kubernetes reported its replacement
Ready before its RPC and native protocol observations recovered; the aggregate remained
non-Operational during that interval and all nodes later agreed. This validates the
existing separation between workload readiness and protocol progress.

## Diagnosis and scope

The exact trigger is not reduced. An earlier, shorter split converged without a
persistent stall. The successful trigger crossed the cycle-18 boundary and combined a
three-node split, asymmetric branch lengths, a depth-two replacement and one unavailable
peer. The evidence supports an association with missing PoX anchor/prepare ancestry; it
does not establish which operation is necessary, causal minimality, exploitability or
mainnet impact.

The root reported `Unknown/TrafficObservationUnavailable` while Bitcoin observations
were 35 blocks ahead of every miner's processed burn height. An experimental height-gap
gate reported `Operational=False/BurnchainProgressOverdue` after an operator rollout.
That gate is not the supported health contract: burst generation and independent forks
can produce a height gap without a stall. The supported diagnostic exposes attributed
height samples separately from Operational; this correction has not been live-qualified
by the recorded experiment.

The observability recorder also watched only `NetworkChaos`; `PodChaos` and
`StressChaos` transitions were absent. The recorder and namespace RBAC now use one
bounded native-fault allowlist for all three kinds. Live probes retained exact
`ADDED`/`MODIFIED` events for a no-target `PodChaos` and `StressChaos` without mutating
an actor.

## Cleanup and verification

See the [findings ledger](findings-20260915.md) for stacks-k8s dispositions and the
separate upstream hypothesis. The network, actions and fault profile are removed after
final evidence capture. Greptime data remains subject to the configured TTL; the shared
cluster and common addons remain running.

Native-fault recorder sources were exercised with no-target probes, not another
consensus fault. Raw logs are not archived in this repository; the dated observations
and identifiers above are the retained summary.
