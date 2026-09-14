# GreptimeDB/OTel pilot — 2026-09-13

**Recommendation:** use GreptimeDB standalone plus OTel as the candidate M4 data
plane. This pilot qualifies basic ingestion, correlation and bounded restart
behavior. It does not qualify a complete evidence journal or the upstream MCP
server for read-only agent use.

## Boundary

Repository base `b0eac80db302`; no operator runtime, API or dependency files changed.
The existing three-node arm64 `kind-stacks-k8s` cluster ran Kubernetes 1.37.0,
Chaos Mesh 2.8.4 and network release `iteration2`. The fresh `full30` fixture used
the reference five-second Bitcoin cadence, `bitcoin/bitcoin:31.1`,
`stacks-core:iteration2-f9b022-probe` and the installed
`stacks-network-operator:m3-20260913` image. The follower metric listener was enabled
through a public configuration override; its actor Pod rolled normally.

Network namespace: `telemetry-network-20260913a`.
Network UID: `304a6d77-6cc3-46e5-9ace-3573d548e3a5`.
Telemetry namespace: `telemetry-pilot`, with an independent 10 GiB PVC.
The pilot installation manifests are superseded by the [supported installation](README.md).
The original rendered artifacts remain part of the local pilot evidence.

Runtime-reported telemetry image digests:

| Image | Digest |
| --- | --- |
| GreptimeDB 1.2.0 | `sha256:d68f6e3f159e2eb1c0c1a38aabe4ac50dbee2cf2c86746657f4a411e9a3cf6c9` |
| OTel Collector Contrib 0.160.0 | `sha256:799dc6cf12c96192af37b5bdba804da8c10b3bc563b43cb90c3f3c58d9572ad6` |

## Results

| Check | Observed result |
| --- | --- |
| Network lifecycle | Public live suite passed in 1,917 seconds: fresh bootstrap through PoX-5, native progress, both faults, recovery, pause/resume, Stop, root deletion and namespace deletion. |
| Logs after network deletion | 379,796 records remained queryable before the separate harness import. Counts include synthetic probes; they are not exclusively actor logs. |
| Native metrics | All six signers produced identity-tagged height samples. The instrumented follower produced 72 burn-height samples spanning 210–336. Other Stacks nodes and Bitcoin native Prometheus exports were not qualified. |
| Lifecycle history | 21,754 public-object observations remained queryable, with 112,670,099 bytes of JSON bodies. Root deletion and both fault UIDs survived namespace deletion. Polls and status changes repeat facts; this count is not a count of unique state transitions. |
| Correlation | SQL joined follower metrics to retained participant/Pod/image identities. Post-deletion queries retrieved native metric identities, root deletion and fault records. |
| Duplicate timestamps | Twenty distinct synthetic records at one timestamp were all retained: 20 rows, 20 distinct bodies, one timestamp. |
| Initial restart/burst | Three node-local probes each emitted 6,000 uniquely numbered lines with 256-byte padding. After database and collector restart: 18,000 rows and 18,000 distinct bodies. |
| Final restart/burst | Repeated with final collectors and native telemetry active. Database scaled down at 18:46:59 UTC, restored after a 20-second hold; all collectors rolled by 18:47:43. All 18,000 distinct records remained. These were graceful restarts, not power-loss tests. |
| Permissions | Observer denied Secret reads and participant patches. Database reader could query HTTP SQL; SQL table creation and MCP pipeline creation were denied by the database. |
| Retention | One-day TTL accepted on logs, object records and the physical metrics table. Elapsed TTL expiry and disk-full behavior were not exercised. |

The delay fault `75c606b1-61e9-477d-b473-4ad6526b068d` produced a median follower
HTTP round trip of approximately **7 → 1,508 → 7 ms** before/during/after the
configured 500 ms delay. This is application round-trip evidence, not a one-way
packet measurement. The partition fault `4182e818-e300-493b-94cf-52ac7e8b82b3`
blocked probes in both directions. Control observations and Bitcoin production
continued during both holds. The harness required new canonical Stacks/traffic
progress after each cleanup. GreptimeDB retained one `ADDED`, fourteen `MODIFIED`
and one `DELETED` observation for each fault.

## Resource measurements

