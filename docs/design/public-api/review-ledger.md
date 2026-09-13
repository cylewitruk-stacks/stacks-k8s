# Public API review ledger

Design-review snapshot: 2026-09-10. The findings below describe that review boundary;
see the [package overview](README.md) for current implementation status and the
[runtime qualification record](../../network-operator/public-api-qualification.md) for live evidence.
Read the [package](README.md) and [review prompt](fable-prompt.md).

## Boundary

Base HEAD: 6337f6c6f4c93a1ad0bd20cbb8d6ac4d68831ade. The reviewed proposal and
unrelated local-cluster work are staged. This follow-up changes only public-api
Markdown; the three standalone YAML examples are unchanged. No staging, commit,
runtime, generated API, dependency or cluster change.

Cached binary diff SHA-256 at the start of this pass:
`0a6d84c3bc168845b96abe31829426d0908864b790f177d1691ea8d7eff482ef`.

## Independent review and dispositions

Three independent reviewers read only this directory, without conversation history
or current implementation: Kubernetes architecture, technical documentation, and
distributed-systems lifecycle correctness. They reported no critical/high findings
or recommendation to replace the architecture. Their review preceded this follow-up;
the revised text has been self-reviewed, not independently signed off.

| Finding | Severity | Resolution |
| --- | --- | --- |
| LC-1: frozen gate requirements cannot be reconstructed from digests | Medium | StacksGenesis.spec.bootstrap retains public gate requirements within the existing artifact size bound. Complete initial policies are durable before freeze. Conflicting updates defer as a whole until their affected gates finish; a freeze/admission race with no compatible retained policy fails before further mutation. |
| DOC-01: execution-record ownership contradiction | Medium | Controller table specifies network-owned initialization/target execution records; scheduling policy/status stays on the participant. |
| DOC-02: Operational aggregation undefined | Medium | Lifecycle defines current baseline production, canonical traffic progress and contract predicates, False/Unknown precedence and excluded experimental extras. Per-participant health remains visible; this condition authorizes no dispatch. |
| K8S-01: long source names cannot be label values | Medium | Full source name is an annotation/status reference; source kind/UID are bounded provenance labels. Reusable names are not subjected to the participant-name limit. |
| DOC-03: polling/heartbeat setting authority absent | Low | Immutable release-profile runtime.observationPolicy owns polling, RPC allowance, heartbeat and progress grace. Root status exposes those settings; capability observations expose cadence-aware progress windows and last successful progress. No CR tuning field added. |
| DOC-04: historical narrative in normative prose | Low | Timing references the complete schedule; action and observer contracts state their final form. Chaos migration detail resides in implementation notes. |

Fable confirmed these six revisions and supplied the follow-up below. The A1–A6
changes have been self-reviewed; no independent approval of this follow-up is claimed.
No worker crash recovery, retirement API, account lease or new orchestration resource was added.
Previously reviewed destructive apply/removal, placement enforcement, omission-based
defaults, exact Chaos endpoints and disposal contracts remain in place.

### Fable clarification follow-up

| Finding | Resolution |
| --- | --- |
| A1: seed/config-Secret distinctions absent | Explicit/discovered seeds are startup hints; losing one does not withdraw an admitted peer. Resolution does not wait for final peer admission. An explicitly selected validated config Secret can roll an actor while credential identities remain pinned; same-name replacement is not automatic rebinding. |
| A2: deferral terminology | Participant condition PolicyDeferred=True, reason BootstrapPending; walkthrough uses the same form. |
| A3: failure scope | BootstrapPolicyUnavailable latches root Failed=True, withdraws new mutation authorization and requires network recreation, including before activation. Bounded cleanup remains available. |
| A4: paused required baseline | Pausing Bitcoin or transaction production makes Operational=False while root operation can remain Running. |
| A5: verification attribution | The preceding six-fixes command returned exit 0; its log had no exit marker. This run records the command exit code in its log. |
| A6: review boundary | Prompt separates proposal-only review from verification of specifically cited external contracts/sources/templates. A limited review must identify unverified references. |

## Consistency cases

- Freeze amount X, request Y before either affected enrollment gate, restart controller:
  genesis still supplies X and cycle coverage; the complete retained policy remains
  admitted. After all affected gates complete, re-evaluate latest Y. Pause/removal
  remains effective; missing original identities never authorize replacements.
- Operational uses current admitted baselines, not the initial cohort or all selected
  actors. An unfunded late stacker or suspended extra follower is not a direct gate;
  no eligible miner/Bitcoin target, missing baseline, or freshly observed overdue
  progress is False. Missing required observations alone is Unknown.
- Profile freshness is 16s. Progress windows are 130s for the example's 5s Bitcoin
  and 10s traffic cadences, and 10810s for a 1h cadence. An old transaction observed
  again does not become new progress. These are proposed bounds, not measured SLAs.
- Source names longer than 63 characters stay intact in annotations/status; lookup
  uses source UID. Full source names never enter label values.

## Verification and limits

- `make verify`: passed. The final marker in
  `/tmp/stacks-public-api-fable-clarifications-verify.log` records `Verification exit code: 0`.
- Markdownlint: all 10 proposal Markdown files passed.
- Documentation checks: 68 relative links/anchors, 20 YAML fences and 119 resolved
  references passed, including composition/defaulting and the UID-guarded late patch.
- Naming checks: 756 runtime, 63 reusable and 63 pre-UID participant cases passed.
  These formula checks do not qualify generated API-server metadata.
- Unstaged, cached and combined whitespace checks passed. The cached fingerprint is
  unchanged; this follow-up remains unstaged and confined to five Markdown files.

The earlier six-fixes log is prior-pass evidence, not evidence of these clarifications.

The consistency cases above were dry-run against the text, not executed against
new controllers. No cluster was used; dependency/container inputs did not change.

The example counts remain 76/9/5 objects, with 40 initial participants / 30 actors /
49 application Pods / 39 accounts; late additions yield 44 / 33 / 54 / 43. Genesis
has 19 allocations totaling 17012000000000000 microSTX. The worked reuse excerpt
is separate from those counts.

Protocol source/gates are unchanged: Core f9b022bff5550d1e9938e1f40805ea354381a59e,
ceilings 234/251/281/294/299 and first waterfall 300. No external source derivation,
new schema admission, live GC or protocol qualification is claimed. Implementation
must test freeze/update races, retained requirements after controller restart,
Operational truth/freshness cases, long-source metadata and native fault selectors.
