# Finite Bitcoin block generation

`actions.stacks.org/v1alpha1` `BitcoinBlockGeneration` requests a bounded
number of single-block regtest RPCs. Its `Completed` outcome means the requested
receipts were accounted, without asserting chain adoption or a Stacks outcome.
It is optional; ordinary [Bitcoin production](bitcoin-production.md) works
with the action controller disabled.

## Enable and request

Provision the baseline profile with `bitcoinProduction.enabled=true` and action
selection disabled. Install the
[action chart](../../charts/stacks-action-operator/README.md) in the same namespace
with its default generation controller enabled, then enable
`bitcoinGeneration.enabled=true` on the network chart release. The network worker remains the
sole RPC executor; the separate action Deployment owns status and finalizers
and receives no mutation credentials.

Create [the example](../../examples/actions/bitcoin-generation.yaml) in the
network's namespace:

```bash
kubectl --kubeconfig "$STACKS_KUBECONFIG" --context "$STACKS_CONTEXT" \
  -n "$STACKS_NAMESPACE" create -f examples/actions/bitcoin-generation.yaml
kubectl --kubeconfig "$STACKS_KUBECONFIG" --context "$STACKS_CONTEXT" \
  -n "$STACKS_NAMESPACE" get bitcoinblockgeneration three-blocks -o yaml
```

| Spec field | Meaning and bound |
| --- | --- |
| `networkRef.name` | Same-namespace owning `StacksNetwork`. |
| `bitcoinNodeRef.name` | Exact compiled `BitcoinNode` name, matching the retained baseline target. |
| `count` | 1–100 acknowledged single-block requests. |
| `cadence` | Required timing mode; see the cadence table below. |
| `address` | Explicit valid regtest coinbase destination, 14–128 characters. |
| `timeout` | Positive Kubernetes duration, at most 10 minutes from object creation, including pending time. |

The whole spec is immutable. Delete to cancel; create a new object for a new
request. The action chart installs a namespace quota of 64 action objects, including
terminal records. After first CRD installation, Kubernetes may reject creates
with `status unknown for quota` until its quota controller discovers the kind
and populates `status.used`; wait for that initialization before submitting.
Export and delete finished records when capacity is needed.
The executor rejects truncated or oversized queue inventories rather than
silently choosing from a partial list; baseline work remains available.

## Cadence

| `cadence.mode` | Additional fields | Delay before each subsequent block |
| --- | --- | --- |
| `Immediate` | None. | No intentional delay. |
| `Fixed` | `intervalSeconds`: 1–60. | The specified interval. |
| `Uniform` | `minSeconds`, `maxSeconds`: 1–60, minimum ≤ maximum. | An independent inclusive integer-second uniform sample. |
| `Explicit` | `delaysSeconds`: exactly `count − 1` values in 0–60. Omit for count 1. | The next entry in order. |

Fields belonging to other modes are rejected. The executor also validates the
complete decoded cadence and count before reservation and before arming.
Unsupported requests remain pending without reserving a target or blocking
eligible work; an incompatible existing idle reservation stops as
`MechanismFailed`. This guard does not provide schema-skew compatibility.

The first block is immediately
eligible after admission in every mode. Immediate generation still uses one
single-block RPC at a time, with authorization and accounting between calls;
controller, API, and RPC latency set its achievable rate.

The executor writes `status.action.nextDispatchAt` on the target ledger in the
same optimistic-lock write as each non-final receipt. The timestamp retains
microsecond precision, rounded upward. Uniform sampling uses the action UID
and receipt ordinal as a stable decision key, so accounting retries do not
redraw a delay or move its anchor from receipt time to accounting time. This
is internal sampling, not a user-facing replay seed or a deterministic outcome
promise. The next due time is cleared with the final receipt.

If subsequent scheduling fails after a successful RPC, accounting still commits
the known receipt and an explicit `MechanismFailed` stop, preserving any earlier
stop reason. API write failures retain the receipt for the existing accounting
retry path. The action must acknowledge terminal progress before release.
An idle partial reservation missing its due time also stops explicitly; the
executor does not reconstruct that timer. Unknown `Armed` work remains excluded.
The due time is exposed on the target ledger, not copied into action status.

Restart during an accounted gap preserves the due time. A late reconcile can
issue only the next single-block request; the following delay anchors to its
receipt, without catch-up. Temporary preflight failures keep the same due time.
A due time grants no authority after cancellation, expiry, suspension, identity
change, or an unresolved call. The target stays reserved during all gaps.

Choose a timeout allowing pending time, delays, and RPC overhead. Admission
does not promise that every requested schedule can complete within its timeout;
the existing deadline can stop a long sequence with known partial progress.

## Admission and shared execution

