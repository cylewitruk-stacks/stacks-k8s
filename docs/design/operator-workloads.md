# Operator installation and execution workloads

The network chart installs one operator independently of individual networks.
A `StacksNetwork` selects reusable definitions or inline entries; the aggregate
creates owned `StacksNetworkParticipant` instances and freezes `StacksGenesis`.
Domain controllers provision those participants' actors and scoped execution
workloads. Admission stays in the aggregate; no second controller competes for it.

The [public API](public-api/README.md) specifies composition, status ownership and
lifecycle. Actor StatefulSets run protocol binaries. Bitcoin control uses durable
per-target execution records. Stacks management uses exact-Pod, single-use Go
workers; consensus signers and stacker administration run separately.

## Credentials and permissions

The operator reads Secret metadata only. Scoped Jobs generate or inspect immutable
credentials and publish bounded public reports. Each runtime mounts only its
required material. Actor RPC access is separated from Bitcoin mutation authority;
optional action controllers and observers do not receive keys.

Management workers use narrow ServiceAccounts and Kubernetes list/watch channels.
Notifications prompt reconciliation, not an unconditional send. Shared status
objects use distinct SSA field managers and disjoint owned subtrees. Portable
protocol code remains independent of Kubernetes.

## Lifecycle

Cooperative pause prevents new managed sends while preserving the process and
observing existing submissions. Operator restart does not replace bound workers.
Stacks worker loss fails the experiment; Bitcoin unknown dispatch closes its target.
No general supervisor, crash-recovery protocol or nonce lease is implied.

Terminal stop drains workers and scales actors to zero with termination evidence.
Destructive removal respects cleanup and storage retention. Shared genesis/Bitcoin
records remain finalized until participant consumers finish disposal. Independent
accounts/definitions survive root deletion. Follow [operations](../network-operator/operations.md)
for cleanup; remove faults and collect optional evidence before destructive changes.
