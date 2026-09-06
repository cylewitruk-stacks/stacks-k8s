# Multi-target Bitcoin production review

## Boundary and scope

Base commit: `107bcc1339d0ce8a9d7fd3dd6827933c16cbfdb8`.
Review the working-tree diff and untracked files together. No changes were
staged or committed for this delivery.

The served profile has one aggregate-owned policy, 1–8 active targets, weights
1–1000, and one fixed interval of 1–86400 seconds. Sixteen target identities
are retained per policy lifetime. Jitter, standalone policies, per-target
credential profiles, execution adapters, and automatic ambiguous-call recovery
remain outside this slice. Broader M0/M1 completion is not claimed.

## Focused follow-up

Fable reviewed snapshot `24c8cca`; the follow-up remains unstaged on top of that
delivery. Review the complete current working tree, including untracked files.

| Finding | Resolution | Regression evidence |
| --- | --- | --- |
| Teardown can strand child finalizers | Capture all pinned target names before deleting the network; wait for root and each retained target before uninstalling. Chart and Stacks teardown instructions include all ledgers. | Documentation and link/contract checks. |
| Stale single-target prose | Root README, API guide, M0 scope, and current-state table describe weighted multi-target production. | Markdown and documentation-contract checks. |
| Removed target says Running | Idle retained targets report Paused with an explicit not-selected message. New actions remain rejected; existing admitted work retains its bounds and cleanup obligations. | Both action kinds reject new admission after removal; admitted generation and reorganization still complete and release. |
| Selection before successful ledger pin is not attributed | Root `unassignedOpportunities` and bounded `lastUnassignedTarget` record the skipped selection in the scheduling CAS. Total opportunities equal retained offered counts plus unassigned counts. Later registration never transfers or replays past selections. | Failed creation, healthy peer progress, lost write acknowledgement, later registration, reconciled totals; API-server field preservation and negative validation. |
| Reservation overwrites an existing skip reason | Preserve PolicyChanged/Expired before applying Reserved/Outstanding, including newly admitted actions. | Both action reservation paths and existing reservations/dispatches. |
| Missing execution/conflict/schema tests | Cover PolicyChanged, occupied collector capacity, both skip/receipt CAS write orders, and both Bitcoin production CRDs in the no-defaults guard. | Assertions preserve receipt hash, one acknowledged block, skip count, dispatch ID, and zero additional RPCs. |
| Short intervals skip frequently | Document one admission attempt per opportunity, expiry, resource constraints, and cumulative-counter limitations. Admission retries remain deferred. | Existing expiry/preflight tests; no timing or retry policy change. |

The action-admission portion of Fable's third finding was not a defect:
`selectAction` calls `admit` with current-membership checks. Membership bypass
applies only to already-admitted obligations. This follow-up documents and tests
that boundary without expanding authority.

Follow-up validation passed: full `make verify` using a populated temporary
index; production/scheduler `-race -count=2`; network envtest; changed Markdown
lint; and whitespace checks. Generated CRDs are included. The real index is
unchanged. These follow-up checks qualify the current source; the live results
below belong to the original delivery.

The demo and live images remain the original delivery; the follow-up does not
extend their qualification. No live suites, dependency scans, or Docker builds
are rerun for this focused pass. No dependency, Dockerfile, or RBAC change is
introduced by the follow-up.

## Review ledger

