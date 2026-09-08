# Managed network operation

Status: Initial profile implemented and qualified. This contract replaces the
external-bootstrap requirement; see the [review ledger](../reviews/managed-operation-review.md).

`StacksNetwork` converges toward a functioning network throughout its lifetime.
Initialization and protocol maintenance are desired operation. Experiment
sequencing, fault selection and interpretation remain outside the controllers.

## Responsibilities

| Component | Responsibility |
| --- | --- |
| Aggregate controller | Compile owned actor and capability declarations; report topology and operation separately. |
| Bitcoin producer | Own every baseline/initialization generation request and its receipt; honor prerequisite holds before new dispatch. |
| Contract controller | Establish the declared contract sources and explicit initial bridge state when their language version is supported. |
| Stacking controller | Establish and renew a declared direct participant's lock and required signer-manager registration. |
| Account ledger | Serialize transactions for one explicitly managed account and retain submission identity across controller restarts. |

Immutable genesis funding does not enroll an account in any capability. Users
may fund unused accounts and later introduce participants. Controllers do not
adopt unrelated resources or revert unrelated on-chain effects.

The initial supported participant profile remains direct PoX-4/PoX-5 stacking
with separate holder, administrator and consensus identities. The bridge uses
explicit initialization of the pinned real contracts; real sBTC signing and
delegated pools remain separate capabilities. There is no PoX-5 network mode:
genesis declares bindings, and the epoch schedule determines activation.

## Transactions and recovery

Each managed account has one capability consumer and one retained ledger bound
to its parent UID, ingress and key identity. A capability supplies a monotonic
operation ordinal and explicit signing inputs. The ledger records the exact
TxID and nonce with an optimistic-lock write before the only submission attempt.
An old ordinal cannot authorize a transaction after a newer operation.

After restart, an authorized transaction is observed by TxID; it is never
silently replaced or resubmitted. Exact canonical execution records its receipt
and advances the expected nonce, including unsuccessful execution. The consumer
must durably acknowledge that receipt before another operation may reserve the
account. Receipt observation continues while new work is paused. Missing or
replaced ledgers do not reopen account authority.

Canonical observations are not permanent finality. Reorganizations change the
chain on which operation continues; controllers inspect the resulting state.
If nonce or transaction identity cannot be established, they report the unmet
condition instead of guessing a recovery transaction.

## Startup and maintenance

Startup dependency holds govern effective dispatch; controllers do not overwrite the
user's desired pause settings. Initial Bitcoin wallet/address preparation and
chain advancement use the Bitcoin production authority. Stacks capabilities
observe the active protocol before selecting an enrollment or maintenance
operation. Contract prerequisites are established as early as supported and
verified before allowing advancement into an epoch that requires them. Completed
startup stages remain acknowledged across restarts and later degradation;
ongoing protocol health does not silently become a Bitcoin production gate.

The standard provisioning command emits configuration, scoped credential
Secrets and managed capability declarations. It does not monitor heights or
sequence transactions. Installing the configured controllers and applying that
declaration is sufficient to start and maintain the supported network.

Topology readiness is distinct from operational readiness. Status must expose
the blocking prerequisite, pending transaction and current protocol observation.
Readiness alone does not prove sustained block production; qualification
observes new chain progress across reward cycles and successful lock renewal.

## Worker placement

The chart installs the operator independently of networks. It watches declarations
across namespaces and creates capability-owned execution Deployments. Bitcoin
scheduling and account disposal stay in the operator; RPC production, transfers,
contract deployment and stacking administration run in independently bound workers.
See [operator workloads](operator-workloads.md) for placement and permissions.
Stacking administration is not a signer sidecar: its restart, credentials and fault
exposure remain independent of consensus.

The network-owned `<network>-receipts:8082` Service accepts legacy
`new_block` callbacks. It validates the current ingress Pod address, exact signed
transaction identity and canonical header membership before a CAS receipt write.
Only already authorized accounts can be accounted; callbacks cannot authorize
submissions. Native Nakamoto indexing remains the normal observation path.
Delivery gaps fail closed and do not turn postconditions into execution receipts.
