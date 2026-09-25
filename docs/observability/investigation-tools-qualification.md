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
complete telemetry, native fault admission or protocol health. The 2026-09-17 run
predates the separate Deployment rollout check, the backend query's five-second
skew window,
explicit cycle selection and kubeconfig support, explicit Stacks JSON output types,
and the distinction between header-walk failure and transaction-summary failure in
ancestry reporting. That run did not qualify those changes live; the 2026-09-23
run below exercises the current preflight and protocol-capture path. The recent
Bitcoin-height row check above used the earlier backend query. Complete snapshots
do not establish atomicity, exhaustive history or a root cause. Native reward-set
unavailability remains a node report.

The gRPC dependency fix was verified in source gates but was not present in that
run's observability image. Its namespace-enrollment rolls did not establish an
image rollout of the fix. No managed-cluster, external-auth-plugin,
large-history or arbitrary-image compatibility is claimed.

## Agent investigation qualification, 2026-09-23

A fresh network on the shared three-node kind/Kubernetes 1.37.0 cluster reached all
six frozen bootstrap gates, then reported `Initialized=True`, `Running=True` and
`Operational=True`. The current network and observability images were built,
loaded and rolled out for the run. Preflight passed against the selected root UID,
current recorder, actor-resource source, observer Deployment rollout, Chaos
enrollment and recent Greptime log and Bitcoin-height rows. All six actor
protocol captures completed after initialization; three earlier Stacks captures
at burn height 210 ended partial with `PoXAnchorBlockRequired`.

Three nested Clarity contracts deployed with exact included TxIDs. A seeded
control included 12/12 calls; a 90-second Bitcoin-peer partition overlapping
load included 24/24; a labeled 60-second retest included 12/12. `run2`, `run8`
and `run16` varied map widths, reads, writes and buffer sizes. Native injection
and recovery were acknowledged. Before/during/after captures found the same
actor Pod UIDs and matching Bitcoin heights across all three peers, with Stacks
progress continuing. This run observed no protocol impairment, but did not measure
peer connections or packet isolation. The third Bitcoin peer, `btc-07`, was
outside the fault selectors and could have bridged the selected peers. One during-fault
snapshot per fault cannot establish whether the pairwise partition affected
peer traffic, so continued progress is also consistent with an ineffective fault.

The first fault omitted the network UID on the *fault object's* metadata, though
its source and target selectors were exact. Chaos Mesh injected it, but the
network-scoped journal had no row for that fault: the recorder intentionally
attributes fault objects by their metadata network-UID label, not by their Pod
selectors. The source-gap count did not increase during this unlabeled-fault
window. With metadata labels present,
Greptime retained `ADDED`, `MODIFIED` and `DELETED` rows for its exact UID. The
namespace's optional Chaos admission profile was initially absent; after it
was installed, server-side dry-run rejected the unlabeled form, admitted the
labeled form, and a further 30-second fault completed with its lifecycle
recorded. These faults were submitted with administrator credentials; the Deny
binding still applied, but the restricted `stacks-chaos-agent` Role was not
exercised. Namespace enrollment and a passing preflight alone do not prove
that the admission profile is installed. The profile was retired after this run;
current experiments use native Chaos Mesh faults without stacks-k8s admission rules.

The final portable bundle traversed seven object/log/metric tables and retained
169,616 rows with no missing context kinds. Every table reported `Exported`,
while coverage remained `Unknown` and the manifest reported 13 sources with 17
gaps in total. Offline checksum verification passed again after stopping and
deleting the network, removing telemetry and the experiment namespace, and
leaving the shared cluster running. Those bounds establish a usable investigation
workflow, not complete event capture, deterministic replay, causal attribution
or a security finding.

## Restricted fault-identity check, 2026-09-23

This optional profile and Role were retired; the result records only that dated run.

A separate disposable namespace exercised the optional Chaos profile with two
synthetically labeled pause Pods. A token-only kubeconfig identified itself as
`system:serviceaccount:restricted-chaos-20260923:stacks-chaos-agent`.
The identity could create and delete `NetworkChaos`, but could not read Secrets,
create or patch Pods, or update a fault. Server dry-run rejected a fault without
the correlation label and admitted a 45-second partition with exact selectors.
The agent identity created and deleted that fault; Chaos Mesh reported
`AllInjected=True` and then `AllRecovered=True`, with successful apply and
recovery records for both Pods. The profile and namespace were removed afterward.

This checks restricted RBAC, admission, and native lifecycle under the selected
identity. The Pods were not network actors, so it does not establish packet
isolation, protocol behavior or journal correlation. An initial probe accidentally
combined a token with the administrator kubeconfig; `auth can-i` exposed the
administrator permissions. That probe is excluded from the restricted-identity
result. The corrected run used a kubeconfig containing only the short-lived
ServiceAccount token. The ignored run-local evidence records both attempts
separately.