| Area | Implementation | Evidence |
| --- | --- | --- |
| API and ownership | Mutable weighted `BitcoinBlockProduction`; internal `BitcoinProductionTarget` ledgers with immutable network, root UID, and logical target. Parent pins root; root pins retained target UIDs. | Generated CRDs/deepcopies; real API-server list/weight/reference validation and target-retarget rejection. |
| Scheduling | One durable weighted selection per policy interval; persisted due time, generation binding, microsecond timestamp precision, no catch-up or availability redistribution. | Exhaustive weighted-draw buckets; restart, policy update, lost-write acknowledgement, and API round-trip tests. |
| Execution | Consumption and arm share a target status CAS. Expired, missed, unavailable, reserved, outstanding, and capacity-limited opportunities are skipped. | Executor tests for expiry during preflight, missed offers, action skips, and receipt attribution. |
| Isolation | Independent target reconcile keys and collectors; one unknown RPC or action reservation does not close other targets. | Same-network ambiguous-target regression; live finite-action isolation. |
| Membership | Removal retains ledgers and admitted obligations; re-addition reuses UID and uncertainty. Missing/replaced ledgers are never silently replaced. Exceeding the lifetime bound blocks the unsupported policy update. | Unit and envtest remove/re-add, missing-ledger, and lifetime-bound checks. |
| Actions | Lifecycle controllers read the root and exact target ledger; network worker retains all RPC authority. Existing cancellation, receipt acknowledgement, and reorganization cleanup semantics remain. | Paired executor regressions, restricted-client action envtest, and live five-block action. |
| Permissions | Scheduler creates/patches target specs and writes policy status; target worker writes execution status. Action operator gains only target-ledger reads. Stacks transaction permissions remain unchanged. | Exact rendered allowlists and chart checks; live deployments use rendered Roles. |
| Tooling and docs | Go helper adds `--target-weights`; common immutable RPC configuration connects helper targets. Existing JS bootstrap uses the per-target ledger and target address field. | Helper bounds tests, existing JS tests, updated operating/API/chart/design guidance. No new JS modules or dependencies. |

## Validation

- `make verify`: passed with a populated temporary Git index, preserving the
  real index and meaningful generated-drift checks. Includes module checks,
  vet, unit/race tests, envtests, generation drift, Helm, RBAC and workload checks.
- Focused production/scheduler race tests: passed with `-race -count=2`.
- Focused executor/scheduler tests: passed after the timestamp precision change.
- Network envtest: passed, including persisted timestamp precision and the
  multi-target lifecycle cases. Action restricted-client envtest passed.
- `TestLiveWeightedProduction`: passed against real Bitcoin Core in 43.83 seconds.
- Changed Markdown lint and `git diff --check`: passed.

No dependency versions, production Dockerfiles, or runtime module boundaries
changed. `make vuln` and full production Dockerfile builds were not rerun.
Live images used Go 1.27.1 host cross-compilation for Linux/arm64, packaged in
the existing distroless runtime base. This qualifies the runtime binaries,
not a fresh execution of the full Docker build pipeline.

## Live qualification

Fresh cluster: `kind-stacks-multi-20260906`, Kubernetes v1.36.1, Linux/arm64.
Kubeconfig: `/tmp/stacks-multi-20260906.kubeconfig`.
Namespace/network: `multi-target-live` / `multi`.
Core: `bitcoin/bitcoin:31.1` pinned to image-index digest
`sha256:da25cedc66b1daefff9f412ee196c901a899c3fa68a33b20849c3e08b5c40d63`.
Network/action image tags: `multi-target-20260906`.

| Automated observation | Result |
| --- | --- |
| Both weighted targets acknowledged baseline blocks | Initial counters 2 and 7. |
| Five-block action on first target | Completed; second baseline advanced 7 → 8 while the first remained reserved. |
| Independent Core reads during pause | Both nodes reported the same canonical tip at height 19. |
| First actor unavailable | Its opportunities were skipped; second baseline advanced to 15; declared weights stayed 1:3. |
| Target removed and re-added | Both original ledger UIDs retained; first resumed production. |
| End of test | Baseline counters 3 and 21; action receipts remained separately attributed. |

Initial Core startup failed on low Docker disk space. Reclaiming disposable
build cache allowed both nodes to start; no existing cluster or volume was
removed. Those startup skips are included in the policy's offered counts.
This run is not a statistical weight-distribution benchmark. Exact weighted
selection is covered by deterministic unit tests.

The demo is left **paused** to limit disk growth, with both nodes available for
read-only review. Its later counters may exceed the test's final observation
because production continued briefly before pause. Existing demos were not
modified. Raw test output is `/tmp/stacks-multi-live.log`.

Live dropped-connection, delayed-receipt, controller-drain, and reorganization
suites were not rerun for this slice. Their executor regressions pass with the
per-target ledger type; this does not extend their previous platform qualification.
