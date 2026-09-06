# Bitcoin actions consolidated review ledger

This supersedes the separate generation/reorganization review ledgers and prompts.
The user staged both reviewed implementations together; this follow-up fixes
Fable's findings on that combined base. No commit has been created.

- Underlying commit: `f788749b638873bc270aa93ebe88172fea707f08`.
- Combined staged base tree: `ea0f6c3163810b63a270afe3368ec9e8d98b7c6f`.
- Current staged base, including the first reviewed corrections:
  `10ba02488746ecd8e812fcb8d2ce012baaaf2578`.
- Current follow-up: ordinary working-tree diff, captured completely in
  `/tmp/bitcoin-actions-read-wait.patch`. The earlier correction patch remains
  `/tmp/bitcoin-actions-followup.patch`.
- Earlier generation review tree: `b605233f92e099e91f7aa45e4bc5c2402ed23267`.

## Findings and corrections

| Review item | Correction | Regression evidence |
| --- | --- | --- |
| 1: finalized deletion advances generation | Both executors retain identity by UID and immutable spec; admission generation remains evidence. Idle cancellation records a stop and waits for terminal acknowledgement. | Real API-server deletion before dispatch and after recorded receipts for both kinds, including finalizer removal; unit cancellation after invalidation and between generation receipts verifies exactly one compensation and no further generation. |
| 2a: deadline after completed replacement | Only incomplete replacement work is stopped by action expiry; acknowledged cleanup is exempt from cleanup-authorization expiry. | Final replacement receipts, cleanup receipt, and final verification delayed beyond action/cleanup horizons; successful completion and exactly one cleanup. |
| 2b: runtime changes after cleanup | Acknowledged cleanup cannot become unsafe. A changed identity or admission-read failure beyond the existing horizon makes final verification inconclusive while CleanupComplete remains true; release still requires acknowledgement. | Changed container before expiry and unavailable Pod past the authorization horizon; no repeated cleanup or retained reservation after acknowledgement. |
| Post-cleanup transient read | Admission-read failures wait as Reserved until expiry+30s; a successful changed identity stops immediately. No mutation is authorized by this wait. | Injected API Pod read failure recovers at the horizon or remains inconclusive there; cleanup and terminal acknowledgement are preserved, with no repeated mutations. |
| Busy/no-effect status | Both action kinds report a held reservation as TargetBusy. Never-armed reorganization stays Admitted while waiting for executor withdrawal. | Direct lifecycle assertions for both states. |
| Transient preflight | Finite generation waits after read-only preflight errors within its existing deadline. Only successful negative regtest/address validation is classified as definite rejection. | Real HTTP classification; controller timeout/recovery/expiry and definite rejection. Existing lost-mutation-response tests remain unchanged. |
| Repeated forks | Guide explains that exposing an older winning fork stops and compensates, rather than generating on an unintended branch. | Documentation refinement; existing exact-tip checks enforce this behavior. |
| Duplicate delayed-arm fixtures | One shared fixture and one test covering both action kinds replace duplicated generation/combined coverage. | Both delayed arm CAS conflicts remain exercised under race testing. |
| Pre-existing envtest conflict | Production-network test edits refresh and reapply only their intended spec mutation on conflict. | Integration suite; assertions and invalid-admission tests are preserved. |

The changes keep the existing controllers, ledger, collector, phases, permissions,
and fixed RPC surface. There are no new dependencies, JavaScript modules,
credentials, schemas beyond generated field documentation, or recovery machinery.

## Follow-up verification

The latest post-cleanup read-wait and busy-status correction passed `make verify`,
focused production race tests, Markdown lint and documentation-contract checks.
The tests cover recovery and exhaustion at the exact horizon, an actual identity
change before expiry, and a clock crossing that must not relabel known cleanup.
No live environment, dependency, container or mutation permission changed.

`make verify` passed, including module/vet/unit/race checks, envtest, chart RBAC,
workload/Helm validation, and generated-file drift. Focused production race tests
passed, including a three-run repetition; final preflight and cancellation
regressions also passed under the race detector. Markdown, documentation-contract
and Git whitespace checks passed. Envtest uses Kubernetes 1.36 and confirms generation
1 to 2 on finalized deletion. The cancellation integration tests manually
schedule the real controllers against the API server with seeded acknowledged
ledger facts; they do not simulate Bitcoin RPC execution. Unit branch tests
exercise actual executor transitions and compensation with the typed test RPC.

Three additional envtest runs passed in separate processes: controller-runtime retains controller
names globally, so an attempted in-process `-count=3` failed name registration on
its second and third runs. No name validation or test assertion was disabled.
The dependency/container checks were not rerun for this follow-up because their
inputs did not change; their prior results are retained below.