120 `kubectl top` samples, approximately 15 seconds apart, covered
18:27:10–18:57:10 UTC. These are sampled usage values on a local development
cluster, not instantaneous peaks or a capacity benchmark.

| Measurement | Sampled maximum |
| --- | --- |
| Database memory / CPU | 310 MiB / 177 millicores |
| One node collector memory / CPU | 118 MiB / 40 millicores |
| Object collector memory / CPU | 73 MiB / 22 millicores |
| Simultaneous total telemetry memory / CPU | 611 MiB / 255 millicores |

Allocated database disk usage at export was 93,352 KiB (approximately 91 MiB),
including its WAL and metadata. This is not a fully compacted size or a daily
retention estimate. The pilot did not establish minimum safe memory limits.

## Findings and follow-ups

1. **Keep read-only MCP integration open.** Released `greptimedb-mcp-server==0.5.2`
   connects through a MySQL client that executes `SET @@session.autocommit` while
   opening the connection. GreptimeDB rejects that operation for the read-only
   account, preventing even `SELECT`. HTTP SQL with the same account works.
   Prefer an upstream HTTP-query path or a compatible session initialization;
   do not give an investigation client write credentials to bypass this failure.
   The MCP SQL error was returned as text with `isError=false`, so that flag alone
   is not a success check. Administrative tools remain advertised despite the
   SQL write gate; database-side authorization is required.
2. **Report watch gaps explicitly.** During a stopped object collector, a harmless
   root annotation was added and removed. Both API writes succeeded, but its
   intermediate value was absent from retained history after restart. This
   receiver configuration has no durable watch position; its queue is in memory.
   A product integration needs source-health/gap records and restart/relist
   semantics. Watches still cannot promise every intermediate state.
3. **Bound queries and design indexed identity fields.** A whole-run body scan hit
   the configured 128 MiB scan limit. The same sentinel count over its two-minute
   window succeeded and returned all 18,000 distinct records. `LIMIT` is not a
   scan budget. Generic JSON identity attributes suffice for this pilot; longer
   histories need deliberate indexed columns and bounded query/export patterns.
4. **Enable supported actor metrics systematically.** The [M4 task](../design/roadmap.md#m4-passive-journal-and-initial-telemetry)
   covers supported actor profiles, verified environment overrides or structured
   TOML rendering, complete manually supplied configs and discoverable endpoints.
5. **Finish collection policy before product adoption.** Define pre-storage
   redaction, identity trust, byte limits, source outages and optional sources.
   Native distributed tracing, all-actor metrics, abrupt node loss, prolonged
   backend outage, log rotation and a long soak remain unqualified. No operator
   or network should require telemetry to function.

## Evidence and cleanup

Local evidence is under `/tmp/stacks-greptime-pilot/`; `evidence-sha256.json` binds
the retained files. Key artifacts:

- `network-live.log` and `network/events.jsonl`: live assertions and ordered stages.
- `after-deletion-export.json`: bounded SQL results after namespace deletion.
- `restart-final-operations.json`, `final-restart-burst-windowed.json` and
  `timestamp-result.json`: operation times and independent sentinel counts.
- `object-gap-operations.json`, `object-gap-result.json`, `mcp-smoke.json` and
  `mysql-probe.log`: negative checks and MCP connection failure.
- `resource-summary.json`, `resources.jsonl`, `disk-final.txt` and `pvc-final.json`:
  sampled usage and storage identity.

The 62 harness events were also imported **after** the run as a separately marked
source and counted through SQL. This was not continuous collection. Their exact
original `at` strings remain in the event bodies; the temporary importer used
Python datetime/float conversion for the OTLP index timestamp, so that index is
not a nanosecond-exact copy. Original harness files were unchanged.

`make verify`, `make vuln`, `make docker-check`, upstream Helm lint and the
documented SQL shapes passed. Final documentation also received Markdown and
relative-link checks. Repository vulnerability/Dockerfile checks do not audit
the upstream database, collector images or temporary Python dependency graph.

The test network, both pilot namespaces, database PVC, telemetry Helm releases,
namespace-specific Chaos admission objects and pilot checkpoint files were removed.
Local tunnels and generated credentials were removed. The original three-node
cluster, network operator, Chaos Mesh, Headlamp and Metrics Server remain running.
