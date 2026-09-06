# Local Bitcoin reorganization

The optional `BitcoinReorganization` action replaces one suffix on the
baseline's exact regtest target. It generates `depth + 1` acknowledged blocks,
clears the original invalidity marker, and verifies the replacement locally.
It does not create partitions, choose peer behavior, or judge Stacks recovery.
The [R4 contract](../design/bitcoin-reorganization.md) defines its execution
and cleanup boundaries.

## Provision and enable

Use a fresh environment with the [baseline helper](bitcoin-production.md),
adding `--reorganization` when generating its private provisioning manifest.
This opt-in adds producer `getblockheader`, `getblockhash`, `getchaintips`,
`invalidateblock`, and `reconsiderblock` permissions. The observer gains only
the corresponding read methods. Actor workloads still receive no mutation
password. Existing immutable baseline configurations are not rewritten.

Keep `bitcoinProduction.enabled=true` on the network chart. First install the
[action chart](../../charts/stacks-action-operator/README.md) in the same namespace
with `bitcoinReorganization.enabled=true`, then enable the matching
`bitcoinReorganization.enabled` flag on the network chart.
`bitcoinGeneration.enabled` is independent on both charts. All enabled actions
share the same producer Deployment, credentials, receipt pool, and durable
execution ledger. The topology controller does not execute actions.

Allow baseline production to establish a sufficiently long original chain,
then submit [the example](../../examples/actions/bitcoin-reorganization.yaml):

```bash
kubectl --kubeconfig "$STACKS_KUBECONFIG" --context "$STACKS_CONTEXT" \
  -n "$STACKS_NAMESPACE" create -f examples/actions/bitcoin-reorganization.yaml
kubectl --kubeconfig "$STACKS_KUBECONFIG" --context "$STACKS_CONTEXT" \
  -n "$STACKS_NAMESPACE" get bitcoinreorganizations,bitcoinblockproductions
```

| Field | Initial bound |
| --- | --- |
| `networkRef.name` | Same-namespace owning network. |
| `bitcoinNodeRef.name` | Exact compiled baseline Bitcoin target. |
| `depth` | 1–6, no greater than the admitted current tip height. |
| `address` | Explicit valid regtest coinbase destination, 14–128 characters. |
| `boundaryPolicy` | All three opt-ins required and true; protocol schedule is unknown. |
| `timeout` | Positive, at most 10 minutes from creation, including pending time. |

The whole spec is immutable. Replacement blocks have a minimum one-second
receipt-relative interval. The affected interval runs from the first removed
block through one height beyond the original tip. Completion requires higher
chainwork; a block count alone does not prove that endpoint.

Each kind has a 64-object namespace quota, including terminal evidence. Export
and delete finished objects when needed. After initial CRD installation, wait
for quota `status.used` before creating actions; Kubernetes discovery/resync can
take several minutes. Queue selection reads complete bounded lists and picks
the oldest eligible action across both kinds, with UID tie-breaking.

## Evidence and interruption

Inspect the action's phase/conditions plus `originalChain`, `forkParent`,
`invalidatedHash`, ordered `replacementBlockHashes`, `finalChain`,
`invalidationAcknowledged`, and `cleanupAcknowledged`. These are bounded
mechanism facts. Admission records the exact leaf/configuration/runtime
identity independently of global network readiness.

`Completed` requires matching receipts for every mutation, contiguous increasing
replacement work, a final canonical check, and acknowledged reconsideration.
`Recovering` means generation stopped and compensation or its verification is
pending. A definite interrupted operation ends `Failed` once cleanup is known;
unknown effects or unsafe cleanup end `Inconclusive` with retained exclusion.
`CleanupComplete=True` means no marker was authorized or its compensation
receipt was accounted. It does not reverse propagated history.

A successful replacement remains the best chain; there is no scheduled rollback.
Cleanup removes the temporary invalidity marker on the original branch, while
the replacement must still win by chainwork. Baseline production then continues
from the current best chain. If interrupted before the replacement wins,
compensation can restore the original branch as best; that action ends `Failed`.
Existing valid forks are allowed, but a later invalidation may expose one with
more work than the intended fork parent. The action then stops and compensates
as `MechanismFailed` instead of mining on an unintended branch.

Deletion cancels new invalidation/generation. Cleanup remains authorized until
`expiresAt + 30s`, only on the same admitted runtime after all prior calls are
accounted. This fixed horizon never resets on restart. Once all replacement receipts are
accounted, passing the action deadline alone does not fail the action. After
cleanup is acknowledged, final read-only verification can finish beyond either
authorization deadline. After acknowledged cleanup, admission-read failures wait
until `expiresAt + 30s`.
A successfully read changed identity stops immediately; a read failure remaining
at that horizon ends `Inconclusive/IdentityDiverged` with `CleanupComplete=True`.
Release still waits for terminal acknowledgement. Successful final reads remain
allowed beyond that horizon. No additional cleanup is authorized. An in-flight receipt can
still arrive later; outcome and `finishedAt` stay frozen while progress/cleanup
evidence can improve. A response loss never causes a mutation retry or a
speculative cleanup call.

Controller loss may leave Bitcoin's marker in place. There is no autonomous
server expiry or in-place reset promise. Unknown server work, replaced runtime,
or exhausted cleanup authorization retains the reservation and safety finalizer.
Neither a visible valid fork nor a client timeout authorizes another call.
Preserve evidence and use a new namespace, fresh credentials and fresh data.

Delete the owning network while controllers are available, wait for the ledger
and action finalizers, then remove the chart/namespace. This is administrative
abandonment, not successful compensation. Keep the reorganization-enabled
executor until obligations resolve or the environment is abandoned. Disabling
it retains its reservation as `Blocked`; older ledger-unaware binaries are
unsupported on active environments.

See the [review and qualification ledger](../reviews/bitcoin-actions-review.md)
for verified cases and remaining limits.