The real staged index remains the user's reviewed base, including the first
corrections. Fable independently approved those corrections with one low-severity
post-cleanup admission-read finding, addressed by the current follow-up. Full verification
uses a temporary index containing the complete working snapshot for generated
artifact comparisons; it does not restage the user's patch.

## Prior implementation qualification

Both original deliveries passed `make verify`, `make vuln`, `make docker-check`,
focused race/schema/chart checks, Markdown lint and Git whitespace checks.
Reorganization also passed the receipt-fixture Docker build check. Fable independently
checked focused packages, generated drift, envtest, charts, supply-chain checks,
and read-only demo evidence; its reported envtest conflict motivated the test fix.
Those earlier checks do not substitute for follow-up verification above.

Live tests used actual Core 31.1, Linux/arm64, kind 0.32.0, Kubernetes 1.36.1,
and one node. They predate this follow-up; no new live mutation run is claimed.

| Earlier suite | Observed evidence |
| --- | --- |
| Generation quota | 64 actions admitted; 65th rejected by actual quota. |
| Finite generation | Three receipts separate from baseline count; unrelated failed Stacks actor; latest pause/cadence; idle executor restart; baseline resumption. |
| Delayed generation receipt | Real response delayed 15 seconds beyond a 10-second deadline; late receipt retained with frozen Inconclusive outcome. |
| Lost generation receipt | Real server response dropped; original Armed intent retained through deletion/restart; parent abandonment cleared finalizers. |
| In-flight generation shutdown | Drain log showed one pending collector; same dispatch accounted once through SIGTERM/replacement. |
| Successful reorganization | Depth two, three acknowledged replacements, original branch valid-fork, replacement canonical with higher work after marker cleanup. |
| Interrupted reorganization | Depth six/five-second deadline; three receipts, acknowledged compensation, Failed/DeadlineExceeded, reservation released and latest paused baseline preserved. |
| Reorganization receipt loss | Separate invalidate/generate/reconsider faults; original branch respectively invalid/invalid/valid-fork; acknowledged replacement counts 0/0/3. Each retained Armed identity through deletion/restart despite independent chain observations; parent abandonment cleared finalizers. |

Generation image: `sha256:9dd5ca3468abd7e3522ba8f5648d47554094848ade74bcfbee7783e7d617a914`.
Reorganization image: `sha256:909686954c9e54f804e989f1e0a9057e8625a68acfb34ca079d167dc62f7caea`.
Generation's original live image predates its later stop/arm CAS correction;
that correction was checked in an isolated staged archive and in the combined tests.

Generation action UID: `927a171c-644f-4914-a68e-2759af1e6a4a`; final receipt hash
`31792ec96593bd290ff2e09298c5147fcdeab780fece425a0dfc2a32a9862dc9`.
Reorganization original tip:
`6748b4b1050f1f93473ec187bf667444d8858cd65f0e28c076b9027a97643d55`;
canonical replacement:
`773add8c7395e35bccb9270399e30959a42889303ee0c6f368a7702a9baf930b`;
final work: `0000000000000000000000000000000000000000000000000000000000000014`.

Task-only kubeconfig: `/tmp/stacks-generation-20260906.kubeconfig`; context:
`kind-stacks-generation-20260906`. Healthy namespaces: `bitcoin-generation-live`
and `bitcoin-reorganization-live`, network `bitcoin`. Fault networks were
removed by abandonment tests. Logs are temporary local evidence under
`/tmp/bitcoin-generation-*.log`, `/tmp/bitcoin-reorganization-final-live.log`, and
`/tmp/bitcoin-reorg-final-{invalidate,generation,cleanup}-live.log`; private
provisioning JSON and Secrets must not be printed or included in review artifacts.

Initial Docker disk exhaustion required recreating only this task's new cluster
with one node. Exact unused compiler caches and this task's superseded image
were reclaimed; no pre-existing cluster was modified. Core restarted at genesis
while ledger counts retained historical receipts, so live setup now checks
current chain height. Initial quota discovery required waiting for `status.used`.
The final live evidence postdates those setup failures.

## Limits

A successful reorganization has no scheduled rollback: marker cleanup leaves
the higher-work replacement as best chain. Completion is local mechanism evidence,
not peer/Stacks convergence. Unknown RPC work remains excluded; fresh-environment
recovery and administrative abandonment are unchanged. No runtime fencing,
multi-target selection, Bitcoin role migration, three-node/cross-platform
qualification, or full M0/M6 closure is claimed.
