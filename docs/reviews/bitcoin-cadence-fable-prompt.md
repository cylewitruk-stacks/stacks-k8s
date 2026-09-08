# Fable review prompt

```text
Review the Bitcoin cadence delivery in:
/Users/cylwit/Code/github.com/cylewitruk-stacks/stacks-k8s

Start with AGENTS.md, README.md, docs/reviews/bitcoin-cadence-review.md,
docs/network-operator/bitcoin-production.md,
docs/network-operator/bitcoin-generation.md, and the M0 plan/roadmap.
Base commit: 15cbdbc76fdbd098ac7819ca32d1796971430317.
Confirm HEAD and the review boundary; review the tracked diff plus untracked
files together. Do not stage, commit, modify demos, or delete Docker resources.

This is the focused follow-up to your cadence review. Start with the ledger's
"Focused follow-up" section. The pre-follow-up working-tree snapshot is
1e94bc8f659bbe1408b779a711194fa8d5cd1889; use a populated temporary current snapshot
to compare trees so untracked source files are included rather than shown deleted.

Prioritize the new complete-cadence Validate(c,count) guard before reservation
and arming; invalid count-one modes and invalid later sequence entries; baseline
and valid-action progress past invalid pending requests; and explicit stops for
idle incompatible or missing-timer reservations. Unknown Armed calls must retain
exclusion and never be reclassified as a definite scheduling failure.

The receipt fallback must commit known progress plus a stop atomically even if
no next delay can be computed. Check real collector/fake-RPC coverage for lost
accounting acknowledgements in both commit states, preserved prior stop reasons,
no duplicate receipt count, and terminal acknowledgement before release. These
malformed-state tests intentionally bypass current CEL using fake clients.
They do not claim supported schema skew or active-environment migration.

Suspension/resume deliberately reuses the pending sample if root generation
and opportunity ordinal are unchanged, with a new time anchor. No wall-clock
sampling key, resume epoch, or public action next-due field was added.

Product context: modular disposable Stacks regtest infrastructure for ordinary
network testing and trusted defensive reliability/security investigations.
Agents orchestrate; operators expose bounded capabilities. The network operator
owns baseline scheduling, per-target mutation exclusion and receipt accounting.
The separate action operator owns bounded-action status/finalizers and has no
RPC credentials or ledger-write authority.

This slice adds:
- Baseline jitterSeconds: inclusive uniform integer delays around the central
  interval, bounded so every interval is 1..86400 seconds; target weights remain
  independent. Existing 1..8 active/16 retained target limits remain unchanged.
- A required finite-generation cadence object: Immediate, Fixed, Uniform,
  Explicit. First block immediately eligible. All modes use serial single-block
  RPCs. Later gaps follow receipts; fixed/uniform 1..60 seconds, explicit 0..60
  with exactly count-1 entries. Count <=100 and creation-relative timeout <=10m.
- Persisted nextDispatchAt in the target's generation reservation, written with
  each non-final receipt. Stable internal sampling keys prevent redraw on retry;
  microsecond timestamps round upward. No user-facing deterministic replay seed.
- Docs and M0.3/M0.4 initial-scope reconciliation, with closure review pending.

Prioritize:
1. Timing correctness: first/subsequent blocks, inclusive random bounds, explicit
   sequence indexing, timestamp precision, delayed receipt accounting, duplicate
   and lost-write acknowledgements, restart, late reconciliation/no catch-up.
2. Authority: no timer bypasses cancellation, expiry, suspension, admitted runtime
   identity, unknown Armed exclusion, lifecycle acknowledgement or reservation
   release. Immediate mode must not batch or parallelize unacknowledged calls.
3. Baseline behavior: generation-bound deadline reset, pause/resume, expiry at
   the sampled next deadline, independence from weighted selection, unavailable
   target skips and existing action coexistence.
4. API: structural/CEL mode-field constraints, count/sequence relationship,
   deepcopies and generated CRDs, including embedded action spec and timing in
   BitcoinProductionTarget status. No default-based reinterpretation of modes.
5. Architecture and scope: no new dependencies/RBAC or cross-runtime imports;
   small shared Go timing package; action operator remains lifecycle-only.
   This unreleased API change has no active-environment migration promise.
6. Evidence quality: tests must assert counts/timestamps/receipts, not just
   phases. Distinguish fake-client/controller, API-server and live-Core evidence.
   The existing demo does not contain these binaries. Docker has almost no free
   space; live testing and image rollout were not performed for this delivery.
7. Planning: M0.3/M0.4 initial contract versus deferred standalone/overrides,
   advanced recovery and broader compatibility. Do not silently close those
   runtime gates or make deferred capabilities prerequisites for this slice.

Run focused tests and make verify. Generated drift checks compare against the
index: use an isolated snapshot with a populated temporary index, including
untracked files. Do not change the user's staging area or weaken drift checks.
Report severity-ordered findings with trigger, consequence and file/line
references, followed by independent checks, limitations and commit readiness.
```
