# Bitcoin observer qualification

The 2026-09-14 qualification uses the existing three-node arm64 `stacks-k8s` kind
cluster (Kubernetes 1.37.0), Bitcoin Core 31.1, OpenTelemetry Collector 0.160.0 and
GreptimeDB 1.2.0. A fresh network enables the observer on two Bitcoin participants;
a third omits it. The complete Stacks declarations resolve and freeze genesis, but
this qualification exercises Bitcoin observation before full Stacks initialization.
It does not qualify PoX transitions or claim protocol recovery after a Chaos fault.

Fork qualification image: `stacks-network-operator:bitcoin-observer-final-20260914`, namespace
`bitcoin-observer-final-20260914`, network UID `3d0920f1-8e53-4928-b251-fcbcb4b4e381`.
At 13:44 UTC, both observed tips were height 14 with chainwork `0x1e`:

- `btc-01`: `40a6946ab5fadeb825c8b66a319c2f6539167115fa74d8e3f6a8cd95f49d45b4`
- `btc-02`: `3a508069dde5ce0bd18abecfc2c33ad3768bad56d1cece1ff027438c3a93b8fd`

Greptime returned height 14 for both distinct participant/Pod identities. The three
[documented Bitcoin queries](../observability/bitcoin-observer-queries.sql) returned
23 height rows, 24 collection-status rows and 64 structured observation rows before
teardown. The replacement `btc-01` Pod UID was `4cb62e15-adf8-4da4-87db-616c0395f458`;
its participant UID remained `75f70110-6bb8-4708-afbd-ec62d9795e1b`.

## Exercised behavior

| Check | Evidence |
| --- | --- |
| Scope | Two two-container Bitcoin Pods; the third Bitcoin Pod has only Core. |
| Permissions | All five observation methods succeed; Core returns HTTP 403 for observer calls to generation, invalidate/reconsider, submission, stop and wallet creation. |
| Fork identity | With baseline production paused and P2P disabled, two nodes generate distinct hashes at equal height. Greptime retains both with exact participant/Pod UIDs and hexadecimal chainwork. |
| RPC outage | The hosting kind node suspends the exact Core PID; `/proc` confirms stopped state. The observer stays running, reports failure and omits protocol metrics. |
| Recovery | Resuming Core restores successful collection without changing either container ID. Greptime contains failure and success samples. |
| Replacement | Deleting one actor Pod creates a new Pod UID; samples are attributed to the replacement while prior evidence remains queryable. |
| Teardown | Stop, root deletion, participant removal, recording deletion and namespace removal complete. Greptime retains recorded observations. |

The initial inside-container `kill -STOP 1` attempt did not suspend Core and is not
outage evidence. The verified outage uses the hosting kind node's container-runtime
PID. Direct administrative RPC calls create the fork while normal production is
paused; these are qualification actions, not new operator capabilities.

Unit/race tests exercise failure freshness, malformed/null/missing/oversized responses,
wrong receipt IDs, cancellation, concurrent scrapes, per-method decoding isolation and
credential/workload boundaries. Envtest exercises interval admission, scoped resolver
outputs and persistence of the exact observer credential UID. HTTP scrape availability
and RPC collection success remain separate assertions.

## Lifecycle and failure-classification follow-up

At 14:48–14:49 UTC on 2026-09-14, image
`stacks-network-operator:bitcoin-observer-correction-20260914` ran in fresh namespace
`bitcoin-observer-correction-20260914`, network UID `ed11d8ff-08f1-43f6-93dc-af38cfe89a6a`.
This focused run used the same cluster/backend and did not repeat fork creation.

- Disabling `btc-01` removed its observer container, volume and generated RPC principal.
  Re-enabling retained credential UID `aa1396b1-e42d-456a-9b78-8fee8d1457e3` and identical
  private credential bytes. Each change replaced the actor Pod; the re-enabled Pod UID
  `0e808159-1d2e-4777-8c69-1b96f51fd52f` appeared in Greptime metrics with the original
  participant UID `53c855ee-e28b-48e4-a362-83e8f52da0ab`.
- Suspending only `btc-02` Core produced a Greptime JSON record with `success: false`,
  `failedMethod: getblockchaininfo` and `failedReason: deadline`, no protocol facts and
  the correct participant/Pod UIDs. Resumption restored collection in the same containers.
- These fixture actors already requested resources, so all three `btc-01` Pod incarnations
  were `Burstable`; this run does not demonstrate a QoS-class transition.

Unit/race regressions additionally verify shutdown during an outstanding RPC without
publication/accounting, shared deadline attribution, every failure reason, the complete
metric vocabulary/types, the 128/129-tip boundary, unsupported chain/status rejection
and credential separation. Envtest verifies StatefulSet container/volume removal and
re-addition with stable workload and credential identity; actual Pod rolls are live evidence.

The network, recording and namespace were removed in order; recorded observations remained
queryable. The shared cluster stays running with the corrected image installed and the
observability release's original namespace scope restored.

## Fast bootstrap and stable diagnostics — 2026-09-15

Image `stacks-network-operator:slice1b-20260915` ran on the shared three-node kind
cluster in namespace `slice1b-20260915`, network UID
`9ae6e338-0809-4bb0-a77b-145cc07e60d2`, using Bitcoin 31.1 and Stacks node/signer
4.0.3. All three Bitcoin nodes reached height 203; `PrepareBitcoin` completed at
07:18:25 UTC using a one-second schedule. Native control polling and peer convergence
still bound throughput. A separately selected five-second schedule then completed
all protocol gates, including PoX-5, by 07:26:55 UTC. This does not qualify all
protocol initialization at one-second cadence or alter the 60-second product default.
The run did not wait for the separate post-waterfall progress requirement:
`Initialized` remained false when production was paused for diagnostic checks.

With production cooperatively paused, both height-296 diagnostic samples retained
identical identities and `firstObservedAt=07:30:36Z` across a 20-second observation
while native source timestamps advanced. Greptime returned 785 observer-success rows
for recording UID `f8d33fe2-87f0-4f39-a513-e105f80d5785` at 07:31:11 UTC.
Suspending the exact Core processes on all three hosts withdrew the Bitcoin sample
at 07:31:30; resuming those processes restored a new sequence at 07:31:31.
Replacing the selected Bitcoin Pod changed its UID from
`d5511ad5-30bc-4269-a596-4b2e123dd1bd` to
`4486014c-0ad2-4897-bdba-70f917e9fdca` with a new container and sequence timestamp.
Brief production resumption advanced height 296 to 297 with a new timestamp at
07:33:23; production was paused again for evidence export.

The qualification applied generated CRDs explicitly: Helm upgrading the Deployment
alone left the prior schema installed and pruned the diagnostic field. An initial
scheduler iteration also exposed a stale preflight offer delay; the final scheduler
replaces unsent offers after fresh, converged height/tip changes. Both corrections
precede the observations above. Unit/race and API-server offer-publication/accounting
tests cover the timing fix; `make verify` passed without generated drift.

Public working evidence is under `/tmp/stacks-slice1/`; temporary scripts and files
are not a durable release artifact. The network was stopped and deleted, followed
by its recording and namespace.
The [portable export](../observability/evidence-export-qualification.md) remained
verifiable after teardown; the shared cluster remains running.
