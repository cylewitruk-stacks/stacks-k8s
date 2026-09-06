# Bitcoin cadence review ledger

## Boundary and scope

Base commit: `15cbdbc76fdbd098ac7819ca32d1796971430317`.
Review all tracked changes and untracked files together. This delivery does not
stage or commit changes.

The network worker adds bounded baseline jitter and all defined finite generation
cadence modes. The action operator retains lifecycle ownership; timing, RPCs,
and receipt accounting remain in the network operator. API/runtime modules,
dependencies, credentials, RBAC, images, and controller flags are unchanged.

The finite spec now requires a typed `cadence` object. This unreleased API and
ledger extension require matching source builds and a fresh environment when
existing actions or reservations use an incompatible schema. No migration or
old-executor compatibility is claimed.

## Focused follow-up

The follow-up starts from local working-tree snapshot
`1e94bc8f659bbe1408b779a711194fa8d5cd1889`, captured before edits with a temporary
index. HEAD and the real index remain unchanged. Review the combined delivery,
including untracked files; no new API, generated, dependency, or RBAC changes
were required for this follow-up.

| Review item | Resolution | Regression evidence |
| --- | --- | --- |
| Unsupported cadence can mine before evaluation fails | Shared complete-cadence validation before selection and arming, including count-one actions and every explicit delay. Invalid pending requests do not reserve. Incompatible idle reservations stop with MechanismFailed. | Validation matrix; zero RPC/preflight or reservation for unsupported actions; baseline and eligible-action progress; reserved counts 0, 1 and 3. |
| A scheduling error can strand a known receipt | Commit receipt, Idle and stop together; preserve any earlier stop reason. API errors still retain the receipt. Release waits for terminal action acknowledgement. | Real collector with fake RPC; committed/uncommitted lost accounting acknowledgements, duplicate accounting, exact count/hash/dispatch/time, preserved earlier stop, and acknowledgement before release. |
| Missing due time waits until timeout | Idle partial reservation stops with MechanismFailed without reconstructing timing. | Known progress retained and released after acknowledgement, even with the target Pod unavailable. |
| Suspension can reuse a sample | Document and retain stable policy-generation/opportunity sampling. Resume resets its time anchor without promising an independent draw. | Suspension leaves root generation unchanged; resumed delay equals the pending sample; retries retain deadline; the next opportunity advances once. |
| Public next-due projection | Deferred; target ledger remains the timing fact source. | Operating guide states the access boundary; no extra status schema. |

Unknown Armed execution still takes precedence over cadence validation, deadline,
and missing-timer handling. Its regression asserts retained dispatch identity,
EffectUncertain, zero new sends, and no release.

The malformed-spec regressions deliberately bypass CEL through fake clients to
exercise defensive executor behavior. Current API-server admission rejects
those objects. No compatibility or migration promise is inferred from these guards.

Follow-up validation passed: full `make verify` with a populated temporary
index, focused unit tests, repeated race tests, Markdown lint and whitespace
checks. Both envtests ran under full verification; the real index hash was
unchanged. No live suite or image rollout was performed.

## Review ledger

