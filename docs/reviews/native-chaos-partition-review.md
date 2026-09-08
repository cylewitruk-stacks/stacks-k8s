# Native actor partition review ledger

## Boundary

Base HEAD: `6c5632767005b741d95a7ba1638fa9422ec14eb3`.
Review the complete working-tree diff and untracked delivery files. Nothing is
staged or committed. Operator runtime source, APIs, generated CRDs, dependency
graphs and permission grants are unchanged. The profile's shared resource names
and enrollment value change with chart 0.2.0; upgrade requires drained faults.

## Focused Fable follow-up

This follow-up remains part of the same unstaged delivery. No operator runtime,
API, generated CRD, permission or dependency changes were added.

| Review item | Revision | Verification / boundary |
| --- | --- | --- |
| Stale delay-only descriptions | Update the design decision register and chart description to include partition. | Markdown and chart checks. |
| Worker build context | Keep action `go.mod`/`go.sum`; remove the full action-source copy. | Actual worker build succeeded; the rebuilt image runs the fresh Stacks fixture. |
| Reverse-direction evidence | Share one mirrored RPC check before/during/after both Bitcoin cases. | Fresh namespace live assertions in both directions; round-trip reachability is not independent packet-direction proof. |
| Reused Stacks baseline | Capture the initial confirmation counter and require a new confirmation before the first fault. | Fresh fixture passed; retained paused fixture timed out before fault creation. The original stalled fixture was not changed. |
| Reward-cycle context | Shared bounded read-only queries collect Core height and each node's projected PoX context at injection, native cleanup, protocol recovery and wait timeout. | Fresh live snapshots include prepare/reward boundaries; decoder tests cover boundaries, missing/malformed fields and discarded unrelated fields. Missing observations are gaps, not zero values or test successes. |
| Preflight attribution | State that `Unavailable` combines admission and preflight refusal; the status message cannot distinguish them. | Code inspection confirms both paths call the same skip helper. No ineffective message assertion or runtime change. |
| In-flight timing and replacement | Document the 20 s injection bound versus 15 s receipt hold, and the old-Pod selection. | Slow injection fails existing assertions; replacement checks prove retained uncertainty, not continued fault injection into that Pod. Control live suite was not rerun for comment/doc-only changes. |
| Environment lifecycle | Reuse the current cluster, provision independent fresh namespaces, preserve failed fixtures. | Guidance added to AGENTS and operations; no new cluster and no changes to the retained stalled network. |

The telemetry samples are sequential and timestamped. `phaseFromReportedBounds`
classifies a node's reported height against that same response's boundaries; it
is not an assertion about current Core/consensus phase. Negative prepare
countdowns are retained. No phase is avoided to obtain a passing result.

Follow-up logs use `/tmp/stacks-partition-followup-*`. The worker build ID is
`sha256:943dada0cbc0e410be36b50a8b6fc11ee2fce2ae4a6ac21b996e9f572a755e96`.
The fresh Stacks run passed; the retained paused fixture deliberately failed the
new baseline gate and emitted a timeout snapshot (Core 406, both nodes 360).
These results do not resolve or supersede the original post-expiry stall.

Follow-up verification completed on 2026-09-07:

- Full `make verify` passed with a populated temporary index and no generated drift.
- `go test -race -count=1 ./internal/integration` and the same command with
  `-tags=integration` passed in `tools/chart-policy` (decoder and all three profiles).
- `make vuln`, `make docker-check`, and the actual worker image build passed.
- Final mirrored Bitcoin live suite passed; fresh Stacks live suite passed.
- Retained-fixture baseline check returned the expected exit 1 before injection;
  timeout diagnostics were captured. It is not counted as a recovery pass.
- Markdown, relative links/anchors, formatting and whitespace checks passed.
- No live control-loss or delay rerun: their behavior did not change in this
  follow-up. Earlier evidence remains separately dated above/below.

## Delivery ledger

