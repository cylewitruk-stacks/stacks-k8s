# Public resource reference

Read [common
rules](README.md#common-vocabulary-and-bounds) and [lifecycle](lifecycle.md) before treating
an example as a complete environment. YAML in this reference is an excerpt; the [full
example](examples/30-actors.yaml) contains every required input object. Shown `status` fields
are operator output and must be omitted from user-applied manifests.

## Shared actor and capability fields

Reusable Bitcoin/Stacks actor and capability CRs define configuration; they carry no
networkRef or runtime status. Their controllers validate shape/direct reusable inputs and
report Resolved or unresolved inputs. Network-relative wiring is validated only when composed.
[Composition](composition.md) defines the heterogeneous list, ref/inline union, typed
overrides and reference meaning.

The network compiles generated StacksNetworkParticipant objects. Unless stated otherwise
below, runtime status and side effects belong to those instances, not to the reusable CR.
Controls live on the network entry's control field. Direct account, wallet and schedule
resources keep their own independent resolution status.

Actors accept image, imagePullPolicy (IfNotPresent), resources, placement and storage. Root
defaults are immutable in the v1 policy; definition/entry overrides may select
supported live updates. Listed default values are resolution fallbacks, not API-server
insertion into definitions/inline branches; see [composition](composition.md).
placement.nodeSelector is a label map; spreadAcrossNodes requests preferred same-kind
anti-affinity. workerPlacement.nodeSelector/tolerations apply to support Pods only, inheriting
root defaults when omitted.

PodScheduled=False/Unschedulable reports PlacementError with scheduler details, not
unexplained Pending. Initial scheduling without a scheduler decision is still Pending. Correct
an established placement error by removing that participant and adding a new name with
corrected placement, or recreate the network. Do not relocate a bound Stacks worker as a
repair. A provably never-activated worker can be destroyed without a shutdown acknowledgement;
an uncertain activation is not proof it never ran. This is the selected experiment
policy for established PlacementError, including actors and never-activated workers;
it is not a Kubernetes inability to edit their placement. After established PlacementError,
the controller does not apply placement changes to that participant's workload and
reports RequiresReplacement; this is reconciliation enforcement, not admission rejection.
Intentional placement updates
on an operating actor with no PlacementError follow its supported roll rules.
Retrying a lost/uncertain inactive-candidate create does not correct placement or
allocate a replacement identity. Stacks-worker placement changes after binding report
RequiresReplacement. Bitcoin control-worker placement changes use its separate
drain/retained-record contract.

Storage defaults to {size: 2Gi, retainOnDelete: false}; ephemeral: true excludes PVC settings.
Omitted class uses the cluster default; explicit empty class means none. Each participant UID
gets fresh storage. Class/mode/retention changes require a new participant; supported size
expansion is allowed, shrinking is not. StatefulSet whenScaled is Retain; whenDeleted follows
retainOnDelete. Kubernetes manages claim ownership; no second cascading ownerRef is added.
Retained PVCs carry provenance labels and are never automatically mounted by a replacement
instance or network.

## Workload controls

Controls are desired state on the network entry, compiled into the instance. They are not
fields on reusable definitions and are excluded from substantive policy comparison. Root
operation takes precedence. Each kind accepts only its listed controls; unsupported controls
fail static validation.

| Kind | Supported control | Effect and limit |
| --- | --- | --- |
| BitcoinNode, StacksNode, StacksSigner | control.suspended: false default | True scales actor StatefulSet to zero, retaining PVC/config. False recreates its Pod with retained data, requiring fresh identity/readiness. Ephemeral data is lost. The separate Bitcoin control worker remains available for evidence/cleanup but cannot use an absent actor. |
| StacksStacker, StacksContractSet, StacksTransactionProduction | control.paused: false default | Cooperatively stops new submissions while keeping the process and local pending state; observation continues. Resume uses current admitted policy. |
| BitcoinBlockProduction | control.paused: false default | Stops new baseline opportunities; existing bounded actions and cleanup continue under their own contract. |
| StacksFaucet | No participant pause control | Stop issuing requests; root operation Paused holds new sends, including admitted-but-unsent requests, with deadlines unchanged. |

Suspension acknowledges observed termination, not a scale request. It is a process
restart/resynchronization on resume, not an in-memory pause. No generic supervisor, SIGSTOP
control or promise of stopped-Pod retention is added. Stacks workers cannot be stopped and
resumed in this profile; explicit removal destroys the instance.

Actor image/config updates roll only that actor after validation. Resources updates use normal
workload reconciliation; a Pod may be replaced, so no transparent-process continuity is
promised. Bound Stacks-worker image/config/placement changes require replacement; shared
operator rollout does not migrate them. Readiness after any actor roll is separate from
protocol recovery. Root operation Stopped terminates all runtime processes irreversibly while
retaining declarations/config/PVCs until destruction.

Each instance admits a complete substantive policy or reports RequiresReplacement. Keep its
prior complete policy only while dependencies remain valid and the entry remains selected.
Never combine a new amount with an old deferred signer/ingress. Controls still apply
independently; correcting an incompatible edit can allow the latest complete policy to
converge without changing participant identity.

Instance status reports resolved bindings, effective policy/control, runtime refs and
execution observations. See [StacksNetworkParticipant](#stacksnetworkparticipant) and [status
ownership](operations.md#admission-execution-status-and-faucet-dispatch). Reusable definitions
report validity/public input resolution only; no account-global nonce/balance or per-network
runtime history.

## StacksEpochSchedule

```yaml
apiVersion: network.stacks.org/v1alpha2
kind: StacksEpochSchedule
metadata:
  name: epochs
  namespace: lab-30
spec:
  epochs:
    - {name: "1.0", startHeight: 0}
    - {name: "2.0", startHeight: 0}
    - {name: "2.05", startHeight: 203}
    - {name: "2.1", startHeight: 204}
    - {name: "2.2", startHeight: 206}
    - {name: "2.3", startHeight: 207}
    - {name: "2.4", startHeight: 208}
    - {name: "2.5", startHeight: 209}
    - {name: "3.0", startHeight: 252}
    - {name: "3.1", startHeight: 253}
    - {name: "3.2", startHeight: 254}
    - {name: "3.3", startHeight: 255}
    - {name: "3.4", startHeight: 262}
    - {name: "4.0", startHeight: 282}
```

Spec is immutable. All names occur once in supported protocol order; heights are
nondecreasing, not strictly increasing, because initial epochs share height zero. Epoch names
are strings, not floating-point numbers (`2.05` must not become `2.5`). The schedule
controller validates structural rules and publishes `status.digest` and `Ready`. It creates no
workload and performs no RPC. Referencing networks copy its contents at freeze; deleting the
schedule later does not change an already frozen network. Create another schedule for another
initialization plan. Omitting a network schedule selects the versioned built-in schedule
above, records its version/digest, and creates no hidden schedule CR. The consuming profile
applies the [source-derived timing gates](protocol-timing.md), not an epoch-minus-offset rule.

## StacksAccount

```yaml
apiVersion: stacks.stacks.org/v1alpha2
kind: StacksAccount
metadata:
  name: holder-01
  namespace: lab-30
spec:
  key:
    generate: true
```

Identity/key spec is immutable once resolved. key is exactly one of generate: true, secretRef:
{name: imported-account, key: privateKey}, or publicIdentity containing validated
address/publicKey. Omission generates a key. An imported Secret is immutable, user-owned and
not overwritten. Public-only identities can receive funds and supply public configuration; a
signing consumer still requires an actual private key.

Status exposes address, publicKey, keyFingerprint, credentialsRef and Resolved. No network
binding, mandatory purpose, writerRef, Lease, nonce or receipt ledger. Several actors may
reference this account or intentionally import equivalent keys. Examples use different keys
for uncomplicated operation; protocol consequences of sharing are observable experiment
behavior, not a blanket admission failure.

The account controller owns an optional immutable key Secret and scoped resolver Job/report.
Private encoding/inspection stays outside the operator process. Resolver Jobs depend only on
this identity specification and its direct inputs, not on a network. Imported complete public
identity can resolve without a Job. Resolved does not mean funded or stacked.

Genesis funding comes from StacksGenesis; later funding uses a network-specific faucet
request. Deleting the network preserves the account and its generated Secret. Deleting the
account itself can delete that Secret and invalidate active consumers; it does not burn
on-chain funds. Cross-namespace reuse requires deliberate credential import/provisioning in
each namespace; no implicit cluster-global Secret access is provided.

## StacksNetwork

The canonical name is network, one root per namespace. Creation requests convergence to
spec.operation (Running by default); deletion disposes runtime. The network owns generated
participants, StacksGenesis and shared Bitcoin execution records. Reusable CRs and their
generated keys remain independently owned.

```yaml
apiVersion: network.stacks.org/v1alpha2
kind: StacksNetwork
metadata:
  name: network
  namespace: lab-30
spec:
  operation: Paused # Resolve for inspection; default Running.
  profile: regtest-pox4-pox5-v1
  epochScheduleRef: {name: epochs}
  participants:
    - name: btc-01
      kind: BitcoinNode
      definition: {ref: {name: btc-01}}
    - name: faucet
      kind: StacksFaucet
      definition: {ref: {name: faucet}}
  # Full actor dependencies and genesis allocations: see the complete example.
```

| Field | Input/default and update boundary |
| --- | --- |
| operation | Running default; Paused holds startup or cooperatively pauses active work; Stopped is terminal process shutdown. See the lifecycle transition table. |
| profile | regtest-pox4-pox5-v1 default; immutable from creation. |
| epochScheduleRef or epochSchedule | Exclusive named or inline {epochs: [...]} input; omission selects the release's built-in schedule. Immutable from creation. |
| genesis.allocations | Explicit accountRef/amountMicroSTX list; equal address/amount aliases count once, conflicting amounts invalid. Immutable from creation. |
| genesis.pox | {rewardCycleLength: 20, prepareLength: 5}; positive prepare shorter than cycle. Immutable from creation. |
| participants | Mutable map-list keyed by participant name: kind, definition.ref or typed inline branch, optional typed overrides and control. Includes faucet and other maintenance kinds. |
| defaults | images.bitcoin/stacksNode/stacksSigner, storage/resources, actorResources and workerPlacement. Immutable from creation; use supported definition/entry overrides for live image/resource changes. |
| expectedInputDigest | Optional reviewed candidate digest, mutable before freeze and fixed afterward. Gates initial capture, not subsequent topology edits. |

Immutable root fields use static CEL transition rules. Defaults are fixed deliberately to keep
inheritance stable, not because images/resources are chain inputs. An image typo can be
corrected on the selected definition or entry override. A different root default requires a
new root. expectedInputDigest is checked against the frozen artifact by the controller; a
differing post-freeze edit reports ImmutableGenesisInput and requires a new network.

Before freeze, the entire selected graph must resolve, including required Bitcoin, Stacks
miner, signer, stacker, production, traffic and contract-set instances. Unverified actors may
be extras but cannot satisfy initial managed prerequisites. A Bitcoin-only automatic profile
is future scope. Later invalid additions block only the affected instances; genesis and
completed initialization are not recomputed.

```yaml
# Illustrative controller output; omit from apply input.
status:
  phase: Initializing
  inputDigest: sha256:...
  genesisRef: {name: "<generated genesis>", uid: "<genesis UID>"}
  genesisDigest: sha256:...
  bootstrap: {stage: EnrollPoX4, bitcoinCeiling: 234}
  participants:
    - name: btc-01
      resourceRef: {name: "<generated participant>", uid: "<participant UID>"}
  identities: [] # Bounded allocation/worker-safety records; never logs or policy history.
  conditions: []
```

The network controller alone writes root status. Current participant references and conditions
summarize topology; [Operational](lifecycle.md#operational-condition) uses the profile's
baseline predicate rather than all participant readiness. status.observationPolicy exposes
the immutable release settings described in [operations](operations.md#observation-policy).
High-frequency observations stay on each instance. The bounded identities
ledger prevents accidental instance/worker reuse after removal, as defined in
[lifecycle](lifecycle.md). It is not a retired-resource archive.

## StacksNetworkParticipant

Generated `network.stacks.org/v1alpha2` resource, one per named network entry. It is not a
reusable definition and users do not apply it. The aggregate controller owns its spec,
complete status.admission and resolution/policy projections. Domain controllers own
configuration validation reports and workload/runtime facts; a scoped worker owns only
its assigned execution status. The [status ownership
contract](operations.md#admission-execution-status-and-faucet-dispatch)
also defines the temporary WorkloadReady fallback for unsupported kinds. A kind
discriminator selects the same configuration branches as
[composition](composition.md).

```yaml
# Illustrative generated instance, not an apply template.
apiVersion: network.stacks.org/v1alpha2
kind: StacksNetworkParticipant
metadata:
  name: "<generated participant>"
  namespace: lab-30
  ownerReferences:
    - apiVersion: network.stacks.org/v1alpha2
      kind: StacksNetwork
      name: network
      uid: "<network UID>"
      controller: true
spec:
  networkUID: "<network UID>"
  participantName: miner-a
  kind: StacksNode
  source: {name: standard-miner, uid: "<definition UID>", generation: 1}
  configuration:
    stacksNode:
      bitcoinNodeRef: {name: bitcoin-a}
      identityAccountRef: {name: miner-key-01}
  control: {suspended: false}
  # genesisRef and admitted dependencies are published only after validation.
status:
  admission: # Complete subtree owned by the network aggregate.
    source: {name: "<definition>", uid: "<definition UID>", generation: 1, digest: sha256:...}
    genesisRef: {name: "<generated genesis>", uid: "<genesis UID>"}
    dependencies: [] # Participant/account/wallet/Secret names and exact UIDs.
    policyDigest: sha256:...
  runtime: {workloadRefs: []} # Domain-controller facts.
  execution: {} # Only present for a Stacks protocol worker.
  conditions: []
```

networkUID, participantName and kind are immutable. source identifies a pinned referenced
definition; inline sources instead record the root generation and inline digest, with no
synthetic reusable CR. configuration is the complete compiled policy, not an override patch.
Aggregate admission keeps the previous valid complete policy when a candidate requires
replacement; admission.source records that policy's provenance independently of the
candidate spec.source. AdmissionReady reports current retained-source and dependency
eligibility separately from candidate resolution; workers never execute spec directly.
Control is independently projected from the
network entry. Users edit that entry; generated spec drift is reconciled back. Domain
controllers watch instances of their kind and their indexed dependencies, and own
workload/config roots through the participant ownerRef. Ordinary StatefulSet/Deployment/Job
descendant ownership follows.

Absent account refs may generate identities: referenced node/faucet definitions own their
reusable default accounts; inline instances own their default accounts instead. Two entries
referencing the same definition share that default identity deliberately; explicit account
overrides give distinct identities. Inline default keys are not reusable across networks and
are deleted with their instance. Genesis records public allocations, not a promise to retain
signing keys after participant removal.

Deleting a generated instance directly while its network entry remains is an InstanceLost
error, not permission to recreate it. Remove the entry and use a new participant name, or
recreate the network. The normal destruction interface is removing the entry, described in
[lifecycle](lifecycle.md#membership-removal-and-re-add).

## StacksGenesis

Generated network-owned `network.stacks.org/v1alpha2` artifact, never a user-created
participant or external genesisRef input in v1. The network controller creates and reports it;
schema validates its immutable entire spec, and consumers verify owner UID and published
artifact UID/digest. It creates no workloads itself.

| Field | Generated contents |
| --- | --- |
| spec.chain.profile | Resolved protocol/config profile revision. |
| spec.chain.epochs | Complete resolved ordered epoch schedule. |
| spec.chain.pox | Resolved first burn height, reward-cycle and prepare settings. |
| spec.chain.allocations | Concrete public principal/amountMicroSTX entries, normalized and deduplicated. |
| spec.chain.contracts | Public sBTC/PoX contract bindings and source manifest hashes required for node configuration. |
| spec.source | Network UID, captured inputDigest, resolved genesis-source UID/generations and initial participant names/UIDs and compiled policy digests. No entire descriptor specs, private keys or rendered configuration. |
| spec.bootstrap | Ordered profile gates with their ceilings/cycles and bounded public requirements: participant/dependency UIDs, holder/miner/signer identities, initial mining-enabled roles, stake amounts and required cycle coverage, wallet funding/maturity requirements, and contract/registry expectations. Values needed after controller restart, not only policy digests. |
| status | Canonical genesisDigest over spec.chain and publication time. Source/cohort provenance is excluded from the chain digest. |

Creation captures validated chain data, initial cohort and bootstrap requirements together.
spec.bootstrap is immutable, excluded from genesisDigest and included in the artifact size
limit. Gate evaluation uses these captured values; [bootstrap policy
updates](lifecycle.md#bootstrap-policy-updates)
cannot rewrite them. Maximum total serialized
artifact size is 900 KiB; reject oversize before activation. There is one deterministic
artifact name per network UID. A lost create acknowledgement is read back, not retried under a
different name. Nodes' public runtime bindings reference its exact UID/digest; Go renderers
consume it into ordinary ConfigMaps/Secrets with matching genesis TOML. Pods do not mount a CR
directly. Custom node configurations must satisfy the same protected-genesis validation unless
explicitly Unverified.

Once published, missing/replaced/corrupted genesis fails closed even if no worker has started.
Never regenerate from edited definitions. Later actor additions use this same artifact and
need existing genesis accounts or explicit later funding. Removing an actor/account does not
remove an allocation from chain history. Deletion of the network removes its owned artifact;
export public genesis/evidence before that if needed. Repeated networks may resolve identical
chain inputs and digests, while still having distinct UIDs, runtime and data.

## BitcoinWallet

```yaml
apiVersion: bitcoin.stacks.org/v1alpha2
kind: BitcoinWallet
metadata:
  name: miner-wallet-01
  namespace: lab-30
spec:
  walletName: miner-01
  keySource:
    stacksMinerAccountRef: {name: miner-key-01}
  watchOnly: true
```

This is reusable identity/descriptor configuration, not one Core wallet database. No
networkRef or nodeRef. BitcoinNode.walletRefs selects where to load it. The same wallet can be
loaded on several nodes and reused with fresh databases in later runs. Within one node,
referenced walletName values must not conflict; different nodes may load the same
name/descriptor independently.

keySource is stacksMinerAccountRef, secretRef to a supported descriptor, or generate: true
(default). Identity/walletName are immutable once resolved; watchOnly defaults true. Managed
miner wallets use public descriptors matching the miner key encoding; sharing that key with
other actors is permitted. The Stacks miner holds its signing key; Core need not. Omitted
walletName uses the CR name.

The current implementation supports watch-only Core wallets only: `watchOnly: false`
is rejected at API admission. Generated or imported wallet keys remain outside Core;
this profile does not implement private-key import or Bitcoin spending from Core.
Previously stored unsupported declarations report `Resolved=False` with reason
`UnsupportedWalletProfile` before dependent admission. An already-resolved wallet is
immutable; select a new supported wallet declaration instead of editing its identity.

Wallet resolution depends only on its key source and directly referenced identity, not on
network or node readiness. Controller owns optional reusable key Secret/resolver report and
publishes public address/descriptorDigest/Resolved. Bitcoin node controllers delegate
create/load/import and unload to their node control worker, reporting results under Bitcoin
participant status.wallets, including walletRef, local loaded state, balance/confirmations and
observation time. There is no single global wallet balance.

Deleting a network preserves this declaration and its key. Removing a walletRef from a node
requests unload after dependent activity has stopped; an active miner dependency reports InUse
until its binding is removed. Deleting the wallet itself does not erase historical Core wallet
files or reverse spending.

## BitcoinNode

```yaml
apiVersion: bitcoin.stacks.org/v1alpha2
kind: BitcoinNode
metadata:
  name: btc-07
  namespace: lab-30
spec:
  peers:
    nodeRefs: [{name: btc-01}, {name: btc-02}]
  walletRefs: [{name: miner-wallet-01}]
  placement:
    nodeSelector: {kubernetes.io/hostname: stacks-k8s-worker2}
  storage: {size: 2Gi}
```

No mining role or cadence field. `peers` is either explicit `nodeRefs` or `discovery: Network`
(default); discovery resolves startup seed identities/DNS from selected participants' allocated
identities,
excluding self, without waiting for peer Pods. The rendered seed set is fixed for that actor
configuration generation; ordinary joins/removals do not rewrite existing configs or roll
peers. Seed resolution does not wait for peers' final runtime admission. An admitted
seed's removal does not withdraw this actor's admission; seeds are startup hints rather
than ongoing mandatory dependencies. Protocol peer discovery connects newcomers.
An explicit peer/config change can replace
that actor and resolve fresh seeds. Wallet attachment is authoritative in
`BitcoinNode.spec.walletRefs`, an optional list of BitcoinWallet name refs. Per-node loading
state is network-scoped. Peer refs are mutable and resolve current actor UIDs; runtime
key/storage bindings remain fixed within that network.

Bitcoin controller manages participant-owned StatefulSet, P2P/RPC Services, config/RPC
Secrets, PVCs and one control worker Deployment. Source BitcoinNode survives network deletion.
Core defaults: regtest, P2P 18444, RPC 18443, listen enabled. Actor RPC principals and control
principals are distinct; the control worker owns wallet setup, regtest initialization and
every baseline/action generation dispatch on that target. Stacks miners may use their
wallet-scoped transaction RPC permissions; they do not receive `generatetoaddress` authority.
Readiness means Core RPC is responsive and reports the correct regtest identity; chain
synchronization is a separate condition. Status includes bound wallet refs and observed
tip/height. Core itself can restart; Bitcoin control worker restart/ambiguity uses the
retained Bitcoin execution rules.

### Bitcoin configuration

`config.overrides` uses native Bitcoin option names. Root scalar values become
`key=value` entries; booleans become `1` or `0`. Scalar lists replace all repeated
entries for that option, retaining list order. One nested object selects a native
section (`regtest`, `main`, `test`, `testnet4` or `signet`); deeper objects are invalid.
For example:

```yaml
config:
  overrides:
    dbcache: 256
    debug: [net, rpc]
    regtest:
      maxconnections: 32
```

`config.secretRef: {name: core-config-v2, key: bitcoin.conf}` instead selects a
complete immutable native document. The scoped resolver reads its exact admitted
Secret UID; replacing a Secret under the same name cannot replace that binding.
Use a new Secret name for a new configuration version. Declared `${SERVICE:alias}`
substitutions follow the DNS-only rules below. No credential interpolation occurs.
Native documents support comments and repeated `key=value` entries. Option names
must be lowercase, without dotted aliases; overrides reject newlines and comment
characters in scalar values.

Managed chain selection, RPC credentials/whitelists, listeners, peer seeds, required
indexes and wallet availability are protected in every section, including negated
option aliases. Complete Managed documents must preserve their generated values,
section scope and repetition order. File inclusion/redirection, wallet paths,
notification commands and daemon/process options are outside this single-document
interface, including in Unverified mode. `Unverified` permits other managed-setting
divergence, reports `ConfigVerified=False`, and excludes that actor from protocol
prerequisites and managed mutation targets.

Before genesis freeze, customized Bitcoin actors require the scoped resolver's
syntax and managed-setting agreement report, bound to the exact configuration
inputs and output Secret. Preparation creates support resources only. Bitcoin's
`ConfigVerified=True` does **not** establish that the selected Core image accepts
unknown options, numeric values or option interactions; semantic compatibility is
checked by Core startup and actor readiness. No help-command or separate Core
startup probe is used as a configuration validator.

The optional `observer` settings enable a network-owned read-only RPC sidecar.
`enabled` defaults to false; `intervalSeconds` defaults to 10 (bounds 5–300).
The participant runtime pins its dedicated `observerRPCSecretRef`; it is never
shared with protocol clients or mutation workers. See the
[Bitcoin observer contract](../../network-operator/bitcoin-observer.md).

## StacksNode

```yaml
apiVersion: stacks.stacks.org/v1alpha2
kind: StacksNode
metadata:
  name: miner-01
  namespace: lab-30
spec:
  bitcoinNodeRef: {name: btc-07}
  identityAccountRef: {name: miner-key-01}
  mining:
    enabled: true
    bitcoinWalletRef: {name: miner-wallet-01}
    feeRateSatsPerVByte: 2
  peers:
    nodeRefs: [{name: signer-node-01}, {name: follower-01}]
```

`identityAccountRef` is required for miners; other nodes default to an owned, unfunded
identity account (see [instance identity ownership](#stacksnetworkparticipant)). It supplies
stable P2P identity, not automatic stacking or STX spending. `bitcoinNodeRef` is required;
changing this binding requires a new participant.
`mining` is optional (disabled); enabled miners require a wallet loaded by the selected
Bitcoin node and matching the miner key. Fee rate is positive and mutable. mining.enabled can
toggle live and rolls that node's config/Pod; identity, Bitcoin-node and wallet bindings stay
fixed once admitted. A future miner must declare its explicit identity and
mining.bitcoinWalletRef even while disabled. Enabling waits for the matching loaded wallet and
mature funding, with MiningReady False/WalletNotReady until available. This does not invoke
genesis initialization. `peers` has the same explicit/discovery alternatives as Bitcoin peers,
within the Stacks node kind. Nodes must start without waiting for peers to become Ready.

Node controller manages participant-owned StatefulSet, RPC/P2P Services (20443/20444 and metrics
9153), config
artifacts and PVCs. It renders the **same StacksGenesis** into every managed node config. A
selected signer references this node; the controller derives signer event destinations from
that relationship. At most one consensus signer attaches to a node in this profile; miners may
also serve a signer if explicitly configured, although the 30-actor example uses dedicated
signer nodes. A changed signer attachment updates that node's event configuration and may roll
it; this is separate from peer discovery.

Status reports role, Bitcoin binding, config/genesis digest, endpoint and native chain
observation. RPC Ready is distinct from synchronized/advancing. A node image may be changed
independently using an explicit OCI reference; readiness does not assert a successful protocol
upgrade.

### Configuration escape hatch

```yaml
spec:
  config:
    overrides:
      node:
        mine_microblocks: false
```

`config.overrides` is a structured TOML-value tree merged after typed settings; arrays replace
whole arrays. Supported scalar/list/table types are validated. Genesis balances, epochs, PoX
settings, node identity, required credentials, managed endpoints and signer event
subscriptions are protected paths. Attempts to override them are rejected with their exact
path. Unknown unprotected settings reach the selected actor image, whose validation error is
surfaced as InvalidConfiguration.

Before initial genesis freeze, customized Stacks actors use the exact public candidate
chain in a scoped resolver and a configuration check in the selected actor image.
Preparation may allocate Services and private support artifacts, but cannot create
actor StatefulSets. Freeze rechecks the candidate report and configuration Secret
UID; pending, rejected or stale validation leaves genesis unpublished. Candidate
rendering has a separate entrypoint and cannot substitute for a published genesis
binding during actor activation.
Stacks node resolvers wait for their Bitcoin dependency's completed public configuration
report and exact credential binding. Allocated empty Secrets do not start dependent jobs;
Bitcoin actor startup is not required for candidate rendering.

Alternatively `config.secretRef: {name: complete-node-config, key: config.toml}` selects a
complete immutable user-owned config, mutually exclusive with overrides. Change it by
referencing a new Secret name: a validated configuration-version change supports an
actor roll without changing its protected credential identities. This is distinct
from automatic rebinding to a same-name replacement Secret.
Optional substitutions use `${SERVICE:<alias>}`; each unique
alias is declared as `serviceRefs: [{alias: bitcoin, kind: BitcoinNode, name: btc-01,
endpoint: rpc}]`. The replacement is the Service DNS name only, not a URL or credential.
Permitted kind/endpoint pairs are BitcoinNode rpc/p2p, StacksNode rpc/p2p, and StacksSigner
events; their fixed ports are listed in
[operations](operations.md#services-and-worker-interface). Unknown aliases, duplicate aliases,
cross-namespace refs and unmatched placeholders are invalid. No other interpolation or
rewriting occurs. Private rendering/inspection happens in a scoped config resolver, not the
operator process. It reports public genesis/key/binding agreement. Managed mode requires
agreement; `config.compatibility: Unverified` explicitly permits a deliberately divergent
actor, reports `ConfigVerified=False`, and excludes it from satisfying initial protocol
prerequisites and managed mutation ingress. It can still be a network peer for an experiment.
Direct Pod edits are not an alternative configuration API and do not establish verified
runtime identity.

## StacksSigner

```yaml
apiVersion: stacks.stacks.org/v1alpha2
kind: StacksSigner
metadata:
  name: signer-01
  namespace: lab-30
spec:
  nodeRef: {name: signer-node-01}
  accountRef: {name: consensus-01}
```

Node/key binding changes require a new participant; the declaration itself is reusable.
Account must be signing-capable; multiple signers may deliberately share its key. Account
ownership is not transferred. Actor image/resources/storage/config escape hatch follow the
node rules, with signer-specific protected fields. The signer controller manages a
participant-owned StatefulSet, event Service on 30000, metrics listener on 31000, configuration
Secret and optional PVC.
The node controller manages its event subscription and provisions a scoped Go resolver
Job that mints the event token into an immutable Secret owned by the node participant.
Only the relevant node and signer workloads mount it; the operator consumes public
binding reports and Secret metadata. Ordinary Pod rolls preserve the token; a new
participant receives a new token. Once bound, missing or replaced credentials block
operation without silent regeneration or rebinding. Public status identifies the
exact Secret binding.

`Resolved` requires the node/key binding and configuration; it does not wait for PoX
registration. The signer starts early and waits for its protocol role. Ready means signer
process and required node connection healthy; `Registered` and observed reward-set
participation are separate protocol observations, not static `weight` or `index` spec fields.
Desired stake is configured on StacksStacker. The protocol assigns actual slots/weights.
Suspended/replaced consensus signers are actor faults, not management-worker failures.

## StacksStacker

```yaml
apiVersion: stacks.stacks.org/v1alpha2
kind: StacksStacker
metadata:
  name: stacker-01
  namespace: lab-30
spec:
  holderAccountRef: {name: holder-01}
  administratorAccountRef: {name: admin-01}
  signerRef: {name: signer-01}
  targetNodeRef: {name: signer-node-01}
  amountMicroSTX: "1000000000000000"
  lockCycles: 6
  renewWhenRemainingCycles: 3
```

Ref changes require a new participant; amount, lockCycles (2–12), renewal threshold (1 to
lockCycles−1) and entry control.paused are mutable, subject to
[unfinished bootstrap gates](lifecycle.md#bootstrap-policy-updates). A changed amount is a
next-enrollment
intent, not an immediate unlock. This profile stops renewal of the old amount, reports
AwaitingUnlockForAmountChange, and enrolls the latest amount after unlock. It does not issue
stake-increase or indefinitely renew the obsolete amount. Examples separate holder,
administrator and consensus keys. Sharing is permitted, but independent workers can encounter
nonce conflicts. Missing balance reports InsufficientFunds; there is no implicit faucet
request or other experiment action.

Stacker controller manages one participant-owned Go Pod and admitted policy status. Worker
uses the Go Stacks library to query active PoX/account/contract state; PoX-4 uses direct
stack/extend. PoX-5 deploys the pinned direct-manager contract under administrator
`<address>.direct-signer`, verifies source and signer identity, then maintains
stake/stake-update. Separate administrator accounts avoid contract-name/nonce contention. An
incompatible existing direct-manager source or signer binding reports Conflict; the worker does not
overwrite it. Deployment occurs only when the contract's language version is supported. The
worker observes state rather than requiring a historical receipt. It does not implement
delegated-pool administration or real sBTC signing.

Resolved means all credential, signer, ingress and policy bindings agree. Operational means
the intended signer registration/stake and requested lock horizon are observed; startup may
report Enrolling or WaitingForRewardCycle. Status includes protocol, manager principal,
desired/observed stake, unlock height, next maintenance window, last observed signer
participation and submission summary. No durable nonce ledger. Removing a stacker participant
destroys its worker without undoing stacking; its lock expires through the protocol unless the
user performs a separately supported action.

## StacksContractSet

```yaml
apiVersion: stacks.stacks.org/v1alpha2
kind: StacksContractSet
metadata:
  name: sbtc
  namespace: lab-30
spec:
  deployerAccountRef: {name: sbtc-deployer}
  targetNodeRef: {name: miner-01}
  bundle: sbtc-regtest-v1
  initialization:
    mode: ExplicitTestRegistry
    signerAccountRefs: [{name: bridge-key-01}, {name: bridge-key-02}]
    aggregateKeyAccountRef: {name: bridge-aggregate}
    threshold: 2
```

Only entry control.paused updates the active worker; other changes require participant
replacement. Bundle names are a closed versioned catalog in the pinned Stacks worker image,
with public manifest/source hashes published in status. `sbtc-regtest-v1` contains real sBTC
prerequisite sources at the repository's qualified revision; it is not hacknet's mock
token/registry. It defines dependency order, supported Clarity versions, contract names, and
exact initialization fields. No arbitrary URLs, source checkout, user-authored execution steps
or free-form RPC.

Controller manages one participant-owned initialization Pod; it remains alive after
convergence to expose status and honor pause, with no further writes once initialization is
satisfied. It verifies contract source equality and explicit registry state, deploys missing
contracts as early as their supported epoch, and never overwrites a differing source or
initialized registry. `Conflict` blocks writes; another worker cannot overwrite the same
conflicting chain state. A different frozen contract identity requires a fresh network. Its
account is a reusable signing identity. Bridge keys are public initialization inputs; only the
deployer key is mounted for STX signing.

Resolved publishes contract principals from deployer address without waiting for a node. Ready
requires native source and registry postconditions; its genesis bindings are frozen into each
network before actor startup. The bootstrap producer ceiling prevents crossing the required
epoch while readiness is missing. Removing its participant destroys the worker; deployed
contracts remain on-chain. Deleting the network preserves the reusable declaration; chain data
follows PVC retention. A fresh network starts a fresh chain and deploys the contracts again.
Real sBTC daemon handoff is **not enabled**: a future mutually exclusive authority mode must
prove the same contract identities and explicit handoff, not race this initializer. No real
bridge signer API/image is required for the 30-actor example.

## StacksFaucet and StacksFaucetRequest

```yaml
apiVersion: stacks.stacks.org/v1alpha2
kind: StacksFaucet
metadata:
  name: faucet
  namespace: lab-30
spec:
  accountRef: {name: faucet-account}
  genesisBalanceMicroSTX: "5000000000000000"
  targetNodeRef: {name: signer-node-01}
  maxRequestMicroSTX: "2000000000000000"
---
apiVersion: stacks.stacks.org/v1alpha2
kind: StacksFaucetRequest
metadata:
  name: fund-late-holder
  namespace: lab-30
spec:
  networkUID: "${NETWORK_UID}"
  faucetRef: {name: faucet}
  destination:
    accountRef: {name: late-holder}
  amountMicroSTX: "1000000000000"
  timeout: 5m
```

The full example selects one faucet participant. Its definition may be referenced or inline;
targetNodeRef is required. When accountRef is omitted, a referenced faucet definition owns its
reusable default account; an inline participant owns a default account deleted with that
instance. See [instance identity ownership](#stacksnetworkparticipant). Identity resolution
precedes startup; sending requires an active admitted worker.

`genesisBalanceMicroSTX` defaults to "1000000000000"; zero means no allocation. After freeze,
changing the allocation requires a fresh network. A replacement faucet never adds funding: its
requested initial balance must match an existing frozen allocation or be zero. Request limits
can update independently; account/target changes require a new participant and still obey
frozen allocation rules.

There is no faucet-specific pause; users stop issuing requests, while root operation Paused
holds new sends. maxRequestMicroSTX defaults to "1000000000000" and is inclusive. The full
example funds the faucet sufficiently for its late-participant requests.

Faucet controller manages one participant-owned Go Pod. The worker serializes requests on its
account in creationTimestamp/UID order among currently observed eligible requests. This is
best-effort ordering under watch delivery: a late-observed older request cannot preempt a
newer request already dispatched. There is no separate Pod per request; the user owns each
request. Destination is exactly one accountRef or testnet address, fixed at resolution. Entire
request spec immutable, timeout positive and at most 1h. expiresAt is creationTimestamp plus
timeout, persisted in status.admission and checked independently by the worker. The worker
checks that absolute deadline immediately before beginning a send; at or past it, it refuses
dispatch. A send begun before the deadline may finish afterward. Clock agreement between
controller and worker is a profile assumption, not a distributed clock guarantee. No
edits/replay of terminal requests. Missing participant/account waits until deadline; a removed
single-use participant name cannot admit requests to its replacement. Invalid amounts or
cross-namespace destinations fail schema validation or controller admission before execution.
The worker checks available funds through a chain read; a successful read showing insufficient
funds yields execution Rejected before send. A failed balance read is an unavailable
observation, not proof of insufficient funds.

A native HTTP 400 rejection with the exact submitted TxID and reason FeeTooLow,
BadNonce, ConflictingNonceInMempool or NotEnoughFunds also yields Rejected. It
retains the TxID with noSend=false and frees pending nonce coordination without
advancing the local nonce. The request is terminal and never resubmitted; unknown
reasons or malformed responses remain uncertain.

Request phases: Pending, Submitted, Completed, Rejected, Inconclusive, Expired. Completed
means this exact transfer is observed included successfully through the native transaction
index; increased destination balance alone is insufficient. Post-Nakamoto funding requests
only in this profile; legacy receipt events are not required. Expired requires proof of no
dispatch: either execution admission was never granted (uncertain admission writes count as
possibly granted), or the worker acknowledges deadline refusal/cancellation without a send.
Once admitted, no report is not proof: at expiry an unknown or possibly submitted outcome is
Inconclusive, with pending-account coordination retained. Later exact evidence may refine
Inconclusive to Completed, Rejected or Expired; Expired cannot later become a send. Request
deletion is best-effort cancellation once a request has been admitted to the live worker.
Asynchronous watch delivery means deletion alone cannot guarantee no later send: cancellation
takes effect only when that worker processes it before dispatch. After dispatch it does not
cancel the transfer or release pending nonce coordination. The request controller owns
status.admission; the faucet worker owns status.execution. The [status/dispatch
contract](operations.md#admission-execution-status-and-faucet-dispatch) defines field
ownership, fresh reads, capacity and outcome retention. Completed execution status contains
txid, amount, destination, inclusion block/time and observed network UID, no signed bytes.
Worker restart fails the network; it must not re-send surviving request CRs.

## StacksTransactionProduction

```yaml
apiVersion: stacks.stacks.org/v1alpha2
kind: StacksTransactionProduction
metadata:
  name: traffic
  namespace: lab-30
spec:
  accountRef: {name: traffic-account}
  targetNodeRef: {name: signer-node-01}
  recipient:
    accountRef: {name: traffic-recipient}
  amountMicroSTX: "1"
  feeMicroSTX: "1000"
  interval: 10s
```

Account/ingress/recipient changes require a new participant; amount/fee/interval and entry
pause can update the live worker. Recipient may alternatively be a concrete testnet address.
Positive amounts/fees; interval 1s–1h. This is offered transaction cadence, not a guaranteed
Stacks block interval. Controller manages one participant-owned Go Pod and admitted policy
status. It starts submitting at the profile's supported epoch when the funded source/ingress
are available; it must not wait for the whole network to become Operational, since it helps
that happen. Status distinguishes offered, accepted, observed included, rejected and uncertain
counts, plus lastTxID/last observed inclusion and effective interval. These counters are
worker-session observations, not historical execution ledgers. One pending submission per
account is the initial profile; slow inclusion reduces offered rate. No catch-up burst after
pause. Deletion stops production without undoing transfers.

## BitcoinBlockSchedule and BitcoinBlockProduction

```yaml
apiVersion: bitcoin.stacks.org/v1alpha2
kind: BitcoinBlockSchedule
metadata:
  name: steady-blocks
  namespace: lab-30
spec:
  cadence:
    mode: Fixed
    interval: 60s
---
apiVersion: bitcoin.stacks.org/v1alpha2
kind: BitcoinBlockProduction
metadata:
  name: blocks
  namespace: lab-30
spec:
  scheduleRef: {name: steady-blocks}
  targets:
    - {nodeRef: {name: btc-01}, weight: 1}
    - {nodeRef: {name: btc-02}, weight: 1}
  payoutWalletRef: {name: production-wallet}
  initialization:
    targetNodeRef: {name: btc-01}
    minimumHeight: 203
    minerWalletRefs:
      - {name: miner-wallet-01}
      - {name: miner-wallet-02}
      - {name: miner-wallet-03}
      - {name: miner-wallet-04}
    matureOutputsPerMiner: 2
```

Schedule is a reusable immutable value, not a worker or network owner. Cadence is Fixed with
positive interval or Uniform with positive `minimumInterval` and `maximumInterval >=
minimumInterval`; bounds 1s–1h. Production accepts exactly one scheduleRef or inline
`schedule` with the same shape; omitting both uses Fixed/60s. Changing a ref switches to
another immutable schedule. The controller snapshots its value/generation into status;
a rejected candidate retains the last complete admission while its exact captured schedule
and payout identities remain available. Missing, replaced or deleting admitted inputs close
new opportunities. Generation jitter is separate from weighted target selection.

Production target list/weights/schedule/pause can update the live network. Payout identity and
initialization changes require a fresh network, even for a new participant. Targets have
positive integer weights, a maximum of 100 entries and no duplicates. Initialization first
resolves/imports every miner wallet, distributes the requested coinbase outputs to their
public miner addresses, then matures them and reaches minimumHeight. It uses only the selected
initialization target, which must be in the captured initial targets, and waits for
corresponding wallet states on miner nodes. This membership check applies before
initialization, not to later live target lists. After initialization, the bootstrap target may
be removed without changing initialization inputs or repeating funding; the retained
production binding retains its provenance. Outstanding RPC/action obligations still retain
their execution records. One output's maturity is established from its actual confirmations,
not just a network-wide counter. All generation goes through the existing per-target executor.
An already initialized network never repeats funding on a reconcile or pause/resume. This
funding list covers bootstrap miners only. A later miner requires an explicit funding
operation: create its account/wallet/node, read the resolved wallet address, request bounded
Bitcoin generation to that address on an admitted production target, then wait for the
wallet's actual mature outputs before enabling the miner. This uses the optional action
operator (or an explicitly external funding client), not a replay or edit of initialization. A
fresh late signer/follower needs no Bitcoin mining funds. Declarative ongoing wallet
replenishment is outside this profile.

During bootstrap, the selected initialization target advances under the [profile
gates](protocol-timing.md); weighted baseline scheduling begins after they complete and
subsequently requires only selected target readiness. Unavailable targets lose their
opportunities; they are not substituted or given catch-up work. Removed targets retain
execution records needed for outstanding actions/RPC outcomes. At most one
BitcoinBlockProduction participant may execute in this profile; independent overlapping
production roots are excluded. Definitions alone create no records or workers. The network
owns shared initialization and target execution records so participant removal cannot erase
uncertain RPCs or cause funding to repeat. The production domain controller publishes
the exact admitted schedule, target participant UIDs and cumulative assigned, acknowledged,
unassigned and skipped opportunities in `status.scheduling`, using the fixed
`stacks-network-domain-bitcoinblockproduction-scheduling` SSA manager. The retained
initialization record commits each weighted selection before checking target availability and
keeps one receipt cursor per execution record. Original receipt time and current availability
time remain separate; neither a retry nor producer replacement repeats initial funding.
Bitcoin node control workers perform mutations; no additional production Pod is required.

Removing the production entry withdraws baseline scheduling under the Bitcoin cleanup
contract. A new named production instance must use the same captured payout and initialization
identity and adopt existing network records. Unknown targets stay closed. Network deletion
never deletes a reusable production definition.

## BitcoinBlockScheduleOverride

```yaml
apiVersion: bitcoin.stacks.org/v1alpha2
kind: BitcoinBlockScheduleOverride
metadata:
  name: slow-blocks
  namespace: lab-30
spec:
  networkUID: "${NETWORK_UID}"
  productionRef: {name: blocks}
  schedule:
    cadence: {mode: Fixed, interval: 20s}
  duration: 2m
```

Immutable bounded override, created directly by user/agent. Required networkUID must match the
current network before admission; production participant UID is pinned at activation; duration
begins then (positive, maximum 1h). Pending admission also has a fixed 5m timeout from
creation. Use exactly one inline schedule or scheduleRef. Override changes timing only, not
targets, credentials or pause state. Only one can be Active: contention waits in deterministic
creationTimestamp/UID order until pending expiry. A later pending override never preempts an
active one. Deletion requests cancellation; expiry/cancellation ends future override
scheduling, not already submitted generation. Latest admitted baseline then applies with a
fresh timer anchor, no stale spec restoration or catch-up. Pause still wins, and elapsed
wall-clock expiry continues while paused. Controller restart reads retained
activation/expiresAt timestamps. Status: Pending/Active/Completed/Expired/Cancelled and
status.admission captures the exact override/production bindings, effective schedule,
optional scheduleRef binding, startedAt and expiresAt; status.reason reports lifecycle state.
Block-count expiration is
deferred because stalled or reorganized chains need a separate clock contract; duration
support is complete without it.

## Optional action and observation resources

These do not gate network startup. Examples are in [optional.yaml](examples/optional.yaml).

- `BitcoinBlockGeneration` and `BitcoinReorganization` use
  `actions.stacks.org/v1alpha2`, with required immutable networkUID targeting the
  exact canonical root. Action cadence uses integer-seconds fields such as
  intervalSeconds; reusable schedule/capability durations use duration strings.
  The action operator writes
  action status; the Bitcoin executor performs RPC mutations. Admission verifies the
  current network UID and generated participant/runtime binding. A reusable actor declaration alone
  does not authorize a mutation. Network failure/deletion stops new admission, while
  existing cleanup follows the original contract. A completed reorganization leaves
  the higher-work chain in place; cleanup does not restore the former best chain.
- Native `NetworkChaos` uses upstream `chaos-mesh.org/v1alpha1` and the installed
  qualified profile. No wrapper or playbook CRD. Only protocol actors carry the
  fault-selection label; both primary and target selectors must include exact
  network-uid, participant-uid and role=actor labels under network.stacks.org/.
  See [Chaos Mesh interaction](operations.md#chaos-mesh-interaction) for the required
  target and full selector contract. A retained fault cannot select actors from the
  next run. Worker/control paths remain excluded.
- `NetworkTelemetry` in `observation.stacks.org/v1alpha2` selects an immutable same-namespace
  `networkName`/`networkUID` and administrator-provided `storageSecretRef`. Its mutable
  `sources` select public objects/events, logs and metrics. The controller owns scoped
  collector workloads and admission/workload status; its Go recorder owns the disjoint
  recording-status subtree through SSA. Status reports freshness, gaps and backend
  acknowledgements, never bulk data. Initial log/object retention is an immutable 1h,
  6h or 24h TTL; metrics use the backend's fixed 24h retention. Per-network byte quotas,
  a query Service and additional native protocol polling are deferred. See the
  [operating guide](../../observability/README.md). No actor Secret access or source mutations.
- Future `EvidenceExport` in the same group has immutable networkUID, telemetryRef and time window,
  timeout (1m–1h), and destination prefix beneath the telemetry's configured store.
  Before work, it verifies telemetry.spec.networkUID equals its requested networkUID
  and persists the telemetry UID plus resolved immutable export inputs. It never
  rebinds after telemetry replacement. The root may already be gone: exports read
  retained telemetry, not current-network admission. A never-admitted old export also
  cannot select a new-network telemetry object of the same name.
  Controller copies retained redacted evidence, publishes an artifact URI/digest,
  coverage/gaps and Pending/Exporting/Completed/Failed phase. It never triggers
  collection, actions or replay. Deletion stops export; already written artifacts
  follow the backend's declared retention, not network garbage collection.

Telemetry/export status is bounded to references and summaries, without logs or bulk
artifacts. The complete initial network remains usable without these optional CRDs or operators.
