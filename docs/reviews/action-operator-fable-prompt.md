# Fable review prompt

```text
Review the follow-up to your two-delivery stacks-k8s review. If this is a fresh
session, the context and original review scope below are self-contained.
Repository: /Users/cylwit/Code/github.com/cylewitruk-stacks/stacks-k8s

Read AGENTS.md, docs/design/README.md, docs/design/steady-state-operation.md,
docs/reviews/bitcoin-node-cleanup-review.md, and
docs/reviews/action-operator-review.md first.

Boundary:
- HEAD 75ae35d89b00c2d7a9edce344c31ef78230635ff.
- Delivery 1 is staged tree 887d7dce206bd57248031c05a809f5dae07389bd:
  remove Bitcoin mining roles end-to-end, with no compatibility shim or
  obsolete historical contract bundle. Preserve Stacks roles.
- Delivery 2 is the unstaged diff plus untracked files: independently deployable
  action operator. BitcoinBlockGeneration is the default baseline action;
  existing BitcoinReorganization lifecycle moves with it and stays optional.

The pre-follow-up combined delivery snapshot is tree
e15bf68b2fdc312f5e3bf3a8e674b81b0819ac47 (not a commit). Prior findings were:
missing action CRDs caused a delayed worker startup failure and the docs gave
the wrong install order; quota ownership prose was stale; root test-race had
accidentally downgraded observability. The ledger's Fable follow-up section maps
all fixes and agreed optional refinements to evidence.

Prioritize the new prerequisites.go check and its SetupWithManager call site:
- Only enabled kinds and the exact consumed API version are resolved.
- Missing APIs report the flag, required kind/version and owning chart.
- Other discovery errors preserve their cause, without asserting a missing CRD.
- Validation runs before collectors/controllers are registered; disabled flags
  retain baseline startup without action CRDs.
- Envtest installed-kind cases require a fresh reconciliation after each
  manager starts, not a status value left by a previous manager.
Check install/remove order, quota ownership, restored race target, negative
both-disabled chart render, layout coverage, one-release-per-namespace rule,
module publication/MVS caveats and documented clock-skew limits.
Do not require dynamic discovery/fallback, a new paired-test module or timing
protocol changes to close this bounded follow-up.

Keep the user's index untouched. Use isolated snapshots with their own indexes
for make verify, which checks generated drift. Populate each snapshot index
before verification, or independently compare all generated artifacts against
pristine copies; an empty index makes the git drift check vacuous. Do not stage, commit, or mutate
running environments. Confirm your exact review boundary before testing.

For delivery 1, trace role-free Bitcoin identities through API/schema,
compilation, workload rendering, canonical fixtures and independent observer
verification. Confirm Stacks role fields remain required. Verify deleted tests
belonged only to the removed superseded fixtures and current coverage remains.

For delivery 2, read docs/action-operator/operations.md and qualification.md,
then trace network executor admission -> action status/finalizer -> RPC ledger
-> receipt acknowledgement -> release. The action operator must never issue
RPCs or write production ledgers; the network worker must never write action
status/finalizers. No second reservation owner, dispatcher or replay path should
exist. Reorganization cleanup must not roll back the resulting chain.

Check:
1. Existing reservation, expiry, cancellation, late receipt, unresolved-call and
   cleanup guarantees across separate processes, including lifecycle outage.
2. Independent per-kind enabling and baseline production without action CRDs.
3. Exact primary-resource finalizer RBAC, status ownership, namespace scoping,
   leader election, quotas, probes, chart schemas and one CRD/chart owner.
4. Independent runtime imports and container source boundaries. Network tests
   deliberately import the action controller packages through a test-only
   module requirement; assess that qualification/versioning tradeoff.
5. Real API-server authorization/finalizer coverage and preserved paired tests;
   distinguish seeded ledger envtests from RPC-driven live evidence.
6. Makefiles, module pins, CI, dependency/container checks, generated artifacts,
   examples and docs/roadmap consistency. Do not expand into unimplemented
   instrumented actions or a generic in-cluster scenario engine.

Follow-up checks: make verify, focused prerequisite unit/envtest checks and
changed Markdown lint passed. No dependency/container/RBAC inputs changed;
live suites and vulnerability/container checks were not rerun for this follow-up.
The running demos still use the pre-follow-up images.

Original delivery checks: make verify, make vuln, make docker-check, both operator image
builds, Markdown lint, chart-authorized envtest, and three live tests passed.
Live coverage includes baseline before any action CRD, lifecycle-only outage,
finite generation and compensated local reorganization. Delayed/dropped-RPC
live variants were not rerun; the earlier controller regressions remain.

Return severity-ordered findings with precise paths, concrete triggers, impact
and minimal fixes. Separate defects from optional refinements. State your
independent checks, limitations and commit-readiness verdict for each delivery.
```
