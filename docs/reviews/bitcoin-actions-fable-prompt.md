# Fable review prompt: consolidated Bitcoin action fixes

```text
Review only the follow-up fixes in:
/Users/cylwit/Code/github.com/cylewitruk-stacks/stacks-k8s

The user staged the implementations and first reviewed corrections:
  current staged tree: 10ba02488746ecd8e812fcb8d2ce012baaaf2578
  original combined implementation: ea0f6c3163810b63a270afe3368ec9e8d98b7c6f
  underlying commit: f788749b638873bc270aa93ebe88172fea707f08

Read AGENTS.md, README.md, docs/reviews/bitcoin-actions-review.md, the generation
and reorganization guides under docs/network-operator/, and the R4 amendment at
docs/design/bitcoin-reorganization.md. The consolidated ledger preserves prior
qualification and distinguishes this follow-up's checks from earlier live tests.

Inspect git status. This follow-up is git diff plus untracked files relative to
the current staged tree; /tmp/bitcoin-actions-read-wait.patch captures it fully.
The former separate review ledgers/prompts have been consolidated intentionally.
If the review boundary changed, identify it before attributing changes.
Do not edit, stage, commit, deploy, or run live mutation suites.

Product: modular disposable regtest networks; small finite actions share the
baseline Bitcoin executor and durable reservation. External agents orchestrate
experiments. Successful reorganization permanently changes the selected branch
until subsequent chain competition; cleanup removes an invalidity marker, not
an automatic rollback. Completion makes no Stacks/peer convergence claim.

Current changes to review:
- After acknowledged cleanup, an admission-read error waits as Reserved while
  now < expiresAt+30s; at the horizon an unresolved read ends inconclusively with
  known cleanup. A successfully read changed identity still stops immediately.
  Successful final reads remain permitted beyond the horizon. No mutation or
  additional cleanup is authorized by this wait.
- Pending reorganization reports TargetBusy for either action kind's reservation.
- Regressions inject an API Pod read failure, cover recovery/exhaustion at the
  exact horizon and immediate runtime change before expiry, preserve cleanup and
  acknowledgement gating, and check mutation counts and both busy cases.

Previously reviewed corrections retained in the staged base:
1. UID plus immutable spec retains identity across finalized deletion's metadata
   generation increment. Cancellation must persist stop, retain exclusion until
   terminal receipt/cleanup acknowledgement, then release and remove finalizer.
2. Action expiry stops incomplete replacement work, not final reads. Cleanup
   authorization expiry must not undo an acknowledged cleanup fact. Completed
   replacement receipts can proceed to cleanup within its existing horizon.
   Unavailable/changed runtime after cleanup is inconclusive final verification
   with CleanupComplete=True, not CleanupUnsafe or a false retained-exclusion claim.
3. Generation queued behind reorganization reports TargetBusy. Never-armed
   reorganization awaiting executor withdrawal does not report recovery.
4. Read-only generation preflight failures wait within the existing deadline;
   successful negative regtest/address validation fails definitely. No mutation
   retries, deadline resets, lost-receipt recovery or extra RPC permissions.
5. Repeated valid-fork limitation is documented; delayed-arm test fixtures are
   consolidated without removing either kind's CAS race coverage; a pre-existing
   production-network envtest update uses conflict retry with a fresh read.

Prioritize real regressions: release before acknowledgement; unknown-call replay;
incorrect terminal/cleanup facts; current-identity bypass; expanded cleanup
permission; unbounded dispatch; malformed preflight interpreted as success;
API-server behavior inadequately represented by fake clients. Check that tests
assert the actual cancellation outcome and receipt retention, not just deletion.

Suggested checks: make verify; focused production -race tests; envtest using
make -C operators/network test-integration; Markdown and Git whitespace checks.
make verify compares generated outputs with the index. Use an isolated checkout
or a temporary GIT_INDEX_FILE containing the full working snapshot; preserve the
user's real staged index. Report exactly which snapshot your tests exercised.
Dependencies/container behavior did not change in this follow-up; prior supply-
chain results are historical evidence. Do not claim you reran live suites.

Read-only demo inspection is allowed only with the exact context and kubeconfig
in the ledger. Never use default contexts, change other clusters, or print Secrets
or private provisioning manifests. Running demos predate these follow-up fixes.

Return severity-ordered findings with file/line, trigger, consequence and minimal
fix. Separate optional refinements; say explicitly if there are no findings.
Finish with independent checks, limitations, and readiness-to-commit verdict.
```
