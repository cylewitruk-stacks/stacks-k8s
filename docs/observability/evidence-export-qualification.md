# Evidence export qualification

On 2026-09-15, `stacks-evidence bundle` exported the historical 07:10–07:34 UTC
window from the shared local Greptime backend. The source was the
[Bitcoin observer qualification](../network-operator/bitcoin-observer-qualification.md#fast-bootstrap-and-stable-diagnostics--2026-09-15):
network UID `9ae6e338-0809-4bb0-a77b-145cc07e60d2`, recording UID
`f8d33fe2-87f0-4f39-a513-e105f80d5785` and table prefix
`stacks_ba4842f8e7c5200c47b9f1f5d2438e45`.

| Source | Exported rows |
| --- | --- |
| Objects and lifecycle records | 12,675 |
| Actor logs | 83,377 |
| `bitcoin_block_height` | 891 |
| `bitcoin_observer_success` | 899 |
| `up` | 2,846 |

The export used 45-second windows, 1,000-row pages, a 200,000-row allowance and
256 MiB page-byte budget. It completed in 236 queries, retaining 100,688 rows in
224 pages (179,340,065 bytes). The context contained one root, one genesis,
14 participant and 25 Pod bodies. No payload omission or context limit was reported.
The manifest recorded 14 capture-gap rows; `coverage` correctly remained `Unknown`.
`Exported` establishes successful bounded traversal, not complete historical capture.

The directory `/tmp/stacks-slice3-evidence-final` contains the pages, context and
manifest; `/tmp/stacks-slice3-live-final-summary.json` is the command's summary.
These are local working artifacts, not a durable published dataset. Credentials
were supplied through stdin from a reader account and were not written into the
request, manifest or command log. The raw pages retain the recorder's existing
redaction boundary.

The exporter only queried existing tables. It did not deploy workloads, write
phase markers or drive faults. Unit regressions cover equal-timestamp pagination,
foreign-row rejection, unavailable schemas/backends, row/byte/query bounds,
cancellation, failed page writes, serialized context limits and offline checksum
verification. Live evidence covers the selected tables and schema above; it does
not qualify arbitrary metrics, database-snapshot consistency or unrestricted SQL.

After export, the root reached `Stopped`; the root and its participants were
removed before the recording and namespace. Offline `verify` succeeded again
after namespace deletion. This checks portability without requiring the original
Kubernetes objects or contacting Greptime. The shared cluster remains running.
