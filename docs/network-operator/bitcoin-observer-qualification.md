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
