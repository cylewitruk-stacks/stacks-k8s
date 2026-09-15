# Bitcoin RPC observer

Enable the optional Go observer on a reusable `BitcoinNode` definition or its typed
participant inline declaration/override:

```yaml
apiVersion: bitcoin.stacks.org/v1alpha2
kind: BitcoinNode
metadata:
  name: bitcoin
spec:
  observer:
    enabled: true
    intervalSeconds: 10
```

Omission disables it. The interval defaults to 10 seconds and accepts 5–300 seconds.
The network operator adds a `bitcoin-observer` container to the actor StatefulSet,
using the independently pinned `bitcoinObserverImage` chart value
([chart default](../../charts/stacks-network-operator/values.yaml)) and `/bitcoin-observer`
binary. Each completed poll
is followed by the interval; scrapes never initiate RPC calls or catch-up work.
Changing observer settings rolls the actor Pod through the normal workload update
path. Changing the observer image replaces the actor Pod; changing only the controller/resolver
image leaves the observer pin unchanged. The same built image contains both entrypoints,
but their deployment settings are independent. Standalone controller invocations must
set `--bitcoin-observer-image`; the binary has no implicit deployment default.
Enable observation before starting an experiment.

## Ownership and permissions

The scoped Bitcoin configuration resolver generates an immutable observer credential,
pinned in `status.runtime.observerRPCSecretRef`. Bitcoin Core enforces a separate
`rpcauth` principal and `rpcwhitelistdefault=1` with exactly these methods:

- `getblockchaininfo`
- `getchaintips`
- `getnetworkinfo`
- `getmempoolinfo`
- `getnettotals`

The exporter mounts only this credential, not the node configuration, chainstate,
actor credential or mutation credential. It has no Kubernetes API token or RBAC.
The operator reads credential metadata only; the resolver Job handles private data.
Disabling the exporter retains its credential identity but removes its server
principal from the generated configuration. Re-enabling reuses that identity;
a missing or replaced pinned Secret is an error.

The observability operator never injects the sidecar or changes Bitcoin workloads.
The exporter remains usable without observability or Greptime. Complete custom
Bitcoin configurations must provide the managed authentication settings to support
collection; an `Unverified` configuration may instead produce unavailable observations.

## Observations

Each attempt makes five sequential RPC calls under one five-second deadline, with
no retries and a 64 KiB response limit per call. Missing, malformed, null or
unsupported fields fail the sample. Chain tips are limited to 128 entries; exceeding
that bound fails collection rather than silently truncating the branch inventory.
Only regtest observations are accepted in this profile.

`/metrics` listens on port 9332, named `metrics` for Pod discovery. Numerical gauges
include block/header heights, initial-download state, branch-tip count, peer count
and mempool size/usage. Traffic counters reflect the Core process lifetime.
The sidecar requests 10m CPU/32 MiB memory and limits 100m CPU/64 MiB memory.
These resources can change the actor Pod's QoS class: an otherwise `BestEffort` or
`Guaranteed` Pod becomes `Burstable`. Observation therefore changes scheduling and
resource-pressure behavior as well as adding RPC/CPU overhead; enable it consistently
when comparing experiments. See [Kubernetes QoS](https://kubernetes.io/docs/concepts/workloads/pods/pod-qos/).

| Metric | Meaning |
| --- | --- |
| `bitcoin_observer_success` | Latest complete poll succeeded and is no older than interval + 5 seconds. |
| `bitcoin_observer_last_success_timestamp_seconds` | Successful completion time; zero before any success. |
| `bitcoin_observer_last_attempt_timestamp_seconds` | Latest attempt completion time; zero before polling. |
| `bitcoin_observer_poll_errors_total` | Failed attempts in this exporter process. |
| `bitcoin_block_height`, `bitcoin_header_height` | This node's selected block and known header heights. |
| `bitcoin_initial_block_download`, `bitcoin_chain_tips` | Native download flag and branch inventory count. |
| `bitcoin_connections` | Current peer count. |
| `bitcoin_mempool_transactions`, `bitcoin_mempool_bytes`, `bitcoin_mempool_usage_bytes` | Current mempool facts. |
| `bitcoin_network_received_bytes_total`, `bitcoin_network_sent_bytes_total` | Core's cumulative network traffic. |

Collection stops at the first failed step. Failed or stale samples omit every protocol
gauge/counter, including facts from earlier successful calls; later methods are not
requested. Collection metrics remain. This complete-sample policy avoids mixing partial
observations with different freshness, at the cost of losing unrelated facts for that poll.
`up=1` means the HTTP scrape succeeded, not that Core was reachable. There are no
RPC-dependent readiness/liveness probes on the sidecar. RPC failures do not restart
it or Bitcoin. Polling begins immediately, including while Core starts; failed polls
before the first success count as collection failures, not an established network outage.
`bitcoin_observer_last_success_timestamp_seconds == 0` identifies that startup interval.
Container startup, scheduling and crashes still share the Pod's
readiness/lifecycle; Pod network or resource faults can affect collection too.

Every completed attempt also emits one JSON line with `event: BitcoinObservation`,
`startedAt`, `observedAt` and `success`. Successful records include the exact selected
hash, hexadecimal chainwork and branch tips. Failures identify `failedMethod` (the failed
step, not the underlying cause) and a
bounded `failedReason`: `deadline`, `transport`, `response`, `validation`, `bound`,
`request` or `unknown`.
`deadline` refers to the shared five-second budget, which earlier calls may have consumed;
`response` covers unsuccessful HTTP/RPC envelopes, `validation` covers missing/malformed
or unsupported facts, and `bound` covers response-byte or tip-count limits. Failures omit
protocol facts and server error bodies. `request` means the client could not construct the
request; `unknown` makes no transport/response attribution for an unclassified error.
Process shutdown cancels an unfinished poll without
publishing it or incrementing errors; the observer's own deadline remains a counted failure.
Hashes never become metric labels.
`getblockchaininfo` supplies the selected chain tuple; the other calls occur separately
within the recorded time window. This is not an atomic multi-RPC snapshot or a
reorganization verdict. Equal heights do not establish chain agreement.

## Greptime collection

Enable both `sources.metrics` and `sources.logs` on a namespace-enrolled
[`NetworkTelemetry`](../observability/README.md#declare-collection):

- Metrics use existing named-port discovery, with collector-assigned network,
  participant and Pod UIDs and `source=actor-native`.
- JSON records travel through container logs to `<prefix>_logs`, with
  `source=container-log` and `event_type=Log`; `BitcoinObservation` is inside `body`.
- Metrics-only collection does **not** retain hashes or branch records. Logs-only
  collection does **not** produce metric tables.

Use the [bounded query examples](../observability/bitcoin-observer-queries.sql) to correlate observations
with recorded action/fault UIDs and public runtime identities. Log timestamps and
in-record sampling timestamps serve different purposes; retain both. Exporter restart
resets its counters and last-success state. Pod replacement produces a new Pod UID;
collection does not infer continuity or actor health from missing samples. Actor shutdown
requires confirmed termination of both Core and its observer container.

See the [qualification record](bitcoin-observer-qualification.md) for exercised paths
and limits.
