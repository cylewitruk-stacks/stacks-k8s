# Public API foundation review ledger

Base: `a07f70238e85b4a8e47c93a5f416c507b40c3102`. Delivery is an uncommitted working-tree
change. The original delivery is staged by the user; this follow-up is unstaged plus new
files. The agent has not changed the user's index or any cluster.

Staged-diff SHA-256 at follow-up start:
`ac04a7c7c0a7412ecede6420d4e283825a5644fef8764701fab25f5060fde3ca`.

## Review boundary

First implementation slice: `v1alpha2` schemas, participant composition/ownership,
independent scoped identity resolution and immutable genesis capture. Existing runtime
controllers retain their behavior. A separate preview chart avoids pretending incompatible
`v1alpha1` and `v1alpha2` shapes are convertible. Runtime activation is deliberately absent.

| Area | Implementation / evidence |
| --- | --- |
| Public API | Shared typed branches and structural/CEL schemas; strict admission of the full example |
| Identity | Independent `libs/stacks`; 32 pinned SDK identity vectors; scoped resolver Jobs, metadata-only controller reads |
| Composition | Defaults → definition → overrides → absent-value fallbacks; exclusive alternatives and atomic lists |
| Ownership | Durable participant UID allocation, single-use names, explicit destructive removal, independent definitions |
| Freeze | Durable admitted policies, immutable genesis, canonical chain digest, public cohort requirements, bounded artifact |
| Failure boundaries | Missing instance/genesis is terminal; lost publication adopts the same artifact; no runtime/health claim |
| Packaging | Separate Go image/chart; incompatible-CRD guard; exact rendered permission allowlist; real RBAC authorization checks |

## Expert correction pass — 2026-09-11

The staged foundation and earlier refinements are the review base; this correction pass is
unstaged plus new files. The index and cluster remain unchanged.

| Finding | Correction / regression |
| --- | --- |
| C1: stale transitive identity | Shared uncached admission/capture checks verify account/wallet fingerprints and credential UID/deletion metadata; derived wallet accounts are checked transitively. Per-pass memoization bounds duplicate reads; capture uses a fresh pass. Unit cases cover absent/replaced/deleting credentials and changed account UID/fingerprint without reading Secret data |
| C2: retained policy dependencies | The selected admission, including a deferred prior policy, must retain available dependencies. `DependencyUnavailable` preserves admission evidence without latching root `Failed`. Envtest injects loss of an old-only recipient read and verifies recovery; unit cases cover retained participant definition loss/replacement and transitive dependency loss |
| C3: controls on rejected edits | Control projection follows identity validation and precedes composition/admission. Envtest combines traffic target and Bitcoin payout/initialization rejection with simultaneous supported control changes |
| C4: late signer attachment | Permanent one-signer-per-node validation runs outside genesis compilation. Existing admissions win; new contenders resolve by participant-name order. Envtest adds conflicts after a real capture, preserves admitted UIDs/policies and demonstrates permitted shared accounts |
| C5: resolver report replacement | Job identity includes the complete input/output binding; replacement reports get a new bounded inspection Job with the existing immutable key. Unit regression checks old-job refusal, one replacement Job, byte-identical key/report and rejection of altered Job input |
| C6: incomplete resolution | Allocation, withdrawal and terminal stop report `Resolved=Unknown`; fully resolved paused roots and validated captures report True. Existing-capture recovery uses `GenesisRecovered/Unknown` and requeues to validate current intent. Envtest checks intermediate conditions and recovery after a newer unresolved spec |

Reviewer follow-up also checks that retained participant availability depends on pinned
source/admitted inputs, not workload readiness or the latest candidate's condition. Cyclic
admission dependencies fail closed. No account exclusivity, Secret-data access, protocol
worker restart recovery, schema, RBAC, dependency or container-input change was introduced.
Only the explicit permanent signer-to-node constraint was moved; stacker cohort uniqueness
remains a bootstrap check.

### Correction verification

| Check | Result / log |
| --- | --- |
| Full `make verify` | Passed; `/tmp/stacks-foundation-corrections-verify.log`, `EXIT_CODE=0`, `DRIFT_EXIT_CODE=0` |
| Populated-index snapshot | `1166b445dfa646ae28dbb2987649062d28b5d410`; no generated drift |
| Foundation unit/race | Passed; `/tmp/stacks-foundation-corrections-race.log` |
| Foundation envtest | Passed, 56 s; `/tmp/stacks-foundation-corrections-integration-final.log` |
| Tagged Go vet | Passed; `/tmp/stacks-foundation-corrections-vet.log` |
| Markdown | Passed; `/tmp/stacks-foundation-corrections-docs.log` |

