# Native Chaos Mesh delay review ledger

## Boundary

Base HEAD: `6de2a10bd1a765875d5d3821207475f4086cd07a`.
The user staged the initial delivery as tree
`5a319dbeeecd8b17a5d680ca277667d2678206db`. Review that staged base plus the
unstaged follow-up together. This follow-up preserves the staged content and makes no
commit. Existing operator runtimes, served APIs, generated
CRDs, Dockerfiles and their RBAC are unchanged. The network contract test changes
only to track reviewed initial M0.3/M0.4 closure and retained wider deferrals.

## Review follow-up

| Finding | Revision | Regression or verification |
| --- | --- | --- |
| Existing nonconforming faults cannot clean up | Make shape/bounds/label rules create-only with `oldObject != null`; retain update spec immutability. | Envtest seeds nonconforming delay and partition objects before installing admission, then verifies retained cleanup status, finalized deletion, rejected spec edits and rejected new unsupported objects. No native controller runs in this test. |
| Profile tests absent from CI | Add a Go/Helm chart-policy job invoking both verification targets; include tools/charts in security triggers and relevant cache inputs. | Actionlint plus local execution of the workflow commands. Hosted Actions execution requires publication and is not claimed. |
| Quota probe inherits server metadata | Decode a fresh native example for the second request. | Live quota rejection now starts from a fresh manifest. |
| Render checker omits policy matching | Compare the full typed match constraints against exact namespaced NetworkChaos CREATE/UPDATE coverage. | Reject dropped UPDATE, alternate resource/scope, extra/excluded rules and object selectors. |
| Checksum failure hides cache location | Include the offending file path and recovery guidance; still fail on mismatched bytes. | Checksum regression requires the path in the error. |
| Vocabulary/list inconsistencies | Use Implemented and Contract complete; put initial-scope qualifications in descriptions. Remove the split chart list and stale M0.4 pending-review phrase. | Scope-preservation contract test and Markdown/link checks. |

The corrected guard is deliberately `oldObject != null || (...)`; using `== null`
would bypass creation validation. No new native mechanisms, broader agent grants,
API changes, runtime changes or dependencies are introduced by this follow-up.

## Delivery and evidence

| Area | Implementation | Evidence and limits |
| --- | --- | --- |
| Planning | Close reviewed initial M0.3/M0.4 contracts; M0.5 remains in progress. | The design-authority test still requires wider R1–R3, standalone and in-place recovery deferrals. No global gate closure. |
| Packaging | Disabled optional `stacks-chaos-profile` chart; explicitly declared upstream 2.8.4 installation. | Six resources only; no wrapper/controller/CRD vendoring. Helm defaults and required version tested. |
| Admission | Namespace enrollment, exact actor labels, one distinct actor per side, delay/to/one, immutable spec, fixed bounds. | Real API-server positive boundaries and negative selector, timing, lifecycle and spec-update tests using the verified upstream schema. |
| Native lifecycle | Accept only empty creation status, including upstream `experiment: {}`; preserve controller metadata/status updates and deletion. | Upstream has no status subresource. Envtest and installed-webhook live creation pass; live current-generation CEL checking has no warnings; de-enrollment cleanup writes remain admitted. |
| Agent access | Namespaced native get/list/watch/create/delete; token automount off. | Exact rendered Role/binding checks, API-server access reviews, and restricted-identity create/delete. No other fault or orchestration grants. |
| Aggregate bound | One existing NetworkChaos object, including recovered/terminating objects. | Rendered quota checked; a second object is denied by the real quota controller during live injection. Envtest has no quota controller. |
| Control-path separation | Directed actor-to-actor delay leaves the unselected producer path available. | Actor RPC latency increases while destination ledger receipts advance; no constant-latency or total-outage continuity claim. |
| Recovery | Native cancellation, expiry and controller replacement. | Measured latency returns after finalized deletion and native expiry; delay remains measurable after controller replacement before recovery. |
| Independence | External Chaos Mesh release can be removed after native cleanup. | Manual removal: destination receipts 184 → 185 with native controller/daemon absent; network Ready retained; pinned release restored. |
| Verification tooling | Shared rendered-object/schema helpers plus opt-in live tests in `tools/chart-policy`. | Adds client-go/controller-runtime only to this existing isolated tool module. No runtime dependency graphs unified. |

## Checks

The follow-up's full verification and live suite passed on 2026-09-07. The
index file's byte checksum changed during verification, but an independent
comparison through a copied index confirmed the original staged tree remained
`5a319dbeeecd8b17a5d680ca277667d2678206db`. No follow-up content was staged.

- Workflow validation: `actionlint` passed for Verify and Security.
- Full `make verify`: passed using a temporary populated index; staged tree unchanged.
- Build-tagged qualification vet and race-enabled admission envtest: passed.
- `make vuln` and `make docker-check`: passed.
- Actual network-operator Docker build and isolated kind deployment: passed.
- Native delay live suite: passed with the installed upstream webhook/controller/daemon.
- Markdown lint, relative links and whitespace: passed.

[Qualification](../chaos/qualification.md) records exact platform, image IDs,
resource identities, measurements and limitations. Follow-up logs use
`/tmp/stacks-chaos-followup-*.log`; initial logs remain separate. The live suite
creates/deletes native faults and
restarts only the qualification cluster's Chaos Mesh controller. It does not
alter baseline policy or actor workloads. After qualification, baseline production
was separately paused in the task-owned review network. Existing demos were preserved.

## Review focus and remaining scope

Check complete selector validation, the namespace binding versus enrollment
check, admission of upstream defaults, immutable specs, native cleanup writes,
exact additive RBAC, and quota semantics. Duration is a bounded request whose
recovery still depends on upstream infrastructure. Static selectors do not pin
Pod UIDs or prove membership in a live StacksNetwork inventory.

Other native kinds/mechanisms, Stacks traffic combinations, daemon/node loss,
control-path loss, other platforms, dynamic inventory admission and passive
journal/divergence/gap correlation remain deferred. The separate observation
operator was not newly qualified in this pass. Upstream image tags are version
pinned; observed runtime digests record this run, without a cross-architecture
image-lock claim.
