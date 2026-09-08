# Operator installation and execution workloads

Status: implemented; locally qualified. See the
[qualification record](../network-operator/operator-workloads-qualification.md).

Helm installs the network operator once. It watches `StacksNetwork` resources
across namespaces by default; `controller.watchNamespace` restricts an installation
to one namespace. Leader election remains in the installation namespace. Do not
run overlapping operator installations over the same resources.

The aggregate compiles actor and capability resources. Actor controllers maintain
StatefulSets. Capability workload controllers maintain execution Deployments,
ServiceAccounts and namespaced permissions. Creating another network requires its
declaration and referenced configuration, not another Helm release.

| Resource | Owned execution workload |
| --- | --- |
| `BitcoinBlockProduction` | Operator scheduler; no RPC credentials |
| `BitcoinProductionTarget` | One Bitcoin RPC worker Deployment |
| `StacksTransactionProduction` | One transfer worker Deployment |
| `StacksContractSet` | One contract worker Deployment |
| `StacksStackingParticipant` | One stacking-administration worker Deployment |
| `StacksNetwork` with managed accounts | One legacy receipt service and Deployment |
| `StacksAccount` | Durable ledger; no Pod |

Workers bind to a resource namespace, name and UID. They re-read current policy,
parent ownership and ledger identity before authorizing effects. Kubernetes
replacement and leader election do not establish server-side quiescence. Existing
CAS authorization, receipt retention and no-resubmission rules remain authoritative.
A worker restart does not reset an account nonce or Bitcoin dispatch record.

Stacking administration stays outside the consensus signer Pod. A signer restart
or protocol fault therefore need not restart its administrator. Go owns runtime
behavior; the existing Node.js adapters only encode and sign offline.

## Credentials and permissions

Production and transfer policies reference a same-namespace `credentialsSecret`.
Bitcoin workers mount only `credentials.json`; transfer workers mount only
`account.json`. Managed workers mount the exact key entries of their assigned
accounts and, for stacking, the declared consensus authorization key. They have no
Secret API permission. `workers.pullSecrets` supplies registry credential names;
provision those Secrets separately in every network namespace. The operator does
not copy them from its installation namespace. Referenced immutable artifacts and their digests remain
part of admission.

Each worker can read the namespace resources needed for admission, but capability
and account writes are restricted to assigned resource names. Receipt workers
hold no signing keys and account only existing transactions for their network UID.
The network-specific receipt Service avoids sharing one fixed endpoint across
independent networks in the same namespace.

The operator needs workload and namespaced RBAC provisioning authority throughout
its watch scope, including the permissions it delegates. This is an administrator
installation, not an untrusted tenant boundary. Network authors must be trusted to
reference Secrets available in their namespace; permission to create workloads can
indirectly expose those Secrets even without Secret API reads.

Capability `WorkerReady` conditions expose missing credential references and
Deployment availability separately from protocol status. They do not authorize
production or assert healthy consensus. Native Deployment status remains available
for rollout diagnostics.

## Lifecycle

Workers use single-replica Deployments with `Recreate` updates and graceful
termination. This reduces overlap but does not replace ledger authority checks.
Pausing desired operation keeps workers available to collect receipts and account
already authorized work. It does not scale them to zero or undo on-chain effects.

Deleting a worker Deployment is repaired from its capability. Deleting a ledger
alone cannot reset its authority. Administrative environment removal uses the
existing abandonment and finalizer rules; garbage collection removes owned
workloads after their owners are released. Operator-side ledger lifecycle
controllers release Bitcoin policy-root, target and transfer retention even when execution workers
are unavailable, preserving an explicit abandoned outcome rather than claiming
cancellation. Unknown effects remain unknown.

Withdrawing managed declarations retains credential-free observation workers for
pinned accounts. They can be recreated to collect and acknowledge outstanding
receipts without restoring signing mounts. Transfer workers retain `account.json`
after policy withdrawal because their process still requires its account profile;
this credential-free behavior applies to managed-operation workers only.

Chart capability switches control provisioning, not revocation of existing work.
The Bitcoin switch also controls baseline scheduling. Use network pause policy
to stop new work while keeping receipt accounting available.

Existing per-namespace installations must be stopped before the new operator
watches their namespaces. Existing ambiguous transactions are not replayed during
migration. A fresh namespace is the preferred qualification boundary.
