# GreptimeDB telemetry qualification — 2026-09-13

## Profile

Shared `kind-stacks-k8s`, three arm64 Kubernetes 1.37.0 nodes. GreptimeDB 1.2.0
(upstream standalone chart 0.4.14), OTel Collector Contrib 0.160.0, Chaos Mesh 2.8.4.
The observation image is `stacks-observability-operator:telemetry-20260913g`;
the network operator is `stacks-network-operator:telemetry-20260913`.
Actor images: Bitcoin Core `bitcoin/bitcoin:31.1` and
`stacks-core:iteration2-f9b022-probe` for nodes/signers.

Raw local evidence: `/tmp/stacks-telemetry-delivery/`. These files are not a
portable signed evidence export. Credentials are excluded from committed artifacts.

## Executed checks

| Check | Result |
| --- | --- |
| API-server lifecycle and SSA | Wrong UID refused; foreign resources not adopted; storage/identity immutable; root deletion preserves recording; independent status writers coexist. |
| Recorder RBAC | Public reads and exact status write allowed; Secret reads and network mutation denied. |
| Native metrics | Fourteen Stacks node and six signer identities present in the first full-cohort recording. Bitcoin nodes have no native metric claim. |
| Backend outage | Bitcoin advanced 163→168 during a 25-second offline hold. Recorder reported backend unavailable, then recovered with retained capture gaps. |
| Append semantics | Real-backend Go test under race detection retained two records with identical timestamps/tags; zero credential-canary leaks. |
| Log collector | Public canary and `[REDACTED]` each retained once; foreign-network log excluded. |
| Metric collector | Forged source identity remains under `exported_*`; authoritative UID labels match the Pod. Supplied year-2100 timestamp is replaced by scrape time; credential-named label is absent. |
| Retention configuration | Live table DDL confirms log/object TTL and physical metric-table TTL. Metric TTL requires the administrator initializer; ingestion hints alone do not set it. |

## Network runs

`telemetry-full-20260913` initialized a full cohort, recorded 252,864 logs and
native metrics, then stopped at missing namespace Chaos enrollment. Its recording
started after initialization began; early transitions are not claimed. Data remained
queryable after namespace deletion.

`telemetry-qualified-20260913` also initialized. It included the backend-outage
exercise and recorder rollout. The fault gate stopped because the external-version
requirement had prevented the Chaos profile installation. No injected-fault or
recovery success is claimed for these two runs.

The final run, `telemetry-final-20260913`, uses network UID
`8beb3937-5cb2-41da-947f-9dc814c87aa6`, telemetry UID
`26ad0b7e-a1bc-4e97-aa45-b8f8a3cfc875`, and tables prefixed
`stacks_95112cfbad440005e74e0069167b7b62`. Its native profile was rendered with the
required version, installed, and read back as compiled without CEL warnings before
the fault gate. The full-cohort harness uses a five-second Bitcoin cadence.

The final lifecycle passed in 1,900 seconds: initialization, native progress,
delay expiry, partition cancellation, fresh canonical progress after each fault,
pause/resume, confirmed stop, root deletion and namespace cleanup.

| Fault | UID | Injected | Native cleanup | Fresh canonical recovery |
| --- | --- | --- | --- | --- |
| Delay | `d43928c9-92d8-40fc-8a7b-0fa7ab8054c2` | 21:32:36 UTC | 21:34:06 UTC | 21:34:17 UTC |
| Partition | `fdbcbfca-1667-4b6b-81fa-be42fb6209d0` | 21:34:19 UTC | 21:34:48 UTC | 21:35:01 UTC |

After namespace deletion, queries returned 352,577 actor logs, 33 native fault
observations with both UIDs' `ADDED`/`MODIFIED`/`DELETED` events, and the root deletion
event. Fourteen node and six signer identities were verified in native metric
series. The seven SQL templates were accepted by the backend, but the selected window
returned zero rows for the three metric queries; that check established syntax
acceptance only. The correction qualification below supplies behavioral assertions. The
harness establishes protocol recovery separately from collector readiness.

The final verification snapshot passed `make verify` with no generated drift;
`make vuln`, `make docker-check`, focused race/envtest checks and the live backend
test passed. The review ledger identifies logs and snapshot boundaries. These are
Go dependency and container contract/build checks, not an upstream image CVE audit.

## Correction qualification — 2026-09-14

Evidence: `/tmp/stacks-telemetry-corrections/`. The same three-node cluster used
`stacks-network-operator:telemetry-fix-20260914` and the final observation image
`stacks-observability-operator:telemetry-fix-20260914b`; backend and collector pins
were unchanged. The disposable namespace was `telemetry-fix-20260914`.

Network UID: `8e582afa-d1c0-40b0-8010-22cdf98bd874`; telemetry UID:
`3690f735-2c4d-4aa2-a948-3ad335a3ce7a`; table prefix:
`stacks_b548637e35104e2975723102ef1754fd`.