The Kubernetes reviewer signed off the identity, resolver-output, retained-dependency and
condition boundaries plus the permanent attachment rule. The Go reviewer signed off C1–C6.
Both reran targeted tests and independent private probes after the additional retained-source
and publication-recovery fixes. Reports: `/tmp/stacks-foundation-corrections-k8s-review.md`
and `/tmp/stacks-foundation-corrections-go-review.md`. Their evidence is unit/fake-client;
the API-server and complete verification results are the author's checks.

Full verification includes the final source and test changes, including the reviewer-requested
retained-source and newer-generation recovery regressions. Only this ledger's result entries
changed afterward; Markdown and whitespace were rechecked. The temporary verification
snapshot was removed; logs remain. No live cluster, kubelet/GC,
protocol runtime or image rollout is claimed; dependency and container checks were not
repeated because their inputs are unchanged.

## Foundation refinement follow-up

| Finding | Disposition / regression evidence |
| --- | --- |
| M1: kind change | Map-list CEL rejects a kind transition; defensive controller fallback reports recoverable `KindChanged`, not identity loss |
| M2: shared senders | Removed writer exclusivity; real API capture accepts traffic and contract workers sharing a sender |
| M3: input digest | Semantic projection excludes UIDs, generations and allocated names; unit tests vary network/account/participant identities and retain key/image/schedule sensitivity |
| M4: CRD guard | Names come from all bundled CRDs; Helm server-side lookup tests reject each of 15 independently incompatible CRDs and accept a compatible inventory |
| M5: lifecycle evidence | Envtest edits captured definitions, checks complete admission/dependency retention and correction, and deletes a recorded participant to assert terminal `InstanceLost` without recreation |
| Entry error isolation | `ResolutionError`, `NameAlreadyUsed` and `RequiresReplacement` leave unrelated admission/removal possible; initial freeze still requires a complete valid graph |
| Reads and watches | Cached definition/account resolution, complete uncached freeze recheck, watch-driven settled roots; stale-cache fixture proves the fresh source read precedes refusal to freeze |
| Resolver failure | Real manager observes API-valid failed Job status, reports bounded `ResolverFailed`, retains the Job UID and does not poll/retry it |
| Bitcoin protected fields | Mutable wallet attachments preserve a late actor UID; payout/initialization edits require replacement before deferral. Genesis captures public payout binding so a new production name cannot bypass frozen identity |
| Zero faucet allocation | Second full capture accepts zero faucet balance with 18 allocations and no faucet principal allocation |
| Stopped removal | API-server test removes the old participant while stopped and refuses allocation of a newly selected entry |
| Cache/RBAC scope | Job/report caches filter foundation-managed labels; tests observe selected outputs and exclude unrelated objects. ServiceAccounts use direct reads with create/get only |
| Events permission | Retained: controller-runtime v0.25 leader election supplies the Lease lock's event recorder; this permission is used indirectly |

New schema changes are the participant kind transition rule and the captured Bitcoin payout
binding. No dependency, Dockerfile or protocol-runtime changes. API-server tests use envtest;
Helm lookup tests use a local HTTP discovery/CRD fixture, not a live Helm installation.
No worker activation, nonce-recovery machinery or account leases were added.

### Follow-up verification — 2026-09-11

| Check | Result / log |
| --- | --- |
| Full `make verify` | Passed; `/tmp/stacks-foundation-followup-verify.log`, `EXIT_CODE=0`, `DRIFT_EXIT_CODE=0` |
| Populated-index snapshot | `523f2971121ec3bd65f5c5e5c5375daacfaa51bb`; no generated drift |
| Focused foundation race tests | Passed; `/tmp/stacks-foundation-followup-unit.log` |
| Foundation envtest under race | Passed, 133 s; `/tmp/stacks-foundation-followup-integration-final.log` |
| Tagged Go vet | Passed; `/tmp/stacks-foundation-followup-vet.log` |
| Markdown, relative links, whitespace | Passed; `/tmp/stacks-foundation-followup-docs.log` and `git diff --check` |

The final stale-cache and selected-report assertions were strengthened after the focused
race run; the full verification snapshot includes and passes those assertions (envtest 50 s).
Only ledger results and documentation wrapping changed after that snapshot. No dependencies
or container inputs changed, so vulnerability checks and image checks were not repeated.
No live qualification, kubelet/GC execution or hosted Actions run is claimed. The verification
snapshot was removed; logs remain. The staged-diff fingerprint remains unchanged.

