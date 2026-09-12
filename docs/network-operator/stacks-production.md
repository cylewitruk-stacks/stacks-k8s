# Stacks steady operation

The network composes consensus actors with independent management participants:

- `StacksNode` supplies a miner or follower process.
- `StacksSigner` supplies the consensus signer binary and its key.
- `StacksStacker` owns holder registration, direct PoX-4/PoX-5 enrollment and renewal.
- `StacksContractSet` deploys and verifies the pinned sBTC contracts and initialization.
- `StacksTransactionProduction` provides ongoing small STX transfers.
- `StacksFaucet` handles explicit bounded funding requests.

A generated network-owned participant carries each instance's admitted policy,
workload identity and execution facts. Reusable definitions are not running workers.
Accounts can be shared as deliberate experiment inputs; independent workers do not
coordinate a shared account's nonce. Use separate accounts for baseline operation.

## Provision and bootstrap

Install the [network chart](../../charts/stacks-network-operator/README.md), load
compatible node/signer images, and apply the
[30-actor composition](../design/public-api/examples/30-actors.yaml) in a fresh
namespace. The root starts paused. Inspect its inputs, then request `Running`.
No external bootstrap or signer-maintenance command is required.

Controllers capture genesis, allocate actors and scoped Go workers, and advance the
[protocol gates](../design/public-api/protocol-timing.md). Workers verify native
transaction inclusion and contract state before reporting completion. Pod readiness,
Bitcoin height and successful transaction submission are not substitutes for those
observations. sBTC deployment is standard initialization; epoch selection comes from
the declared schedule, not a separate PoX-5 network mode.

Transfer interval controls transaction demand rather than exact block cadence.
Miners and signers still determine whether and when blocks are produced. Pausing
traffic can therefore make the root non-operational without changing actor readiness.

Management Pods use `restartPolicy: Never` and exact-Pod activation. A surviving
worker retains its nonce and pending transaction across operator restarts; loss of
that worker fails the experiment. Unknown sends are not reconstructed or retried by
a replacement process. See [runtime status and failure boundaries](public-api-foundation.md).