| Check | Executed result |
| --- | --- |
| Source transitions | Objects-only: zero collectors. Metrics-only: three non-root collectors, no hostPath volumes. Combined: three collectors with log collection restored. Both storage directions passed after correcting an SSA volume-union conflict found during the first attempt. |
| Scrape health | A synthetic selected exporter returned 503 for 35 seconds, then recovered. The maintained query asserted both `up=0` and `up=1` for its exact participant UID. A separate pinned-collector probe confirmed target relabeling preserves identity on `up`. |
| Ingestion canaries | Authoritative identity replaced forged labels; the supplied future timestamp was ignored. The sampled log window contained 78 records, 39 redacted lines and zero credential-canary leaks. |
| Fault observations | A 20-second native delay targeted only the synthetic exporter Pod. The fault's observations remained queryable; this was not a consensus recovery test. |
| Maintained queries | All eight SQL templates returned rows and passed assertions before and after root deletion, and again after namespace deletion. The deduplication query returned nonempty resource versions. |
| Admission after deletion | After stopping and deleting the root, changing the recording to objects-only produced generation 10 and a fresh recorder Pod while preserving `admitted=true`. |
| Root deletion evidence | A separate exact-UID query returned the retained root `DELETED` observation. |

The final query window was 2026-09-13 23:41:45–23:47:24 UTC. Queries 1–8 returned
200, 11, 45, 1, 1, 200, 29 and 200 rows respectively; query limits mean these are
result sizes, not total event counts. `query-after-namespace.log` records the
assertions. `transitions.log`, `canary-results.json` and `post-root-rollout.json`
record the other checks.

The fixture used the full-cohort declarations to exercise resource collection but
was stopped while initializing, before Bitcoin preparation completed. This pass
makes no new PoX or protocol-recovery claim; the earlier full-cohort qualification
above remains separate.

Snapshot `be26c4b554c62dfdc3cf92bd2dad0060ee109800` passed isolated `make verify`
with `EXIT_CODE=0` and `DRIFT_EXIT_CODE=0` in `verify-final.log`. `make vuln` and
`make docker-check` passed; the latter also validated all seven nonempty source
combinations with the pinned collector binary.

The namespace, fault and exact-UID checkpoint directories were removed. Experiment
enrollment was withdrawn; the backend PVC retains the data subject to TTL. The
shared cluster and updated operator installations remain running, with no test
network or recording retained.

A subsequent cleanup narrows the storage-transition patch to volumes, matching
container mounts and the required Pod user/group settings. API-server regressions
under race detection verify both directions preserve unrelated Pod fields and defaults.
The deployed image and live results above predate this narrowing; no additional
live rollout or protocol qualification was performed for it.

## Actor-resource and scrape qualification — 2026-09-15

The shared three-node cluster ran
`stacks-observability-operator:followup-final-20260915` for network UID
`3aeea8a6-4adf-4454-84fa-65f76aa1a854` and telemetry UID
`f2d502b9-424c-4382-a2e4-9a4e6f9d1662`. The recorder's PodMetrics Role contained
only namespace-scoped `list`; the backend requested 1 GiB, was limited to 3 GiB
and rendered separate 1 GB shared query-execution and scan pools.

The first 10,000 retained object rows contained 371 `ContainerResource` records
after namespace deletion. Each record carried exact participant and Pod UIDs plus
container CPU, memory, Metrics Server timestamp and window. This bounded result
proves collection, not total sample cardinality.

The shared `up` table contained 25–32 current-recording samples for each of three
Stacks nodes and two signers. Their mean timestamp spacing was exactly three
seconds. Every actor reached `up=1`; one signer node retained its startup `0→1`
transition. Native scrape availability remains separate from protocol progress.

An earlier 138-second live probe crossed the API server's normal watch timeout
without increasing the Event source's initial gap count. The final lifecycle later
recorded a second Event gap when the requested resource history was unavailable,
then continued from a fresh snapshot. Unit tests distinguish normal closure from
expired history. Evidence is under `/tmp/stacks-followup-20260915/` and
`/tmp/stacks-followup-qualified-20260915/`.

## Native fault-source follow-up — 2026-09-15

The adversarial recording for network UID
`fe2e4cc2-5597-4f22-883e-8f672f7845e1` used a recorder image with the shared
NetworkChaos, PodChaos and StressChaos source allowlist. Its namespace Role contained
only those three Chaos Mesh resources. No-target probes with PodChaos UID
`d48ab3ea-3402-4af7-92eb-0548476c2d40` and StressChaos UID
`9ff0acd7-f4c6-47e9-8173-ed6f4214b316` produced exact `ADDED` and `MODIFIED` rows in
Greptime without selecting an actor. This proves source/RBAC/ingestion coverage, not
fault injection or protocol recovery. The related real fault experiment is documented
in [the experiment report](../experiments/bitcoin-split-anchor-stall.md).

## Initial delivery cleanup

All qualification namespaces, native admission objects and twelve UID-specific
collector checkpoint directories were removed. No network or recording remains.
The existing kind cluster is running; Greptime and the observation operator remain
installed in `stacks-observation-system`, with no experiment namespaces enrolled.
The backend PVC retains the queried evidence until TTL expiry. The updated network
operator remains installed. No failed fixture was retained.

## Limits

- Readiness, source availability and protocol recovery are separate observations.
- Watches/re-lists, recorder restarts, log rotation and finite queues can lose or
  duplicate facts; gap markers do not quantify exact loss.
- TTL expiration/physical reclamation, full-disk behavior, long outages, private
  registries, other architectures and alternate actor images were not qualified.
- The collector timestamp/identity canary is a separate synthetic exporter, not a
  modified consensus actor. It tests ingestion provenance, not protocol behavior.
- M4 remains open for broader source/lifecycle coverage, audit and export contracts.