This profile defines R1–R3 for finite generation. Admission joins the current parent
declaration catalog to the exact ready leaf, immutable approved configuration,
StatefulSet revision, Pod, and container identity using uncached reads. Global
inventory readiness is unnecessary. Specs cannot choose endpoints or credentials.
`admittedNetwork` records name, UID, and declaration generation;
`admittedTarget` records leaf/configuration/runtime identity; `admittedPolicy`
records the execution-ledger UID/generation and approved configuration digest.
It does not claim that an `ActionSafetyPolicy` exists.

The producer selects the oldest eligible request, breaking creation-time ties
by UID. Invalid or unavailable requests do not reserve the target. Admission
requires a compiled baseline policy; that policy may be paused. A reservation
lives in the existing UID-pinned `BitcoinProductionTarget.status.action`.
Only the production controller writes that ledger and issues RPCs. Only the
kind's lifecycle controller writes action status and its finalizer.

Before the first call, the action controller must persist admission and
`actions.stacks.org/action-cleanup`. Each authorization records the action
owner and `Armed` intent in one optimistic-lock ledger write. The reservation
excludes baseline generation through inter-block gaps and receipt accounting.
The action cannot follow a changed admitted runtime. Temporary unavailability
waits without dispatch; a different current identity stops the action. Read-only
preflight failures are retried within the existing action deadline. A successful
read that rejects regtest or the destination stops the action as `MechanismFailed`.

The collector accounts each matching receipt atomically into action progress,
without incrementing baseline `blocksProduced`. Action status acknowledges
`blocksGenerated`, `lastBlockHash`, and `lastDispatchID`. The executor releases
only after a terminal action acknowledges all known receipts and no call is
unresolved. These fields retain the latest receipt and a counter, not a full
block journal. Future baseline dispatches require a fresh weighted policy opportunity.

New generation and reorganization actions require a target selected by the
current compiled production policy. Removing a target retains its ledger but
does not admit new actions. Already-admitted work retains its existing bounds
and cleanup obligations; retaining the ledger does not grant new authority.

Baseline pause, cadence, destination, or policy removal does not rewrite an
admitted action. Parent suspension stops its future authorizations. After
release, the executor evaluates the **latest** baseline declaration; it never
restores a saved policy snapshot. The aggregate continues to update other
actors independently.

## Outcomes, cancellation, and recovery

| Phase | Meaning |
| --- | --- |
| `Pending` | Waiting for an eligible target and executor before the creation-relative deadline. |
| `Admitted` | Exact identities and reservation persisted; no call armed yet. |
| `Active` | At least one call authorized; finite work is in progress. |
| `Completed` | All requested receipts arrived within the deadline and before cancellation. |
| `Failed` | Definite stop with known partial or zero progress, without an unresolved call. |
| `Inconclusive` | An authorized effect or admitted identity became uncertain. |

The common conditions are `Admitted`, `Progressing`, and `EffectObserved`;
`CleanupComplete` is absent because generated blocks are irreversible.
`ActionCancelled` supplements the common reason vocabulary for deletion.
Deletion may advance Kubernetes metadata generation; UID and the immutable spec
preserve action identity. An idle cancellation records the executor stop and
waits for terminal receipt acknowledgement before releasing the reservation.
`startedAt` marks the first durable authorization, which may be committed even
if its acknowledgement is lost before any send begins.

Deadline or deletion stops new authorizations, without cancelling receipt
collection or pretending to undo blocks. A terminal outcome and `finishedAt`
remain frozen. A late receipt can still update progress; it cannot turn a
terminal uncertainty into success. Terminal conditions describe that outcome,
while progress records subsequent receipt facts.

Collectors retain the baseline profile's limits: no normal read deadline,
ten-second preflight, three-second dial, 32 receipt slots, five-second accounting
attempts, and 25-second graceful drain within the 30-second manager/45-second
Pod allowance. A successful drain or restart between accounted calls preserves
progress. Lost arm-write acknowledgements conservatively close the target even
when the live process sent nothing. An `Armed` request without a surviving
collector or durable receipt stays excluded across restart, deletion, and
expiry. No mutation is retried or inferred successful from chain height.

Recreate an unresolved environment in a **new namespace with fresh credentials
and no reused data**. Delete its owning network while controllers are running,
wait for action and ledger finalizers to clear, then uninstall and delete the
namespace. Administrative abandonment removes retention obligations without
claiming server quiescence. Never force-clear a ledger to resume an old network.
If a ledger was forcibly lost, the parent's permanent UID pin still prevents
replacement; action finalizer removal is not readmission.

Keep the action-enabled executor installed until reservations resolve or the
environment is abandoned. Disabling the flag retains existing reservations as
`Blocked`. Downgrading to a binary predating this ledger extension is unsupported.
Do not reuse an active environment across incompatible executor versions.

The separately enabled [reorganization profile](bitcoin-reorganization.md)
uses the same exclusion boundary. Credential epochs, server fencing, and
in-place recovery remain deferred.
See the [action qualification](../reviews/bitcoin-actions-review.md) and
[cadence review ledger](../reviews/bitcoin-cadence-review.md).
