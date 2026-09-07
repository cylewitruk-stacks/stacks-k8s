# Steady Stacks transfers

The initial profile combines the existing Bitcoin baseline with a funded
Stacks miner, one signer-node and signer, external PoX-4 enrollment, and a
separate `StacksTransactionProduction` worker. It offers one small STX transfer
per configured interval without requiring actions, Chaos Mesh, or observability.

## Policy and evidence

Declare the policy through `StacksNetwork.spec.stacksTransactionProduction`:

```yaml
stacksTransactionProduction:
  target: signer-node
  sender: ST_YOUR_PROVISIONED_ACCOUNT
  recipient: ST_YOUR_RECIPIENT
  amountMicroSTX: 1
  feeMicroSTX: 1000
  intervalSeconds: 10
  paused: false
```

Use actual testnet addresses; the placeholders above are not valid addresses.
The offered interval is not a guaranteed Stacks block interval. Miner scheduling,
Bitcoin tenures, signer participation, and transaction processing also affect
blocks. An outstanding transaction applies backpressure: there is no queued
catch-up burst or automatic fee/rate escalation. The next opportunity is at
least `intervalSeconds` after the preceding authorization and after its exact
successful inclusion has been accounted.

The aggregate owns one same-name `stacks.stacks.org/v1alpha1`
`StacksTransactionProduction` and permanently pins its UID. Its controller owns
status and submission. Direct target/sender changes are rejected; removing and
re-adding the policy cannot change the retained ledger's identity. Permanent
cadence, destination, amount, fee and pause changes go through the parent and
apply to future transfers. Removing the declaration pauses new production.
Production errors do not stop actor convergence, and unrelated actor failures
do not stop an independently admitted ingress.

| Phase | Meaning |
| --- | --- |
| `Waiting` | Funding, current identity, indexing, or signing prerequisites are unavailable. |
| `Paused` | Parent intent stops new transfers. |
| `Pending` | Core acknowledged the exact TxID; canonical inclusion remains pending. |
| `Ambiguous` | Submission or observation is uncertain; the existing nonce remains reserved. |
| `Running` | Exact successful inclusion was accounted; the next interval may be pending. |
| `Blocked` | Matched ingress rejection, failed execution, or nonce mismatch prevents new work; nonce mismatch is re-evaluated. |
| `Abandoned` | The owning environment was removed; cancellation is not claimed. |

The worker signs offline with explicit nonce and fee, computes the transaction
ID, then reserves that ID and nonce by an optimistic-lock status write before
one POST. No redirect, proxy discovery, or possible-delivery retry is allowed.
Core requires a fixed-length binary request body; it remains non-replayable in
the HTTP client. A lost authorization acknowledgement sends nothing and leaves
the ledger unresolved. There is no automatic disarm or resubmission.

A native HTTP 400 `transaction rejected` response is classified only when its
TxID matches the reserved transaction and it contains a reason. The worker
persists `rejectionReason` as `FeeTooLow`, `BadNonce`, or `Other`, discards raw
server details, and reports `Blocked`. This means this ingress rejected this
submission, not that the transaction can never execute. The nonce remains
reserved and exact inclusion is still observed, including after restart or
pause. A later exact successful inclusion may be accounted normally. Rejection
evidence stays associated with the last TxID until the next authorization.
Malformed or unmatched responses remain ambiguous. If the rejection status write does not commit,
its reason can be lost even without a restart; the ledger remains ambiguous
and its durable reservation still prevents resubmission.

A lost submission response or worker restart does not discard the known TxID.
The worker queries Core's native `/v3/transaction/{txid}` endpoint, verifies the
returned signed bytes hash to that ID, requires canonical membership and the
exact successful transfer result, and accounts the nonce once. Account nonce
advancement alone never proves this transaction executed. After initial nonce
selection, unexpected canonical nonce changes stop production rather than
silently adopting external traffic or a reorganization. This `Blocked` nonce
mismatch is re-evaluated every two seconds and resumes only when the ingress
reports the exact expected nonce. A lower nonce may reflect lag or a
reorganization; the worker does not infer which. `nextNonce` equals `nonce`
while that transaction is outstanding and becomes `nonce + 1` after exact
successful inclusion.

`confirmed` counts successful canonical inclusions observed at accounting time;
it is not a permanent finality or current-chain-membership claim. Status retains
only the latest transaction, inclusion block ID, timestamps, and a counter.
Pause and policy removal preserve outstanding observation. Parent suspension or
loss of current ingress identity may delay that observation. No new transfer is
authorized while the account is reserved. A transaction that disappears or was
never sent can remain unresolved indefinitely; use a fresh environment.

## Signing and admission

The administrator selects the worker image and account Secret through chart
values, not public capability specs. The immutable `account.json` binds a
single sender to a network name and an approved ingress configuration digest.
The helper generates a fresh account used only for transfer demand. It is
separate from miner keys, signer/enrollment keys, Bitcoin RPC principals, and
all other networks. Sharing it with another producer or external wallet is
outside the profile.

Only the transfer Deployment mounts this account Secret. It uses a separate
ServiceAccount with no Secret-read API or workload-mutation permissions. The Go
controller passes explicit public parameters to a packaged offline SDK signer;
signing material never enters specs, status, logs, or evidence. The generated
local manifest contains private provisioning inputs and must be protected.