| Area | Implemented contract | Evidence |
| --- | --- | --- |
| Baseline timing | Optional integer `jitterSeconds`; inclusive uniform interval ± jitter, every result within 1–86400 seconds. Weighted selection remains independent. | Sampling range/endpoints and fixed weighted buckets under jitter; parent/root schema bounds. |
| Durable scheduler | Persist next deadline with selection; generation changes and resume reset the interval. No catch-up or redraw of committed decisions. Opportunity expiry equals the next sampled deadline. | Lost initial/selection write acknowledgements, replacement reconciler, late scheduling, pause/resume, policy edits. |
| Finite API | Required Immediate, Fixed, Uniform, or Explicit mode. First block immediately eligible. Count 1–100; fixed/uniform gaps 1–60 seconds; explicit gaps 0–60, exactly count minus one entries; timeout ≤10 minutes. | API-server mode/field/sequence bounds, immutable cadence, explicit single-block action, existing count/timeout checks. |
| Receipt-relative timing | Next dispatch timestamp or explicit scheduling-failure stop committed with each non-final receipt; stable action UID/receipt ordinal sampling; microsecond precision rounded upward. Final receipt clears the timer. | All four modes through real reconcilers with fake RPC/client; fractional timestamps; committed/uncommitted accounting acknowledgement loss and duplicate accounting. |
| Exclusion and lifecycle | Serial single-block calls, reservation held during gaps, current identity and deadline gates retained. No replay of unknown Armed calls. | Restart during each cadence, immediate response loss/restart/deadline, explicit delay beyond timeout, existing cancellation, identity, exclusion, and reorganization suites. |
| API timestamp storage | Target ledger retains `nextDispatchAt` through actual API serialization. | Envtest status round trip and finalized cancellation while retaining timing facts. |
| Architecture | Small pure timing package shared by scheduler and finite executor. No new controller, webhook, worker pool, permissions, or dependencies. | Module policy, generated CRDs, chart verification and existing independent-operator tests. |
| Planning and operations | Current guides, examples, charts, design index and roadmap cover cadence. M0.3/M0.4 initial scope is implemented and awaits closure review. | Documentation contracts, links and Markdown lint. |

Uniform samples use a stable internal decision key, not a user-facing replay
seed. Failed uncommitted scheduler writes may move the time anchor forward;
only persisted deadlines and selections are authoritative. Receipts always keep
the original receipt-time anchor across accounting retries. Clock synchronization
remains an operating requirement; delays do not promise an upper latency bound.

## Validation

| Check | Result |
| --- | --- |
| `make verify` | Pass: module checks, generation/drift, vet, unit/race suites, both envtests, Helm, exact RBAC and workload checks. Used a populated temporary Git index; the real index hash was unchanged. |
| `go test -race -count=2 ./internal/cadence ./internal/production ./internal/productionscheduler` in network module | Pass. |
| Network and action envtests | Pass under final follow-up `make verify`. |
| Changed Markdown | Original 15 files and follow-up 4 files pass `markdownlint-cli2`. |
| `git diff --check` | Clean. |

The action contract guard now requires the exact per-kind spec rules: whole-spec
immutability, plus generation's explicit sequence-length constraint. Cadence mode
also has an explicit maximum string length, preserving the existing bounded
spec guard. Neither check was weakened.

Final follow-up logs: `/tmp/stacks-cadence-followup-verify.log` and
`/tmp/stacks-cadence-followup-race.log`.

## Qualification limits

No live suite or image rollout was performed for this delivery. The existing
Docker VM had approximately 11 MB free on its 95 GB filesystem. Existing demos,
images, caches, volumes, and clusters were left intact. The paused multi-target
demo runs the preceding delivery and does not qualify these cadence changes.
Unit/race tests exercise controller behavior with fake RPCs; envtest validates
API-server admission, serialization, and lifecycle semantics. Neither is a new
Bitcoin Core timing or throughput measurement.

Dependencies, Dockerfiles, container-build behavior and RPC methods did not
change, so vulnerability/container checks are not required for this slice.
Live cadence measurement and wider compatibility/platform qualification remain
necessary before extending the supported runtime matrix.

## M0 scope reconciliation

M0.3/M0.4 closure awaits this review of cadence and the consolidated initial
contracts. The served profile covers independent target admission, static RPC
separation, retained ambiguity, finite lifecycle/cleanup, weighted/jittered
baseline, mutable parent policy, bounded facts, and action priority/exclusion.
Continuous eligible actions may starve baseline; no fair-share guarantee is made.

Standalone capabilities, temporary override CRDs, per-target credential profiles,
credential epochs, adapters and in-place ambiguous-call recovery remain deferred.
They are not prerequisites for the served profile. Broader M1 compatibility and
M0.5 protocol/control-path qualification remain open. After closure review, the
next design slice is M0.5 native Chaos Mesh admission and platform qualification.
