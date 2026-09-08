# Stacks baseline review ledger

Review base: `d8af4aa0650d51f1f21bb660da569be8a81b0c3a`
(`feat(network): add steady-state Bitcoin block production`). This delivery is
uncommitted. Review all staged and unstaged changes and newly added files.

## Outcome and scope

A normal disposable Stacks network now has explicit external bootstrap and
ongoing tiny STX demand. The aggregate compiles an owned transfer policy; a
separate worker signs and submits one transaction at a time. The external
helper funds the Stacks miner's Bitcoin address, registers a PoX-4 signer and observes productive
operation. A separate bounded helper extends the signer lock for longer runs.
No agent, action controller, Chaos Mesh or observability operator is required.

The implementation deliberately offers transaction cadence rather than a
block guarantee. It does not close all of M0.6 or M2. See the
[operating contract](../network-operator/stacks-production.md) and
[qualification record](../network-operator/stacks-qualification.md).

## Review ledger

| Area | Delivered behavior | Evidence / review focus |
| --- | --- | --- |
| API and ownership | Additive `stacks.stacks.org/v1alpha1` capability, parent declaration and permanent ledger UID pin; fixed ingress and sender. | Generated CRD/deepcopy; CEL and remove/re-add envtest. Inspect deletion/replacement and backward-compatible topology behavior. |
| Aggregate composition | Compile transfer policy independently of topology convergence and Bitcoin production. | `TransactionsConfigured`, current target declarations, actor progress despite unavailable ledger. No bootstrap execution in the aggregate. |
| Admission | Direct current parent/leaf/StatefulSet/Pod reads; approved raw configuration, runtime identity, native indexing and testnet account preflight. | Negative identity tests and live unrelated-actor failure. Only the selected ingress is required. |
| Signing authority | Administrator-mounted immutable account profile in one separate worker Deployment/SA; offline locked SDK. | SDK signing tests and generated-fixture isolation assertions; rendered chart mounts and exact RBAC. No signing key in public specs/status. |
| Submission and nonce | CAS reserves exact TxID/nonce before one nonreplaying POST. One outstanding transfer applies backpressure. | Concurrent-writer test, lost arm/accepted acknowledgement tests, failed execution and external nonce drift tests. |
| Recovery and evidence | Query exact signed bytes, canonical membership and successful result; no nonce-only inference or blind POST retry. | Lost-response unit test and pending-worker replacement live test; canonical observations do not assert finality. |
| Bootstrap | Explicit fresh height-zero Bitcoin initialization, 201-block receipt, external PoX-4 enrollment and native productive-operation checks. | Final-image fresh bootstrap completed. An ambiguous startup is not silently restarted. |
| Signer maintenance | Separate externally bounded PoX-4 lock extension; its own account, explicit nonce and no blind retry. | Observed lock extension 460 → 580. This helper is not an in-cluster workflow or PoX-5 implementation. |
| Packaging | Separate Node/Go worker image; independently enabled chart component; exact Role/SA separation. | All four chart combinations checked: topology only, Bitcoin only, transfers only, both. ConfigMap `list` is for readiness. |
| Documentation | API/chart/operator/example/operations guidance, current-state and roadmap updates. | M1/M2 and broader remediation remain open; local image support is stated narrowly. |

## Validation

Passed: `make verify`, `make vuln`, and `make docker-check`.
Markdown lint passed for all 19 changed Markdown files; whitespace and final
documentation-contract checks also passed.
The full verification includes Go unit/race/vet, envtest, module/generation
checks, SDK tests, and exact chart RBAC/workload validation. Native live suites
are opt-in and were run separately.

Live results are recorded in the qualification document: fresh bootstrap,
exact transfers and Stacks tip progress, pause/cadence, unrelated unhealthy
actor, pending-worker replacement, and administrative ledger abandonment.
Renewal evidence is the helper's observed unlock-height change, not an
independent assertion about its transaction's permanent canonical membership.

The repository Markdown link test now excludes installed `node_modules`, just
as it excludes `.git`. This avoids auditing missing upstream package files;
all repository-owned Markdown remains in scope. No existing contract test was
weakened to excuse a product failure.

## Deliberate limits

- One exclusive transfer account and ingress per namespace worker; no account
  sharing, multi-ingress scheduling, automatic top-up, fee escalation or backlog.
- A never-sent or vanished reserved transaction can remain closed indefinitely.
  Recovery uses a fresh namespace/account/environment, not a ledger reset.
- Static worker access is isolated. Withdrawal requires worker termination;
  rotation uses a fresh account/environment. Existing signed transactions may
  still execute; there is no protocol revocation or paused-process fence claim.
- Bootstrap/renewal are explicit external tools. The initial signer lock is
  finite; long-running networks need the bounded renewal helper. PoX-5 and
  general image/epoch compatibility remain open.
- Qualification uses one local normal Stacks image on one-node kind. No public
  release, managed-cluster, multi-miner, or arbitrary-fault support is implied.
- Status is bounded latest-transaction evidence, not a journal or indexer.

## Review entry points

Start with `apis/network/stacks/v1alpha1/types.go`,
`operators/network/internal/network/transactions.go`,
`operators/network/internal/transactions/`, and
`operators/network/transactions/`. Then inspect chart/manager wiring, envtest,
the native live test and documentation claims. The
[self-contained Fable prompt](stacks-baseline-fable-prompt.md) specifies the
review base, context and suggested checks.

## Fable review refinements

Fable found no blocking defects and independently confirmed native inclusion
semantics, controller/API tests, and rendered permissions. The follow-up adds:

| Review point | Refinement |
| --- | --- |
| Definite rejection versus unknown response | Matching native rejection persists a bounded reason and reports `Blocked`; nonce retention and exact inclusion observation continue. Unknown/malformed responses stay ambiguous. |
| Re-evaluated nonce mismatch | Documented the two-second check and exact-match resumption; status no longer asserts that lag proves external account use. |
| Signer horizon | Documented approximately 20 minutes for the initial lock and renewal eligibility near 10 minutes remaining at the default cadence. |
| `nextNonce` interpretation | API comment and operating guide distinguish outstanding `nonce` from post-inclusion `nonce + 1`. |
| Offline CI | Documented npm registry/cache prerequisites, offline cache mode and online vulnerability-audit dependencies. |

Regression coverage checks matched/malformed/mismatched rejection responses,
raw-detail exclusion, restart and unavailable-ingress retention, later exact
inclusion, clearing old rejection for a new TxID, and lost rejection writes
before and after persistence. Envtest checks that the API retains supported
reasons and rejects arbitrary text. Nonce tests cover both lagging and leading
values and resumption only at an exact match. Rejection-write loss before
persistence remains an honest evidence gap, without releasing the reservation.

Follow-up checks passed: `make verify`, focused transaction tests with `-race`,
Markdown lint, whitespace, and an offline `npm ci --ignore-scripts` using the
populated cache. No dependencies, Dockerfiles, or deployment permissions changed
in this refinement. The live-image qualification remains the earlier run, as
explicitly noted in its record.