Admission reads the current parent declaration catalog, owned Stacks leaf,
configuration digest, StatefulSet, Pod, revision, image and readiness directly.
Requests use that Pod IP. The approved raw ingress configuration enables
`txindex = true`; preflight verifies testnet identity, native transaction
indexing and available account state before signing. Kubernetes readiness alone
is not protocol readiness. The profile trusts the selected disposable node's
RPC facts; it does not prove server-process identity cryptographically.

Disable `stacksTransactions.enabled` and wait for the worker Pod to terminate
to withdraw further worker access to the key. Existing signed transactions may
still execute. Initial rotation means a fresh account and isolated environment;
there is no in-place key rotation, protocol-level key revocation, or claim that
Pod deletion fences a paused former process. Never reuse an old namespace,
account, data, or credential profile for recovery. Wider rotation and delegated
signing contracts remain open.

## Provision and bootstrap

Build the normal network operator image and the separate transfer worker from
the repository root. Select a dedicated cluster and load both images there.
The Stacks node image must provide `stacks-node` and `stacks-signer` and support
the qualified configuration and native transaction endpoint. See the
[qualification record](stacks-qualification.md) for the tested image.

```bash
docker build -f operators/network/Dockerfile -t stacks-network-operator:local .
docker build -f operators/network/transactions/Dockerfile \
  -t stacks-transaction-worker:local .
npm --prefix operators/network/transactions ci --ignore-scripts

export STACKS_KUBECONFIG=/absolute/path/to/dedicated-kubeconfig
export STACKS_CONTEXT=kind-YOUR_DEDICATED_CLUSTER
export STACKS_NAMESPACE=stacks-baseline
export STACKS_IMAGE=YOUR_QUALIFIED_STACKS_IMAGE

kubectl --kubeconfig "$STACKS_KUBECONFIG" --context "$STACKS_CONTEXT" \
  apply -f charts/stacks-network-operator/crds
umask 077
go -C operators/network run ./cmd/stacks-environment \
  --namespace="$STACKS_NAMESPACE" --stacks-image="$STACKS_IMAGE" \
  > /tmp/stacks-environment.json
kubectl --kubeconfig "$STACKS_KUBECONFIG" --context "$STACKS_CONTEXT" \
  create -f /tmp/stacks-environment.json
helm install stacks charts/stacks-network-operator \
  --kubeconfig "$STACKS_KUBECONFIG" --kube-context "$STACKS_CONTEXT" \
  --namespace "$STACKS_NAMESPACE" \
  --set image.repository=stacks-network-operator --set image.tag=local \
  --set bitcoinProduction.enabled=true --set stacksTransactions.enabled=true \
  --set stacksTransactions.image.tag=local
go -C operators/network run ./cmd/stacks-bootstrap \
  --manifest=/tmp/stacks-environment.json \
  --kubeconfig="$STACKS_KUBECONFIG" --context="$STACKS_CONTEXT"
```

The Go generator shares the Bitcoin provisioning library and the operator’s TOML
templates. Node.js is used only for offline SDK key encoding and signing.
See [network configuration and genesis](configuration.md) for reusable profiles.
Its output has an additional local `bootstrap` document; `kubectl create` applies
only the Kubernetes List items. No bootstrap credential Secret is mounted in
actor Pods. The external helper exclusively initializes a height-zero Bitcoin
node while the never-used production ledger is paused: create a watch-only
miner wallet, import its destination descriptor, and generate 201 blocks in one
bounded call. Those startup blocks are recorded separately from baseline counts.
Do not edit production or run another bootstrap client concurrently. An
interrupted or ambiguous bootstrap must be investigated; the helper refuses to
repeat initialization on an already-advanced chain.

The helper then enables the actors, resumes Bitcoin, submits one PoX-4
registration, observes its lock, and enables transfers. Completion requires an
exact confirmed transfer. It has bounded waits and writes separate bootstrap
evidence; it is an external client, not a workflow embedded in a reconciler.

The compressed profile starts Nakamoto epochs around Bitcoin height 223 and
holds Epoch 4/PoX-5 outside its initial operating window. The initial signer
lock covers 12 reward cycles of 20 Bitcoin blocks each: approximately 20 minutes
at the default five-second Bitcoin interval. Actual wall-clock duration depends
on Bitcoin progress and reward-cycle boundaries. For longer experiments, run the external renewal
helper through an explicitly forwarded signer-node RPC endpoint:

```bash
go -C operators/network run ./cmd/stacks-maintain-signers \
  --manifest=/tmp/stacks-environment.json --stacks-port=20443 \
  --duration-seconds=3600
```

It offers one six-cycle extension when the remaining lock horizon is at most
six cycles (approximately 10 minutes remaining at the default cadence), records
the submitted TxID and observed unlock-height increase, and
stops on ambiguous submission or timeout. Only one enrollment/renewal helper
may own the signer account at a time. Stopping it eventually lets the signer
lock expire; transfer traffic alone cannot maintain signer participation.
PoX-5 transitions and automatic re-enrollment are not part of this profile.

## Teardown

Delete the parent and wait for the Stacks transaction ledger, Bitcoin production
policy, and **all retained Bitcoin target ledgers** to disappear before
uninstalling the chart or deleting the namespace. Follow the
[Bitcoin teardown procedure](bitcoin-production.md#dispatch-state-and-recovery)
to include targets removed from the current policy. Controllers release
their own ledger finalizers during explicit environment abandonment. This does
not cancel already-signed transfers or prove Bitcoin RPC quiescence. Removing
controllers first can strand finalizers and requires administrator disposal.
