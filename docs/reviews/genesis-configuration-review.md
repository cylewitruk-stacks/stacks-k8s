# Genesis configuration and bootstrap review

## Boundary and scope

Base: `552c349`. Review the complete working tree, including untracked files.
No commit or index changes are part of this delivery.

- Immutable `StacksNetwork.spec.genesis`: named allocations, epoch schedule,
  test-genesis selection and PoX cycle lengths.
- Immutable namespaced `StacksGenesisProfile`: public recipe copied at
  provisioning, with no controller or live reference from a network.
- Shared embedded TOML templates and typed Go rendering for nodes/signers.
- External Go environment, bootstrap and renewal commands. JavaScript only
  performs offline SDK key encoding and transaction signing.
- Pinned Go TOML parser added; unused direct JavaScript network dependency removed.
  No operator RBAC or controller/bootstrap ownership expansion.

## Review ledger

| Area | Evidence | Boundary to check |
| --- | --- | --- |
| API immutability | Real API-server tests reject profile edits and network genesis edits/removal/late addition; ordinary operation and deletion remain possible. | A profile is copied, not watched or referenced at runtime. |
| Shared configuration | Parsed TOML tests compare role-specific nodes' common epochs, cycle lengths and allocations; explicit test-genesis selection is tested. | Complete external raw configurations remain expert inputs, not semantically inspected by controllers. |
| Accounts | SDK-backed Go tests require the matching key for reusable named accounts, preserve profile allocations and check transfer-key separation. | Profiles carry public values only; credentials stay in immutable Secrets/private provisioning output. |
| SDK boundary | Enrollment and extension sign with network discovery disabled; Go checks signed-byte hashes and exact submission receipts. | No Kubernetes, HTTP submission or bootstrap sequencing remains in JavaScript. |
| Bootstrap | Tests reject genesis/account drift, unqualified schedules, reused/foreign/unpaused/reserved ledgers, malformed account reads and nonmatching receipts. | External single-owner helper; uncertain mutations are not replayed. |
| CLI entrypoints | Actual `--help` smoke checks for all three Go commands. | Guards the flag-registration error encountered during live iteration. |
| Compatibility | Current PoX-4 schedule retained; Epoch 4.0/sBTC prerequisites documented as the next delivery. | Arbitrary rendered epoch schedules are not automatically qualified for bootstrap. |

## Validation

The initial delivery’s `make verify` passed in an isolated snapshot with a populated index
(`/tmp/stacks-genesis-release-verify.log`), including generated drift, unit/race,
SDK, CLI entrypoint, API-server, module and chart checks. Earlier snapshots also
passed; the final run includes the provisioning composition cleanup.

`make vuln`, container checks, focused race tests, envtest, offline SDK tests and
Markdown checks passed during implementation. Verification snapshots have their
own populated Git index; the main index is untouched.

## Focused review follow-up

The follow-up addresses findings 1–4, 6 and 7. Finding 5 (future defaulting versus
whole-spec comparison) remains deferred; existing drift detection is unchanged.
All changes remain uncommitted and the main index is untouched.

| Finding | Resolution | Regression evidence |
| --- | --- | --- |
| 1: admitted invalid genesis | Shared `GenesisSpec` markers enforce complete ordered epochs, nondecreasing heights, zero initial heights and unique nonempty balance names. Empty/omitted schedules retain defaults. | Real profile/network admission rejects malformed schedules and duplicate names/addresses; admitted objects pass `ResolveGenesis`. Explicit empty fields are sent through unstructured objects. Maximum-size allocations exercise CEL cost limits. |
| 2: missing miner key | `profiles.Stacks` rejects miner rendering without a key. Existing API/compiler/reconciler SecretRef gates are unchanged; the originally reported controller path was unreachable. | Renderer regression rejects an empty key; valid miner and other-role rendering remains covered. |
| 3: evidence ownership | `Run`/`Maintain` claim operation-specific files exclusively before external mutation. Each record flushes a temporary file and atomically replaces that invocation's snapshot. | Library-entrypoint tests cover defaults, existing files and failed claims; snapshot tests cover retention, permissions, unclaimed writers and replaced-file refusal. |
| 4: fee ownership | Go passes its existing 3000 micro-STX default to the offline adapter. | SDK tests decode a nondefault supplied fee, reject invalid/missing fees, and exercise enrollment/extension through Go. |
| 6: rejection diagnostics | Bootstrap and transfer submission share a pure native-envelope decoder. Only HTTP 400 plus matching TxID, envelope and nonempty reason produces `FeeTooLow`, `BadNonce` or `Other`. | HTTP tests assert one send, bounded classifications, discarded detail and unconfirmed handling for mismatched, malformed, oversized or generic error responses. No retry or nonce-release behavior is added. |
| 7: TOML escaping | The renderer emits TOML 1.0-compatible Unicode control escapes and rejects invalid UTF-8. | Exact expected encodings plus rendered round trips cover NUL, BEL, DEL, whitespace controls, quotes, backslashes and Unicode. |

Name uniqueness uses a name-keyed CEL map: duplicate keys fail admission without
a quadratic pairwise rule. This requires Kubernetes 1.32+; the network chart's
minimum version and docs now state that requirement. Helm checks admit 1.32 and
reject 1.31. API-server evidence uses the pinned 1.37 binaries; this is not a
claim of full runtime qualification on 1.32. The 1,000-allocation limit is retained.

