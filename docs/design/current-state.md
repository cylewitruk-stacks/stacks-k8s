# Current capabilities and gaps

The [composable public API](public-api/README.md) governs network and action
`v1alpha2` behavior. [Runtime operation](../network-operator/public-api-foundation.md)
is distinct from profile-specific live qualification.

## Network

| Capability | Implemented behavior | Boundary |
| --- | --- | --- |
| Composition | Explicit heterogeneous participants, reusable definitions or inline entries, typed overrides | Participant names are single-use within a root |
| Accounts/wallets | Scoped generation/import and public identity resolution | No operator access to private key data; shared accounts are legal experiments |
| Genesis | Immutable balances, epoch/PoX inputs, contract sources and bootstrap cohort | New genesis requires a new network |
| Actors | Bitcoin nodes, Stacks miners/followers, consensus signers; configured StatefulSets and Services | Ready Pods do not establish protocol health |
| Bitcoin production | Fixed/uniform timing, weighted targets, initialization ceilings and per-target execution records | Ambiguous dispatch closes that target |
| Stacks operation | Transaction demand, direct PoX-4/PoX-5 enrollment/renewal, pinned sBTC deployment and registry initialization | Exact-Pod workers do not recover after process loss |
| Faucet | Bounded request CRs, native submission/inclusion, per-worker deduplication | No generic command server or cross-worker nonce coordination |
| Lifecycle | Cooperative pause, terminal stop, actor rolls, destructive participant/root removal and configured PVC retention | No historical network-instance registry or mandatory evidence export |

`StacksNetwork` resolves and admits; domain controllers provision runtime; scoped
workers perform protocol work. Shared status uses disjoint server-side-apply writers.
The portable Go library handles RPC, Clarity and transaction signing; Stacks.js is
an offline test oracle only.

The initial timing profile initializes Bitcoin, enrolls PoX-4, establishes a native
Nakamoto tip, deploys/initializes sBTC, enrolls PoX-5 managers and observes the final
release boundary. `Initialized` records those historical gates. `Operational`
requires fresh native evidence and recent ongoing progress. These are independent
of optional observability tooling.

## Actions and faults

The independent action operator projects bounded generation and reorganization
lifecycle facts. The existing Bitcoin control worker remains the sole sender and
reservation owner. Generation supports immediate, fixed, uniform and explicit
receipt-relative delay. Reorganization cleanup removes its invalidity marker;
it does not restore the previous best chain.

Temporary schedule overrides resume the latest baseline on expiry/cancellation.
The native Chaos Mesh profile requires exact network/participant identities and
actor role on both ends of supported faults. Protocol faults and RPC control loss
are qualified separately; fault cleanup does not prove protocol recovery.

## Observation

The independently versioned `NetworkObservation` API remains `v1alpha1`.
Its default network reader verifies `v1alpha2` participants, workload/process
identity and public configuration reports. It never reads Secret values or
controls the environment. Snapshot identity excludes unrelated protocol heartbeats.
The explicitly selected old inventory reader remains available for external
legacy installations; there is no fallback between network API versions.

Logs, metrics, tracing, durable journals, broader capture/export and richer
correlation remain roadmap work. Native RPC and worker observations used for
baseline convergence are not a general observability implementation.

## Qualification and remaining scope

Qualification must name images, schedules, cluster/version, topology and verified
outcomes. A live result does not establish arbitrary custom-image compatibility,
all cadence/resource combinations or recovery from every fault. Unit/envtest
results do not prove kubelet, storage, GC or protocol behavior.
The [local qualification record](../network-operator/public-api-qualification.md)
includes full-cohort initialization, renewal, native faults and fresh-network reuse
on their recorded images. The bootstrap correction also passed initialization and
one fresh-storage late join after PoX-5 activation. Earlier missing-anchor and
enrollment failures remain without a causal diagnosis or reliable-repeat claim.
Iteration 2 implementation is delivered; the [qualification backlog](public-api/qualification-backlog.md)
tracks remaining acceptance work without treating the delivery as full qualification.

The roadmap retains broader instrumented/adversarial actors, protocol actions,
observation and release qualification. There is no scenario engine, reducer,
replay coordinator, account lease service or management-worker crash recovery.
See the [roadmap](roadmap.md) and [M0 remediation](m0-remediation-plan.md).
