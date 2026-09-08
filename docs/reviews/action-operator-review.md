# Action operator delivery review

## Boundaries

- HEAD: `75ae35d89b00c2d7a9edce344c31ef78230635ff`.
- Staged cleanup: tree `887d7dce206bd57248031c05a809f5dae07389bd`;
  [cleanup ledger](bitcoin-node-cleanup-review.md).
- Action delivery: subsequent unstaged changes and untracked files. No commit
  was created; the cleanup index remains unchanged.

## Fable follow-up

The pre-follow-up combined delivery snapshot is tree
`e15bf68b2fdc312f5e3bf3a8e674b81b0819ac47`. The follow-up stays unstaged on top
of the reviewed deliveries, including the small “Stacks miner” wording fix.

| Finding | Resolution | Validation |
| --- | --- | --- |
| Missing action CRDs stop the shared worker | Install action chart/CRDs before network selection flags; setup resolves only enabled action GVKs before registering workers. Discovery failures retain their cause. | Unit coverage for flags, served versions and discovery errors; envtest missing-kind rejection before manager start. |
| Baseline independence | Disabled action flags require no action discovery or watches. | Envtest starts without action CRDs and observes a fresh paused-baseline reconciliation; all three enabled combinations reconcile after CRD installation. |
| Quota ownership prose | Network gets read-only action rules; action chart owns quotas and lifecycle writes. | Both affected READMEs corrected. |
| Root race target | Observability again uses `test-race`. | Root dry-run includes all runtime race targets; full verification runs their race suites. |
| Small completeness items | “Stacks miner” wording, disabled-kind negative Helm render, action module/chart layout checks. | Markdown, Helm and contract checks. |
| Deployment/version assumptions | One action release per namespace; API → action → network module publication; test dependency affects network MVS selection. | Operating, chart, release and development guides agree. |
| Clock skew | Document conservative outcome reporting and retained acknowledgement requirements. | Timing authority redesign remains deferred; lifecycle logic is unchanged. |

`make verify` passed using a populated throwaway index, preserving meaningful
generated-drift checks and the user's index. Focused prerequisite unit/envtest
checks and changed Markdown lint passed. The final envtest requires a fresh
status write from each started manager, so previous `Paused` status cannot
satisfy an installed-API case.

No dependencies, API schemas, RBAC grants or container definitions changed in
this follow-up. Vulnerability/container checks and live suites were not rerun;
the running demos and image qualification remain the pre-follow-up versions.
The new setup behavior is qualified against real API discovery in envtest.
A missing enabled API still prevents this worker's baseline from starting;
validation improves diagnostics rather than providing a fallback mode.

## Review ledger

| Area | Change | Evidence |
| --- | --- | --- |
| Runtime ownership | Existing generation/reorganization lifecycle controllers moved to `operators/action`; network worker retains RPC and ledger ownership. | Existing paired executor tests execute the moved controllers; API-server and live regressions pass. |
| API | Shared finalizer/terminal vocabulary; action CRDs generated into their single owning chart. | Types-only module policy, drift checks and CRD ownership regression. |
| Modules | Independent action runtime/image; only API imports across production boundaries. Network tests have an explicit action-module dependency. | Runtime import guard, isolated module checks and both image builds. |
| Permissions | Action writer has no Secret, workload or ledger mutation authority; executor loses action writes. | Exact rendered allowlists for all enabled-kind combinations; real restricted-client envtest. |
| Lifecycle | Separate manager, namespace cache/Lease, health checks and bounded concurrency. | Chart validation, API-server restart and live Deployment outage. |
| Packaging | Independent chart/image/version, per-kind flags and quotas, root verification, CI and dependency scanning. | `make verify`, `make vuln`, `make docker-check`, Helm/RBAC/workload checks. |
| Product behavior | Baseline works with no action CRDs. Finite generation is the default action; existing reorganization remains optional. | [Fresh-cluster qualification](../action-operator/qualification.md). |
| Documentation | Install, upgrade, disposal and compatibility boundaries; M0.8 remains in progress. | Changed Markdown lint and repository link/contract tests. |

The only mechanism change is deployment ownership. Reorganization still retains
its resulting chain; cleanup removes the temporary invalidation marker. The
existing fail-closed RPC ambiguity profile is preserved. Broader protocol hooks,
overrides, reset/readmission and arbitrary release skew remain outside this
baseline delivery.

The test-only network-to-action module requirement keeps paired controller
regressions intact; production imports and container source copies exclude the
other runtime. Review this distinction explicitly when assessing independent
versioning. The supported initial pair is `0.1.0`/`0.1.0`.

See the [combined Fable prompt](action-operator-fable-prompt.md).