## Validation

All checks below passed locally on 2026-09-10. Full verification used a separate,
populated-index snapshot (tree `ee96cd4e954741fea2785129b9c4ee1158f1337e`); `git diff`
against that index remained empty after generation. Subsequent CI wiring and documentation-only
edits were checked separately. The main repository index remains unchanged.

| Check | Result / log |
| --- | --- |
| Full `make verify` | Passed, explicit exit 0; `/tmp/stacks-foundation-verify-complete.log` |
| Final API generation/drift | Passed against populated snapshot index; also covered by full verification |
| Foundation unit/race regressions | Passed; `/tmp/stacks-foundation-race-final.log` |
| API-server lifecycle under race | Passed; `/tmp/stacks-foundation-integration-final.log` |
| Scoped controller and resolver RBAC | Passed against real API authorization, including full cohort resolution under controller permissions |
| Library SDK vectors | 32 public-encoding vectors passed in normal and race gates |
| `make vuln` | Passed, zero reachable or imported-package vulnerabilities; `/tmp/stacks-foundation-vuln.log` |
| `make docker-check` | Passed; `/tmp/stacks-foundation-docker-check-final.log` |
| Foundation image build | Passed; `/tmp/stacks-foundation-build-complete.log` |
| Image entry-point smoke | Passed with `--network none`; `/tmp/stacks-foundation-image-smoke.log` |
| CI syntax and final module/foundation gates | Passed; `/tmp/stacks-foundation-workflows.log`, `/tmp/stacks-foundation-gates.log` |
| Markdown and whitespace | Passed; `/tmp/stacks-foundation-markdown.log` and `git diff --check` |

The vulnerability tool reports GO-2026-5932 for the unused `x/crypto/openpgp` package in
one required module. This delivery imports `ripemd160`, not `openpgp`; no affected package
or symbol is imported. Detail: `/tmp/stacks-foundation-vuln-detail.log`.

Focused regressions also cover explicit empty-list overrides, default-identity exclusion
for miners, transient read recovery clearing stale conditions, mutable selection versus
protected bindings, and committed/uncommitted lost key-write acknowledgements. The local
image is `stacks-network-operator:review`. The temporary verification snapshot was removed;
logs remain available for review. Hosted Actions were not executed.

## Limits and next slice

No live cluster, image rollout, kubelet/GC or protocol qualification. Envtest invokes the
Job entry point directly under its ServiceAccount because it has no Job controller.
The foundation cannot activate actors or finish bootstrap gates. Actor configuration escape-hatch
validation and broader wallet descriptor parsing are not delivered. The existing SDK
runtime exception remains until transaction signing is migrated.

Next: actor participant reconciliation and configuration rendering from captured genesis,
then capability worker migration and live protocol initialization. Preserve the existing
Bitcoin execution contract when integrating those workers.

## Fable review prompt

Review the staged foundation delivery plus the unstaged expert corrections and all untracked files.
The staged diff alone does not include the refinement pass.

Review this working-tree implementation against
`docs/network-operator/public-api-foundation.md` and the relevant contracts in
`docs/design/public-api/`. Review the slice actually delivered; distinguish implementation
bugs from explicitly pending runtime capabilities.

Focus on immutable identity/freeze boundaries, partial writes and retries, source/participant
ownership, schema admission and pruning, whole-policy retention, private-key boundaries,
rendered RBAC and actual authorization, independent module boundaries, and evidence claims.
Check that the old runtime remains buildable and the preview cannot silently replace its
incompatible CRDs. Inspect the tests for meaningful outcomes, especially lost acknowledgement,
post-freeze edits, protected versus deferred policy, unrelated participant progress, stopped
removal, shared accounts, zero faucet allocations, UID-free input digests, failed resolver
Jobs, cache scope, partial incompatible CRD installations and strict full-example admission.
Also check C1–C6 above: stale credential status at admission/capture; old-only dependencies
under policy deferral; control projection during policy rejection; late attachment conflicts;
report replacement without key regeneration; and accurate intermediate/publication-recovery
conditions. Distinguish controller-owned identity inspection from protocol-worker recovery.

Run focused unit/race/envtest checks and `make verify` in an isolated populated-index snapshot
when feasible. Do not stage, commit,
modify the real Git index, or touch a live cluster. Report severity, trigger, consequence,
recommended fix and verification limits for each finding.
