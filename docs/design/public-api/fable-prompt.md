# Fable review prompt

```text
Review the proposed public API in:
/Users/cylwit/Code/github.com/cylewitruk-stacks/stacks-k8s/docs/design/public-api/

First review this directory's Markdown and three YAML examples as a new design,
without implementation/history context. Then verify specifically cited contracts,
sources and chart templates by following their links; this is reference verification,
not a general implementation audit. If doing only the proposal review, say so and
list material references left unverified. Review metadata is not correctness evidence.
No edits, staging, commits or cluster access.
The ledger records the base and untouched staged fingerprint; review the combined
working-tree proposal.

Purpose: composable disposable Stacks/Bitcoin networks for regtest, reliability and
trusted defensive investigations. Definitions are reusable; heterogeneous root
participants compile to network-owned instances. Go controllers/workers, optional
observation/actions/native Chaos; no scenario planner or deterministic outcome promise.

Review the six follow-up dispositions in review-ledger.md:
1. Reconstructible frozen bootstrap requirements, complete retained policy, deferred
   conflicting edits and freeze/admission-race handling.
2. Network ownership of initialization/target execution records.
3. Operational's current baseline predicates, exclusions and False/Unknown precedence.
4. Full source names in annotations/status, with source-UID label lookup.
5. Release-owned observation settings, freshness and cadence-aware progress windows.
6. Final-form normative prose and focused implementation-transition notes.

Also check the A1–A6 follow-up: peer seeds versus mandatory dependencies, explicit
config-Secret version rolls versus credential rebinding, PolicyDeferred condition /
BootstrapPending reason, latched root BootstrapPolicyUnavailable, required-baseline
pause yielding Operational=False, and accurate verification evidence/scope.

Dry-run freeze at amount X, change to Y before unfinished gates, controller restart,
pause/removal and later gate completion. Check long source names, absent/failed extra
actors, missing required baselines, stale observations and slow/changed cadences.

Reconcile baseline/late/optional examples (76/9/5 objects), participant/reference
identities, 19 genesis allocations and the worked definition-reuse excerpt. Preserve
the existing explicit worker-crash, destructive-removal, per-instance authority and
optional evidence boundaries. Separate deliberate tradeoffs from contradictions.

Return a severity-ordered ledger: concrete scenario, file/line, consequence and the
smallest correction. Distinguish inspected design from executed checks and live
qualification. Identify any remaining ambiguity without inventing additional machinery.
```
