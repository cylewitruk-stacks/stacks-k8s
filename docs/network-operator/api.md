# Network API

The served composition contract is `network.stacks.org/v1alpha2`, with reusable
Bitcoin and Stacks definitions under `bitcoin.stacks.org/v1alpha2` and
`stacks.stacks.org/v1alpha2`.

| Resource | Responsibility |
| --- | --- |
| `StacksNetwork` | Select definitions/inline entries, resolve topology, request operation |
| `StacksNetworkParticipant` | Generated instance, admitted policy, workload and execution status |
| `StacksGenesis` | Generated immutable chain inputs and captured bootstrap requirements |
| `StacksAccount`, `BitcoinWallet` | Reusable identity and key references; no network binding |
| `StacksEpochSchedule` | Reusable epoch activation inputs |
| `BitcoinNode`, `StacksNode`, `StacksSigner` | Reusable actor definitions |
| `BitcoinBlockProduction`, `BitcoinBlockSchedule` | Production and timing policy |
| `StacksStacker`, `StacksContractSet`, `StacksTransactionProduction`, `StacksFaucet` | Reusable managed capability definitions |
| `StacksFaucetRequest` | One bounded request tied to a network and faucet instance |
| `BitcoinExecution`, `BitcoinInitialization` | Generated Bitcoin execution and initialization facts |

Use the [resource reference](../design/public-api/resources.md),
[composition rules](../design/public-api/composition.md) and
[examples](../design/public-api/examples/) for field-level contracts.
[Generated CRDs](../../charts/stacks-network-operator/crds/) define served schema
validation. [Runtime status](public-api-foundation.md#network-status) distinguishes
resolution, initialization, running operation and protocol health.

`BitcoinNode.spec.observer` optionally enables a network-owned read-only RPC exporter.
The participant's `status.runtime.observerRPCSecretRef` pins its scoped credential.
See [Bitcoin observation](bitcoin-observer.md) for settings, lifecycle and metric/log contracts.
