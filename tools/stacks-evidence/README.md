# Stacks evidence tool

Read-only, bounded Greptime queries, separate from workload submission and network
orchestration. Build and pipe exactly one JSON request:

```bash
GOWORK=off go -C tools/stacks-evidence build -o stacks-evidence .
stacks-evidence export < query.json > result.json
```

```json
{
  "endpoint": "http://127.0.0.1:14000",
  "authorization": "Basic <reader-credential>",
  "table": "stacks_<recording>_objects",
  "timeColumn": "timestamp",
  "networkUID": "00000000-0000-0000-0000-000000000000",
  "from": "2026-09-15T00:00:00Z",
  "to": "2026-09-15T01:00:00Z",
  "limit": 1000
}
```

Windows are half-open, limited to 24 hours, with at most 10,000 rows and a 64 MiB
response. `participantUID` is optional. The time column must be `timestamp` or
`greptime_timestamp`. Redirects are refused. Authorization is input-only; protect
input files and use a backend reader credential. A row limit does not bound backend
scan memory. This single-query command does not claim complete evidence coverage.

## Portable evidence bundles

`bundle` exports the recording's object and log tables plus explicitly selected
metric tables. It writes query pages, `context.json` and `manifest.json` into a
**new** directory; it never overwrites an earlier export. No cluster credentials,
Kubernetes client or network mutation permissions are needed.

```bash
stacks-evidence bundle < bundle-request.json > export-summary.json
stacks-evidence verify ./evidence-run
# Archive or move the ordinary directory with your preferred filesystem tools.
```

```json
{
  "endpoint": "http://127.0.0.1:14000",
  "authorization": "Basic <reader-credential>",
  "networkUID": "00000000-0000-0000-0000-000000000000",
  "telemetryUID": "11111111-1111-1111-1111-111111111111",
  "tablePrefix": "stacks_<recording>",
  "from": "2026-09-15T00:00:00Z",
  "to": "2026-09-15T00:10:00Z",
  "outputDir": "./evidence-run",
  "metrics": ["bitcoin_observer_success", "bitcoin_block_height", "up"],
  "pageSize": 1000,
  "windowSeconds": 45,
  "maxRows": 100000,
  "maxBytes": 268435456,
  "maxQueries": 4000,
  "timeoutSeconds": 300
}
```

Read the network UID, recording UID and table prefix from the selected
`NetworkTelemetry`; they are caller-supplied export bindings. All queries filter
network UID; metric queries additionally filter telemetry UID. Recorded objects
carry public root/genesis inputs, participant admission/configuration digests,
requested images, runtime image/container identities, resource observations and
fault/action lifecycles. `context.json` selects the last successfully retained
eligible root, genesis, participant and Pod bodies **within the requested window**.
Choose a window that includes fixture creation when you need initialization context. It does not fetch
private configuration Secrets or chainstate. Missing kinds and omitted/oversized
payloads are reported explicitly. An older context body can remain when a newer
body was omitted; consult the timestamped pages for chronology.

| Bound | Default | Maximum |
| --- | --- | --- |
| Time range | Required | 24 hours, ending no later than invocation |
| Query window | 45 seconds | 300 seconds |
| Page rows | 1,000 | 10,000 |
| Total rows | 100,000 | 1,000,000 |
| Retained query-page bytes | 256 MiB | 1 GiB |
| Queries, including schema reads | 4,000 | 10,000 |
| Total deadline | 300 seconds | 3,600 seconds |
| Selected metric tables | None | 32 |

Context is independently bounded to 4,096 objects and 16 MiB, with 64 KiB per
object; a limit sets `contextLimited`. Manifest/context overhead is additional to
the query-page byte budget. Backend queries retain the server's own memory limits;
smaller query windows may be needed for dense logs. Rows sharing a timestamp are
paged using a total ordering over returned columns; offsets reset per window.
Schema changes, unsupported ordering and unavailable tables produce partial exports.

Exit zero and table state `Exported` mean the requested traversal finished within
its budgets. They **do not mean complete evidence**: queries are independent live
reads, without a database snapshot. Late ingestion, concurrent changes, retention
and historical capture gaps can cause missing or repeated rows during pagination.
Use a settled historical window and export before TTL expiry. `coverage` remains
`Unknown`; explicit `CaptureGap` counts, absent context and partial-source reasons
remain in the manifest. No detected gap is not proof of uninterrupted capture.

Cancellation, row/byte/query limits and query failures return nonzero while retaining
successful pages and a manifest when the filesystem remains writable. Unattempted
sources are listed with their limiting reason. A hard kill or filesystem failure may
leave a directory without a complete manifest; it is not a finished bundle. Start a
new export rather than resuming offsets against a changing database.

`verify` streams checksums for all files listed in the manifest, with paths confined
to the selected directory. It checks byte integrity, not authenticity, coverage or
whether a partial export was sufficient. The manifest is unsigned; changing both
files and their manifest cannot be detected. The export retains the recorder's
redaction boundary and does not guarantee arbitrary actor text contains no secrets.
Keep workload JSONL receipts alongside the bundle; their association with a network
is supplied by the external experimenter, not inferred by this tool.

See the [live qualification](../../docs/observability/evidence-export-qualification.md)
for measured traversal and retained capture gaps.
