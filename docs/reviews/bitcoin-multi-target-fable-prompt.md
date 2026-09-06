# Fable review prompt

```text
Review the multi-target BitcoinBlockProduction delivery in:
/Users/cylwit/Code/github.com/cylewitruk-stacks/stacks-k8s

You are starting fresh. Read AGENTS.md, README.md,
docs/network-operator/bitcoin-production.md,
docs/reviews/bitcoin-multi-target-review.md, and the current M0 plan/roadmap.
Base commit: 107bcc1339d0ce8a9d7fd3dd6827933c16cbfdb8.
Confirm HEAD and the review boundary. Review all tracked changes and untracked
files together. Do not stage, commit, or modify running environments.

This is the focused follow-up to your review of snapshot 24c8cca. Compare it
to a populated temporary snapshot: plain git diff against that tree can show
still-untracked delivery files as deletions. Start with
the ledger's "Focused follow-up" section. Prioritize the new unassigned-selection
accounting and failed-registration/lost-acknowledgement tests, inactive-target
reporting and action membership distinction, skip-reason precedence, both
skip/accounting CAS write orders, and teardown instructions for retained targets.
Verify root opportunities = sum(retained offered) + unassignedOpportunities,
including after successful later registration; no unassigned choice is replayed
or redistributed. The lastUnassignedTarget field is a bounded latest fact.

New action admission still uses admit(...baseline=true). Only already-admitted
work uses baseline=false; do not interpret that continuation path as permission
for new actions on removed targets. This boundary is now directly tested for
both action kinds. Short-interval retry behavior, self-peer configuration and
watch optimizations are intentionally unchanged. The demo contains the original
reviewed binaries, not this follow-up.

The product is modular infrastructure for disposable Stacks regtest networks,
including ordinary testing and trusted defensive reliability/security work.
Operators provide small capabilities; agents orchestrate investigations.
This slice adds weighted steady-state Bitcoin production, not a scenario engine.

Scope:
- One aggregate-owned policy; 1–8 active targets, weights 1–1000, one fixed
  policy interval of 1–86400 seconds, sixteen retained target identities.
- BitcoinBlockProduction owns scheduling facts; BitcoinProductionTarget owns
  each target's execution, receipt accounting, and action reservation facts.
- Timing jitter, standalone policies, per-target credential profiles, adapters,
  credential epochs and automatic ambiguous-call recovery remain deferred.

Prioritize:
1. Selection durability: CAS and lost acknowledgements, policy generation,
   timestamp round trips, restart/no-catch-up behavior, and no redistribution
   of opportunities selected for unavailable or reserved targets.
2. Execution authority: consume and arm atomically; expiry and fresh identity
   checks after preflight; skipped/missed counters; no unknown-call replay.
3. Target isolation: independent queue keys and collectors; current root and
   target UID/owner bindings; missing/replaced target ledgers; action admission
   and baseline exclusion scoped to exactly one target.
4. Updates: membership, weight/address/cadence/pause changes; removed targets
   retain ambiguity and admitted cleanup obligations; re-addition cannot reset
   ledger identity; lifetime bounds fail explicitly.
5. Existing action guarantees: reservation -> arm -> receipt -> lifecycle
   acknowledgement -> release; cancellation and reorganization cleanup.
   Reorganization cleanup must not roll back the resulting best chain.
6. Least-privilege RBAC, independent operator/module ownership, served API
   schemas and generated artifacts. Stacks transaction RBAC must not expand
   accidentally because its allowlist shares helpers with Bitcoin production.
7. Tests assert effects, not merely transitions. Distinguish fake-client,
   API-server, and real Core evidence and check the ledger's limitations.
   Check helper peer configuration and that JS stays confined to existing SDK
   bootstrap/signing glue.

Run appropriate focused tests and make verify. The latter checks generated
changes against the index: use an isolated snapshot with a populated temporary
index or compare generated files against pristine copies. Do not weaken drift
checks or change the user's staging area. Report severity-ordered findings with
concrete triggers, consequences and file/line references, then limitations and
commit readiness. Do not require deferred capabilities to accept this slice.

A fresh paused demo is available for read-only inspection with explicit flags:
--kubeconfig /tmp/stacks-multi-20260906.kubeconfig
--context kind-stacks-multi-20260906
namespace multi-target-live, network multi.
Docker disk space is tight; do not create additional clusters or delete caches,
images, volumes or existing environments during review. Live qualification
results and its exact scope are recorded in the review ledger.
```