| Area | Change | Evidence / limit |
| --- | --- | --- |
| Capability | Independently enabled native bidirectional partition beside directed delay; no wrapper/controller. | Delay-only, partition-only and combined Helm/API tests. Both disabled by default; external version 2.8.4 required. |
| Admission | Same one-actor-per-side, same-network/namespace, 1–120 s duration, immutable spec and lifecycle guards. Partition rejects delay parameters and non-bidirectional modes. | Common real API-server matrix runs under all three enablement combinations; legacy cleanup, de-enrollment and restricted-identity create/delete retained. |
| Composition | One shared six-resource profile and existing-object quota. | Exact RBAC/binding/quota checks; a delay request is denied by real quota during an existing partition. No expanded agent permissions. |
| Bitcoin effect | Both isolated peers receive producer requests and form distinct chains; stable unequal work reconverges after native cleanup. | Live cancellation and expiry, actor RPC failure/recovery, both receipt counters, exact heavier tip and resumed production. Pause/drain avoids comparing moving tips. |
| Stacks effect | Selected miner loses burn-chain progress while unselected signer-node and Bitcoin production advance. | Initial productive PoX-4/transfer fixture passed cancellation/expiry and new inclusion. A repeat expiry case stalled at a reward-cycle transition after native cleanup; retained as failed qualification, not hidden. |
| Control loss | Administrator-only producer path interruption; never part of public agent profile. | Public admission rejects producer selectors. Fresh real-Core receipt-delay fixture proves no preflight mutation, actual in-flight drain exhaustion, producer replacement, retained Armed/Blocked dispatch and no extra Core blocks. |
| Identity | Explicit cluster/namespace opt-in; logged native and actor UIDs; actor container/restart checks. | Native selectors themselves remain label-bound. No passive journal or automatic identity-divergence service is claimed. |
| Delay regression | Existing suite accepts an explicit alternate namespace. | Live delay/cancellation/expiry/controller-replacement measurements pass under the combined profile. |
| Packaging repair | Worker Dockerfile copies only the already-required local action module manifests before download/build. Root `docker-build` now builds this worker, so the existing CI container job exercises it. | Actual worker/network builds and live productive deployment; no new JavaScript, dependencies or runtime logic. |
| Planning/docs | Update current profile scope and operations; record platform and actual outcomes. | M0.5 remains in progress; other kinds/platforms, dynamic admission and passive correlation stay open. |

## Qualification and test iterations

[The qualification record](../chaos/partition-qualification.md) records image
identities, resource UIDs, measured effects, and recovery boundaries.
Test-local logs use `/tmp/stacks-partition-*.log`. Provisioning manifests are
private files containing disposable credentials; do not paste them into review
output. Tests use observer credentials through stdin and read-only administrator
API proxy access, neither of which is granted to the experiment agent.

The control harness initially expected `Blocked` for unavailable preflight;
current scheduling records `lastSkipReason=Unavailable` before any authorization.
A second iteration waited only 15 s after a replacement became Running, missing
leader-lease acquisition. Both assertions were corrected, and the complete test
passed in a new credential-isolated namespace. These were harness corrections;
no controller recovery logic changed. The test image holds one real receipt for
15 s to establish the in-flight window and is not a production execution adapter.

The replacement test verifies native finalized deletion and local Core observer
RPC after removal. It does not directly probe the replacement producer's RPC
socket path. Missing receipt knowledge stays unresolved regardless of network
recovery; the test does not authorize in-place readmission.

## Checks

- Full `make verify`: passed with a populated temporary index; no generated drift.
- Chart-policy admission matrix under `-race`: passed for all three profiles.
- `make vuln`, `make docker-check`: passed; actual network/worker/receipt-fixture builds passed.
- Native Bitcoin partition and delay suites: passed, including cancellation/expiry
  and delay controller replacement.
- Administrator-only control-loss suite: passed, with observed drain exhaustion and retained uncertainty.
- Stacks suite: initial cancellation/expiry passed; repeat cancellation passed but
  expiry recovery failed. The strict assertion and recorded failure remain.
- Markdown lint, relative links/anchors and whitespace checks: passed for all changed/new
  Markdown files. External links were not re-fetched.

The failed repeat is a Stacks/fixture liveness observation, not a confirmed
upstream vulnerability or root-cause finding. Native cleanup completed while
Stacks progress did not. The retained paused fixture and read-only evidence
support a separate follow-up; the profile does not promise universal protocol
recovery.

## Retained state and cleanup

Current fixtures are paused with no native faults; the Stacks liveness failure
and control-3 unresolved receipt state are preserved. The two superseded
control namespaces were administratively removed. Direct namespace deletion
removed their colocated controllers before finalizers drained; after confirming
no Pods or native faults remained, their ledger outcomes were archived as
`Abandoned` and residual finalizers cleared. This was removal only, not recovery
or namespace reuse. The passing control-3 test involved no status/finalizer edit.
Future teardown follows network → retained ledgers → namespace/operator ordering.
The user independently removed older demo clusters. Follow-up qualification
reuses the retained cluster; successful follow-up namespaces are removed in
network/ledger/operator order after recording results.

## Remaining scope

TimeChaos and other native kinds, other platforms/CNIs, daemon/node loss,
actor replacement during injection, dynamic inventory admission, and passive
journal/divergence/gap correlation remain open. New live tests use administrator
measurement authority in disposable fixtures; they do not expand the public
agent Role. Duration requires functioning upstream cleanup infrastructure.
