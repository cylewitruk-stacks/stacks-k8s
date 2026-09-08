# Initial Bitcoin reorganization contract

Status: implemented constrained R4 amendment; see the
[qualification ledger](../reviews/bitcoin-actions-review.md).
The [finite generation profile](../network-operator/bitcoin-generation.md)
shares the staged implementation base. Historical Lease/elapsed-time recovery
rules do not apply.

## Scope and bounds

`BitcoinReorganization` replaces one local regtest suffix on the baseline's
exact Bitcoin target. Depth is 1–6; it generates exactly `depth + 1` acknowledged
blocks at a minimum one-second interval. Timeout is positive and at most ten
minutes from creation. References and address follow finite generation; the
spec is immutable. All three explicit epoch/reward-cycle/prepare-phase boundary
opt-ins must be true because this profile has no trusted Stacks schedule.
These deliberately smaller bounds replace the historical 144/288-block proposal.

The existing producer remains the sole mutation executor and ledger writer.
The kind's controller owns only its action lifecycle and finalizer. Generation,
reorganization and baseline share exclusion; optional action kinds can be
independently disabled. Selection uses oldest eligible creation time and UID.
Each enabled kind has a 64-object namespace quota; lists must be complete.

Admission requires the normal approved current regtest target plus the extra
typed read methods and explicit reorganization credential profile. It captures
the original tip, retained ancestor, and first removed hash, and rejects known
invalid tips or oversized tip inventories. No arbitrary hash, endpoint, RPC,
wallet, shell command, or plan is accepted in the public spec.

Before a clean terminal stop, the executor records withdrawal in the same
ledger with an optimistic-lock write. A delayed arm then conflicts; an
independent lifecycle deadline read cannot declare pending authorization absent.
The same rule applies to finite generation.

## Mechanism and evidence

1. Reserve and persist action admission/finalizer before mutation.
2. Revalidate the exact original tip and actor identity, then atomically arm
   `invalidateblock` for the first removed hash.
3. Account its matching null receipt and verify the active tip is the ancestor.
4. Generate single blocks, each with durable intent and matching receipt;
   verify contiguous ancestry, height and increasing work against the preceding
   accepted block before permitting another generation call.
5. Arm and account `reconsiderblock` for the same original hash.
6. Observe that all acknowledged replacement blocks form the intended branch,
   remain canonical locally, and exceed the original chainwork.

`Completed` requires all mutation acknowledgements, acknowledged marker cleanup,
and the final local-chain check. It does not assert peer or Stacks convergence.
Progress retains at most seven replacement hashes and bounded chain points.
The cleanup claim is the trusted Core method's acknowledged semantics, not
proof about unknown external writers or an independently attested server.

The [Core 31.1 implementation](https://github.com/bitcoin/bitcoin/blob/v31.1/src/rpc/blockchain.cpp)
defines the invalidation and reconsideration operations. They are normal
regtest test controls; generated history itself remains irreversible.

## R4 cleanup table

| Boundary | Rule |
| --- | --- |
| No invalidation authorization | Deadline/deletion fails the action without cleanup; release after status acknowledgement. |
| Acknowledged invalidation; every call accounted | Stop further generation on cancellation, timeout or definite failure; attempt only the compensating reconsideration on the same admitted runtime. |
| Any Armed call lacks a receipt/local collector | Mark uncertainty, retain exclusion and finalizer, and issue no further mutation, including cleanup. |
| Live receipt arrives after terminal uncertainty | Copy the receipt without revising the outcome; compensation may proceed only after the ledger is Idle and the exact runtime remains admitted. |
| Target identity changes after invalidation | No cleanup on a replacement process; retain cleanup uncertainty and exclusion. |
| Controller restart between accounted calls | Continue from durable facts; never repeat an acknowledged step. |
| Cleanup receipt accounted | Record CleanupComplete; release only after terminal action status acknowledges all receipts and cleanup facts. |
| Whole environment deletion | Explicit abandonment permits finalizer removal without claiming cleanup or server quiescence. |

The action deadline bounds new invalidation/generation authorization. Cleanup
has a separate fixed authorization horizon of `expiresAt + 30s`, never reset by
restart or a delayed reconciliation. Past that horizon no new compensation is
armed; an already authorized receipt can still arrive. These authorization
limits do not expire acknowledged cleanup or prohibit final read-only
verification. Completed replacement receipts may proceed to cleanup within its
remaining authorization window even after the action deadline. Unresolved cleanup needs
a fresh environment. No timeout, Lease expiry, tip observation, or Pod deletion
is used as proof that server work ended.

This is an irreversible finite operation with compensation, not a timed
reversible fault with autonomous server expiry. Bitcoin's marker may persist
through controller loss; the supported profile exposes that uncertainty and
retains the target. It does not claim that timeout automatically restores a
network. Cleanup permission survives action deletion or parent suspension,
but always requires current exact actor identity and known completed prior RPCs.

A live compensation call remains `Recovering` within its separate recovery
window; exhausting that window with no receipt is `Inconclusive`. The common
outcome stays frozen; cleanup evidence may subsequently improve.
No known response may be relabeled as an unknown send merely because status
accounting is temporarily unavailable. The existing bounded receipt collector
and graceful-drain rules apply to all three fixed mutation methods.
