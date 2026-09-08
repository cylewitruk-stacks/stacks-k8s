# Managed operation qualification

Status: Qualified for the initial direct PoX-4/PoX-5 profile on 2026-09-07.
This is one image/platform qualification, not general epoch or fault compatibility.

## Boundary

Cluster: explicitly selected three-node `stacks-k8s`, Kubernetes 1.37.0.
Actors: Stacks 4.0.1 (`62e03cc`, arm64) and Bitcoin Core 31.1.
Sources: the pinned real sBTC contracts, not unit-test substitutes.
Each experiment uses a fresh namespace and generated keys/configuration.
Startup consists of applying generated resources and installing the chart;
no external bootstrap or mining command is used.

## Iterations

| Namespace | Observation | Disposition |
| --- | --- | --- |
| `managed-first` | Automatic wallet preparation and Bitcoin advancement; PoX-4 submission accepted and native PoX reported stacked STX, but its legacy receipt was unavailable from the Nakamoto index. | Retained, production/operation paused and actors suspended. Public state in `/tmp/stacks-managed-first/held-receipt-state.json`. |
| `managed-second` | Legacy callback receipt accounted; real sBTC sources and registry initialized; transfers confirmed. At height 244 the old lock was released while `/v2/pox` still selected PoX-4; reenrollment returned canonical `(err none)` and blocked the participant. | Retained, paused and suspended. `/tmp/stacks-managed-second/observations.jsonl`, `pox5-stopped-state.json`, and `transition-rejection.json`. |
| `managed-third` | Automatic initialization, PoX-5 enrollment, sustained progress through reward cycle 20, successful renewal, pause/restart and teardown. | Removed in network → ledgers → chart → namespace order; evidence retained under `/tmp/stacks-managed-third/`. |

The first iteration also identified the strict PoX-4 activation boundary and
missing read permission for prerequisite status. Those startup issues were
corrected before the second experiment. The second failure resulted in an
acknowledged unsuccessful transaction; its nonce was not reset or replayed.

## Automated evidence

Unit tests cover account CAS authorization, lost acknowledgements, restart
observation, receipt retention/acknowledgement, contract source checks, dependency
ordering, epoch/renewal windows, startup-stage retention, and legacy callback
origin/identity/canonicality checks. Envtest exercises managed account/contract
admission, immutability, parent compilation, identity retention, topology progress
with an unavailable account, and finalizer release during environment removal.

Callback tests simulate node delivery and native canonicality; they do not
constitute protocol qualification. SDK tests use offline signing and explicit
synthetic contract artifacts for provisioner tests. Live experiments use the
pinned upstream sources. No bridge daemons, delegated pools, fault injection or
mixed-version compatibility is qualified by this delivery.

## Successful run

Images: `stacks-network-operator:managed-r6` and
`stacks-transaction-worker:managed-r6`, built from this delivery. The only later
API edit clarifies the enrollment-amount description; it does not change schema
validation or runtime behavior.

| Observation (UTC, 2026-09-07) | Bitcoin height | Stacks height | Confirmed transfers | Other evidence |
| --- | --- | --- | --- | --- |
| 18:55:28, first observed PoX-5 reward cycle | 261 | 79 | 19 | Reward cycle 13; holder unlock height 500 |
| 18:55:48, fresh progress after that observation | 265 | 85 | 21 | Both chain height and confirmation count increased |
| 19:05:32, renewal observed | 381 | — | — | Unlock height increased 500 → 620; nonce 2, exact successful native receipt |
| 19:07:12, later complete periodic snapshot | 401 | 286 | 89 | Reward cycle 20; progress continued after renewal |
| Final direct reads before removal | 405 | 287 on both nodes | — | Both nodes reported PoX-5 and identical genesis chainstate hashes |

Renewal TxID:
`35fb6bc2bc6f5a32aa07c8ad65447bf33eff456ac48385699f7305a11c08f723`.
Its observed canonical block ID:
`b4d3998e395a33ab3a56530f6feafaff2535f22e5c8d70f79eb45b7f9eb7f42c`.
The initial holder receipt is retained in the periodic evidence as `LegacyEvent`;
subsequent enrollment/renewal uses `NativeIndex`.

The automated pause/restart check waited for completed PoX-5 startup, paused
only managed operation, observed Bitcoin blocks 274 → 275 while paused, replaced
the maintenance worker Pod, verified unchanged accounted transactions, and
resumed both capabilities to `Ready`. It tests restart of acknowledged work;
unknown/pending transaction restart behavior is covered by unit tests, not this
live rollout.

`validated-summary.json` asserts fresh Stacks/transfer progress after the first
observed PoX-5 reward cycle, exact successful renewal with increased unlock
height, and further progress after renewal. It excludes incomplete samples
captured concurrently with teardown; original samples remain in
`observations.jsonl`. Native observations and Kubernetes reads are separate
samples, not an atomic distributed snapshot.

The finish script asserted height/renewal completion, read both native nodes,
paused new authorizations, waited for idle Bitcoin dispatch and accounted Stacks
work, then removed the network, all ledgers, chart and namespace. Teardown
completed at 19:08:16 UTC. Relevant artifacts are `pause-restart.json`,
`sustained-final-state.json`, `final-native-observations.json`,
`drained-state.json`, `final-kubernetes.json`, and `teardown.json`.
Private `environment.json` files contain credentials and must not be published.

## Deployed image identities

These are runtime image IDs captured from the final Pods, not mutable tag claims.

| Image | Runtime identity |
| --- | --- |
| `docker.io/bitcoin/bitcoin:31.1` | `docker.io/bitcoin/bitcoin@sha256:da25cedc66b1daefff9f412ee196c901a899c3fa68a33b20849c3e08b5c40d63` |
| `docker.io/library/stacks-core:4.0.1-pox5` | `docker.io/library/import-2026-09-07@sha256:8d8ebeced15b4f9abdee7d77aaa1969667112092aab77b697252f675368b7794` |
| `docker.io/library/stacks-network-operator:managed-r6` | `docker.io/library/import-2026-09-07@sha256:5a87c4cd2f80a68395e104720c7f3a0f411f0d470471e6408704c5514bd83d6e` |
| `docker.io/library/stacks-transaction-worker:managed-r6` | `sha256:3d94f71a59c53b32bd6615173cccfda2dc94025b109a4b4d4609a438afb11207` |

## Verification and retained environments

`make verify` passed with a temporary populated Git index; the user's staged
index was unchanged. Focused account/production/transfer/managed-operation tests
passed under the race detector. SDK, envtest, generated-drift, exact RBAC,
workload/Helm, `make vuln`, and `make docker-check` checks passed.
Hosted CI and native chaos fault requalification were not run.

`managed-first` and `managed-second` remain paused with actors suspended for
review. The pre-existing `pox5-final` fixture was not modified. The successful
`managed-third` namespace is absent. The shared cluster is returned to its
initial stopped state after evidence capture.