No RBAC, chart templates, dependencies, images, controller ownership or
whole-spec comparison changed in this follow-up. The transfer runtime only
adopts the shared decoder with its existing classification and recovery policy.
No live cluster, actor-parser run, image rollout, vulnerability scan or container
check was repeated; earlier evidence below predates these follow-up changes.

Follow-up validation completed on 2026-09-07 with Go 1.27.1 and Node.js 25.6.1:

| Check | Result |
| --- | --- |
| `make verify` in an isolated snapshot with a populated index | Pass; generated drift, module/vet/unit/race, both operator envtests, chart-policy envtest, SDK/CLI and chart checks. Log: `/tmp/stacks-genesis-followup-verify.log`. |
| Focused race tests: bootstrap, profiles, stacksrpc, transactions | Pass; `/tmp/stacks-genesis-followup-unit.log`. |
| `make -C operators/network test-sdk` | Pass; `/tmp/stacks-genesis-followup-sdk.log`. |
| Changed Markdown and `git diff --check` | Pass; nine Markdown files. |

The complete verification includes the final 1,000-account/63-character-name
cases and explicit empty wire values. Its populated index has no post-generation
drift. The temporary snapshot was removed; logs remain. The final ledger update
was checked separately with Markdown lint.

## Live evidence

Kubernetes 1.37.0, the repository's three-node `stacks-k8s` cluster, and the
previously qualified `stacks-core-attacknet:a12-normal` arm64 artifact were used.
No chaos faults were injected in this refactoring qualification.

The final fresh namespace is `genesis-final`, network UID
`a3439ed2-c5b4-4aa5-ba16-3f2527d86666`. Bootstrap completed at burn height 230,
Stacks height 29, with one exact confirmed transfer. Enrollment TxID:
`ebfdd4462771bfde256f05f11a8479b4ad1a99e49c00ebd304abb4cdb4373c0f`.
Its initial signer unlock height was 460. Native `/v2/pox` reported
`Epoch34` and `pox-4` at burn height 233 before renewal qualification.
The Go maintenance command then submitted nonce 1 and observed unlock height
460 → 580. Renewal TxID:
`a0d5109162a8ffc6571ac583a0fedf2a950845e20da25f35f8534188c2be4be3`.

Network image: `stacks-network-operator:genesis-final`, local content ID
`sha256:eadb3b9b335278a980fef11d462efe94fe2081e2a95d01a0d9e5bca55c46308f`.
Worker image: `stacks-transaction-worker:genesis`, local content ID
`sha256:2b7f0fde925f9e6403b60947e66d23a6c83c8fe8e86566c4673ddcc147f70098`.
The 620-second maintenance command completed successfully. The network had 70
confirmed transfers at the final public snapshot, before draining its pending
transfer. Both `genesis-live` and `genesis-final` namespaces were confirmed absent
before `cluster-destroy` removed all three test nodes. No qualification cluster
or fixture remains. See `/tmp/stacks-genesis-final-teardown.log`.

The last internal Bitcoin-builder cleanup removes positional resource edits and
post-render digest rewriting; its final output contract was rechecked by Go/SDK
tests after this live fixture was provisioned. Upper allocation-bound validation
and removal of an unused renderer input are likewise covered by automated tests.
Neither changes the valid live fixture's protocol settings.

Public logs use `/tmp/stacks-genesis-*.log`; final public bootstrap/PoX/renewal
evidence is under `/tmp/stacks-genesis-final/`. `environment.json` in that
location contains private disposable credentials and must not be committed.

Initial iteration corrections are retained in the logs: a missing local image
repository override, a bootstrap CLI flag-registration collision (before any
mutation), and an older transfer-worker image that correctly rejected the new
API fields as a declaration mismatch. Matching images resolved these issues;
the corrected initial bootstrap completed at height 256 and its namespace was
removed before the final renewal check. No test condition was weakened.

## Fable review prompt

```text
Review the complete stacks-k8s working tree against HEAD 552c349, including
untracked files. Read AGENTS.md, docs/network-operator/configuration.md and this
ledger first. Include the focused follow-up section in your review. Do not stage, commit or modify the main index.

This delivery moves Stacks actor configuration and external bootstrap/renewal
out of JavaScript. Review API/schema immutability; immutable profile copying;
shared genesis rendering and defaults; account/credential consistency; complete
external config limitations; exact-byte digests; offline SDK signing; Go RPC
submission and uncertainty handling; subprocess cleanup and explicit cluster
selection. Check that no controller executes bootstrap or gains Secret access.

Check shared CEL rules on both profile and network CRDs, including explicit
empty fields, 1000 allocations and duplicate names at the size boundary. Check
the documented Kubernetes 1.32 minimum. Evidence ownership/defaults must hold
when calling Run/Maintain directly, before mutation, and preserve existing
files. Check Go-owned fees, matched native rejection classification without
retry, and TOML 1.0 string encodings. Whole-spec drift checks remain unchanged.

PoX-5/sBTC provisioning is explicitly next work. The current bootstrap remains
qualified only for the existing PoX-4 schedule, and must reject other schedules
before mutation. Do not interpret generic epoch fields as bootstrap support.

Run focused tests and full make verify in an isolated snapshot with a populated
index so generated drift is meaningful. Distinguish unit, SDK, API-server and
live evidence. Do not create or alter clusters without explicit authorization.
Report severity-ordered findings with concrete triggers and tests needed.
```
