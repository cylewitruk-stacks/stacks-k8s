# Investigation tools qualification

## Scope

The 2026-09-17 qualification exercised
[`stacks-inspect`](../../tools/stacks-inspect/README.md) and
[`stacks-preflight`](../../tools/stacks-preflight/README.md) against a fresh network
on three-node kind/Kubernetes 1.37.0, Bitcoin Core 31.1, Stacks node/signer 4.0.3,
Chaos Mesh 2.8.4 and the local GreptimeDB telemetry profile.
The cohort had three Bitcoin nodes, one Stacks miner and two follower/signer pairs.
Protocol bootstrap used accelerated Bitcoin production; subsequent baseline
production used 60 seconds. Operator images were recorded at one mid-run
observation (12:44:21 UTC), not at both run boundaries; unchanged images throughout
the run are not established.

## Verified behavior

| Check | Evidence |
| --- | --- |
| Positive preflight | Exact root UID, current recording, recorder heartbeat, actor-resource source, observer watch scope, namespace enrollment and recent network-scoped logs/Bitcoin-height rows passed. |
| Negative preflight | A wrong root UID produced nonzero exit and failed identity checks. |
| Protocol capture | All six node endpoints produced complete bounded captures after initialization. Stacks PoX/reward-set reads used the captured index block ID; Bitcoin walks included headers and transaction summaries. |
| Bitcoin ancestry | An explicitly selected earlier block was reported as an ancestor within the requested walk bound. |
| Partial capture | A nonexistent Bitcoin hash produced nonzero exit, retained its requested hash in the failed record and ended with `complete=false`. |
| Fault-time capture | A Pod-specific forward during an acknowledged native Bitcoin-peer outage produced a sanitized transport failure and `complete=false`; no retry or recovery action was performed by the tool. |
| Static verification | `make verify`, `make vuln` and `make docker-check` passed. Focused race tests cover bounds, deadlines, output errors, redaction, failed-read subjects, ancestry, scope parsing and stale recording status. |

## Limits

The caller recorded participant, Pod and container identities before and after
Pod-specific forwards. The tool's attribution string alone does not establish that
binding. Live Bitcoin reads used the existing actor RPC credential; the tool issued
only its three documented read methods, but that credential is not restricted to
those methods. No RPC allowlist was widened.

Preflight establishes selected prerequisites at observation time. It does not prove
complete telemetry, native fault admission or protocol health. This run predates
the separate Deployment rollout check, the backend query's five-second skew window,
explicit cycle selection and kubeconfig support, explicit Stacks JSON output types,
and the distinction between header-walk failure and transaction-summary failure in
ancestry reporting. Those changes have automated regression coverage, not repeated
live actor qualification. The recent Bitcoin-height row check above used the earlier
backend query. Complete snapshots do not establish atomicity, exhaustive history or a
root cause. Native reward-set unavailability remains a node report.

The gRPC dependency fix was verified in source gates. The existing observability
image was not rebuilt with that fix. Namespace enrollment and withdrawal rolled
the Deployment; those rolls do not establish deployment of the source fix, which
requires a separately verified image rollout. No managed-cluster, external-auth-plugin,
large-history or arbitrary-image compatibility is claimed.
