# Steady Stacks transfers

The initial profile combines the existing Bitcoin baseline with a funded
Stacks miner, one signer-node and signer, managed PoX-4/5 enrollment, and a
separate `StacksTransactionProduction` worker. It offers one small STX transfer
per configured interval without requiring actions, Chaos Mesh, or observability.

## Policy and evidence

Declare the policy through `StacksNetwork.spec.stacksTransactionProduction`:

```yaml
stacksTransactionProduction:
  credentialsSecret: stacks-transaction-account
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

The administrator selects the worker image through `workers.sdkImage` in the
operator chart. The network policy selects its same-namespace `credentialsSecret`.
The immutable `account.json` binds a
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

Pause the network policy to stop new signing while retaining receipt observation.
The operator recreates a deleted worker Deployment while its capability exists.
Disabling capability workload reconciliation does not revoke existing workers.
Existing signed transactions may still execute. Initial rotation means a fresh
account and isolated environment;
there is no in-place key rotation, protocol-level key revocation, or claim that
Pod deletion fences a paused former process. Never reuse an old namespace,
account, data, or credential profile for recovery. Wider rotation and delegated
signing contracts remain open.

## Provision and bootstrap

Reuse an explicitly selected cluster and **a fresh namespace for each independent
environment**. Build/load the operator and SDK worker images, and choose a Stacks
image that supports the configured epochs and native transaction index. See
[custom actor images](actor-images.md) and the
[managed-operation qualification](managed-operation-qualification.md).

Obtain the pinned upstream sources described in [PoX-5 operation](pox5.md).
Then, from the repository root:

```bash
docker build -f operators/network/Dockerfile -t stacks-network-operator:local .
docker build -f operators/network/transactions/Dockerfile \
  -t stacks-transaction-worker:local .
npm --prefix operators/network/transactions ci --ignore-scripts

export STACKS_KIND_CLUSTER="${STACKS_KIND_CLUSTER:-stacks-k8s}"
export STACKS_KUBECONFIG="${STACKS_KUBECONFIG:-$PWD/tools/local-cluster/kubeconfig}"
export STACKS_CONTEXT="${STACKS_CONTEXT:-kind-$STACKS_KIND_CLUSTER}"
export STACKS_NAMESPACE=stacks-baseline # Choose an unused namespace.
export STACKS_IMAGE="${STACKS_IMAGE:-YOUR_QUALIFIED_STACKS_IMAGE}"
export STACKS_SBTC_CONTRACTS=/path/to/sbtc/contracts/contracts
export STACKS_MANIFEST=/tmp/stacks-environment.json

kind load docker-image stacks-network-operator:local stacks-transaction-worker:local \
  --name "$STACKS_KIND_CLUSTER"
kubectl --kubeconfig "$STACKS_KUBECONFIG" --context "$STACKS_CONTEXT" \
  apply -f charts/stacks-network-operator/crds
umask 077
go -C operators/network run ./cmd/stacks-environment \
  --namespace="$STACKS_NAMESPACE" --stacks-image="$STACKS_IMAGE" \
  --sbtc-contracts="$STACKS_SBTC_CONTRACTS" > "$STACKS_MANIFEST"
kubectl --kubeconfig "$STACKS_KUBECONFIG" --context "$STACKS_CONTEXT" \
  create -f "$STACKS_MANIFEST"
# Install once per cluster, independently of the environment namespace.
helm upgrade --install stacks charts/stacks-network-operator \
  --kubeconfig "$STACKS_KUBECONFIG" --kube-context "$STACKS_CONTEXT" \
  --namespace stacks-network-system --create-namespace \
  --set image.repository=stacks-network-operator --set image.tag=local \
  --set workers.sdkImage.tag=local
```

For subsequent networks, repeat only provisioning and `kubectl create` in a fresh
namespace; do not repeat Helm installation. No bootstrap or maintenance command
follows resource creation. Controllers prepare
the watch-only miner wallet, advance the initial chain on the existing Bitcoin
executor, enroll PoX-4 participation, deploy contract prerequisites and maintain
PoX-5 participation. Initial blocks are included in `blocksProduced`; scheduler
opportunities remain separate accounting. Transfers start at the declared
`minimumBurnHeight`, after Nakamoto activation in this profile.

The private manifest retains a local `bootstrap` identity record for inspection;
`kubectl create` applies its Kubernetes List items. It is not an execution plan.
Managed keys are placed in separate immutable Secrets. The older external Go
bootstrap/maintenance commands reject managed declarations, and the provisioner
no longer creates a competing Bitcoin bootstrap credential.

Inspect `StacksAccount`, `StacksContractSet`, `StacksStackingParticipant`, and
`StacksTransactionProduction` status. `StacksNetwork` reports `Operational`
separately from topology readiness. Ready capability status does not prove
sustained consensus progress; observe native chain tips and confirmations over
time. See [configuration ownership](configuration.md) and [PoX-5 operation](pox5.md).

## Teardown

Capture the Bitcoin target names and managed account names before deleting the
network, including retained resources removed from current desired policy.
Delete the parent and wait for its Stacks account ledgers, transaction ledger,
Bitcoin production policy and **all retained Bitcoin target ledgers** to disappear
before removing the environment namespace. Keep the shared operator installed for
other networks; uninstall it only after disposing of all managed environments. Follow the
[Bitcoin teardown procedure](bitcoin-production.md#dispatch-state-and-recovery).

Controllers record administrative abandonment and release their own finalizers.
This does not cancel an already signed transaction or prove Bitcoin RPC
quiescence. Keep the workers running until ledger removal completes.

## PoX-5 support

The [direct PoX-5 capabilities](pox5.md) use the epoch schedule to select protocol
behavior. Standard provisioning always includes sBTC deployment artifacts;
there is no `--pox5` switch. Real bridge daemons and delegated pool administration
remain separate, unimplemented capabilities.
