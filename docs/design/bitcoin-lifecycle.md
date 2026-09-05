# Bitcoin lifecycle design

## Status and authority

**Direction agreed; replacement implementation contracts open.** This document
applies the [steady-state operation amendment](steady-state-operation.md).
No Bitcoin production or bounded-action CRD is implemented. The reviewed
[M0.4 baseline](history/bitcoin-lifecycle-m0.4.md) and its three fixtures are
superseded design history, not implementation authority.

Target API groups remain `bitcoin.stacks.org/v1alpha1` for
`BitcoinBlockProduction` and `actions.stacks.org/v1alpha1` for
`BitcoinBlockGeneration` and `BitcoinReorganization`. Exact replacement
production fields, schemas, admission, and recovery contracts are not frozen.

Only bounded actions follow the [atomic action contract](actions.md).
Production maintains mutable baseline behavior without a timeout or terminal
completion claim. It is not a scenario or ordered action plan.

## Network and actor ownership

`StacksNetwork` declares baseline production and compiles an owned
`BitcoinBlockProduction` alongside neutral `BitcoinNode` resources. Bitcoin
nodes have no mining role in the target API. The implemented `miner`/`follower`
enum remains a compatibility fact until an explicit migration updates types,
schemas, compiler, examples, and wire fixtures.

M0.4 owns the migration design under
[Requirement 9](m0-remediation-plan.md#requirement-9-consolidated-bitcoin-generation-api);
M1 implements and validates that reviewed migration.

The aggregate writes child specs; a separately permissioned production
controller issues typed regtest RPC requests. Permanent baseline changes go
through the aggregate. Baseline operation does not require enabling bounded
action controllers. Standalone policy ownership and packaging remain open.

### Bitcoin image basis

Use the Debian-based [bitcoin/bitcoin image](https://hub.docker.com/r/bitcoin/bitcoin)
as the planned standard regtest image, with 31.1 as the current qualification
baseline. The publisher describes these as unofficial Bitcoin Core testing
images. Record version, platform, and immutable digest for qualification;
avoid floating latest tags. Existing 25.2 examples remain pending workload
qualification and an explicit example update. Custom images remain subject
to the same capability and identity checks.

## Target admission

Admission establishes current declaration, network membership and ownership,
UID, generation, runtime/configuration identity, credential profile, and
reachable regtest capability. It must not require unrelated actors to be
Ready or accept stale aggregate inventory. R1 in the amendment owns the
replacement target-validation and endpoint-identity contract.

The [R1/R2 proposal](target-admission-and-rpc-execution.md) recommends a
health-independent declaration catalog joined to current leaf/runtime identity,
plus instance-authenticated RPC. The concrete R3 transport remains a dependency.

References are typed and same-namespace. Public specs do not choose arbitrary
RPC endpoints, methods, wallets, or credentials. Destinations remain explicit,
runtime-validated regtest addresses. Replacement objects never silently inherit
admission; target-set edits need explicit generation/identity semantics.

## `BitcoinBlockProduction`

The policy separates emission timing, target selection, destination mapping,
and pause state. It offers fixed or qualified jittered cadence and fixed or
weighted random selection over a bounded set of Bitcoin node references.
Weights distribute generation opportunities, not physical hash power or
guaranteed canonical progress. It does not choose a winning branch.

One policy per network is the recommended initial shape. Naming, uniqueness,
target overlap, exact bounds, jitter distribution, and mutability require API
review. The former name-equals-node rule and single `bitcoinNodeRef` schema are
superseded. Reservations serialize effects but cannot prevent overlapping
policies from multiplying offered rates.

Each selected opportunity requests one block on the target's local chain
through `generatetoaddress`. Unavailable/reserved targets do not cause silent
weight redistribution. Skip-and-report is recommended pending final API review.
No catch-up burst follows downtime. Bounded interventions take priority on
their own targets without implicitly stopping every other target. Test
stale/expired waiting status and disabled action controllers while preserving
outstanding execution and cleanup obligations.

Status stays bounded: baseline generation, per-target identity/availability
within the target bound, offered/acknowledged/skipped/ambiguous summaries, and
a recent-hash ring. Detailed history belongs to configured observation sinks.
An acknowledgement does not prove global adoption. Exact fields and flush
rates require replacement-fixture review.

In-memory timing state with `RequeueAfter` remains a recommendation. The
standard reconcile request carries namespace/name, not UID or generation.
Store UID, admitted policy generation, and due time in reconciler-owned timer
state. On every reconciliation, compare that record with the current object;
discard stale timing state and select a fresh delay after replacement, policy
change, or restart. A delayed queue entry only prompts reconciliation and
never authorizes applying previous target weights or cadence.

## Bounded Bitcoin actions

`BitcoinBlockGeneration` requests a finite block count. Retain immediate
batches, fixed cadence, uniform timing, and bounded explicit delay sequences
as the design direction. A sequence describes one mechanism's timing, not a
list of actions. Specs stay immutable and creation-relative timeouts bounded.
The `actions.stacks.org/correlation-id` label is a search hint; UID remains
authoritative.

`BitcoinReorganization` requests one bounded replacement of a local suffix.
It does not create partitions or judge Stacks recovery. Higher chainwork and
observed canonical status matter; local completion does not imply global
convergence.

Prior count, batch, depth, and timeout limits remain conservative design
inputs. Replacement fixtures must explicitly retain or justify changing them.
Selective boundary admission still lacks a trusted protocol schedule; the
all-opt-ins approach remains the conservative review baseline.

Only successful mutation responses support acknowledged progress. Partial
progress, definite failure, ambiguous effects, and protocol conclusions remain
separate. Cancellation cannot undo propagated chain history.

## RPC execution and reservation gate

All production and Bitcoin actions require one shared per-target exclusion
contract across their deployment boundary and independently enabled
controllers. The prior Lease/token/leader-gated-manager design is a starting
point, but its recovery guarantees are reopened under R2:

- Client timeout does not terminate Bitcoin Core execution.
- Lease expiry and uncached reads are not server-side fencing.
- Absence from observed tips does not prove an outstanding request cannot
  execute later.
- A fixed grace interval does not establish server-side quiescence.

The [R1/R2 proposal](target-admission-and-rpc-execution.md) recommends persisting
dispatch authority and outstanding work in one protected CAS record. Ambiguity
retains exclusion instead of granting automatic recovery. Reservation ownership
survives individual receipts and executor replacement until explicit clean
release. Action deadlines, transport cancellation, and receipt collection have
separate lifetimes. An explicit reset/readmission candidate is being evaluated
with R3 against IO/stress faults and Bitcoin/controller restarts; a full
execution-aware adapter is not a prerequisite if that contract can be qualified.

Choose an enforceable execution boundary or an explicitly weaker contract with
durable unresolved-operation handling before implementation. Do not authorize
retry, takeover, or resumption solely because `rpc-not-after` elapsed.
Historical `ProvenAbsent` does not authorize retry without proof that
outstanding execution ended. `Inconclusive` does not establish that another
mutation is safe.

Leader election, optimistic updates, local mutexes, bounded clients, and honest
attribution remain useful; their guarantees must match the server-side
contract. R2 also determines whether production can omit durable per-request
execution state. Bounded status does not justify forgetting outstanding work.

## Reorganization cleanup gate

Reorganization combines irreversible history with a temporary invalidation
marker. R4 must define cleanup after success, definite failure, timeout,
cancellation, ambiguous RPC, reservation loss, and target replacement.

Track whether invalidation occurred, whether compensating `reconsiderblock`
is safe and acknowledged, and whether cleanup remains unresolved. Removing
the marker does not undo history. Do not release a target as clean or clear
its safety finalizer merely because generation stopped. Terminal uncertainty
may coexist with retained cleanup obligations and explicit administrative
recovery.

## Credential and renderer gate

Keep high-entropy credentials, immutable administrator-provisioned inputs,
salted `rpcauth` rendering for profiles that use it,
digest-verified actor rendering, and no credential output in logs, status,
arguments, or Helm values. The topology controller does not read Secret bytes.
Current v1 development profiles are not automatically eligible for managed RPC.

R3 reopens the shared `stacks-bitcoin-rpc` design. Define separate observation
and mutation authority, Secret access, actor client permissions, and
server-enforced method restrictions. Freeze names, keys, renderer inputs,
rotation, and profile versions together. Putting both passwords in a Secret
readable by the observer does not separate authority.

For reset recovery, qualify per-process credential epochs alongside actual
termination evidence. Bitcoin's rotating cookie is a candidate primitive;
static mutation credentials do not fence a former producer. Armed requests
pin credentials and never refresh them for a replacement. Provisioning,
restricted principals, and server identity remain R3 gates; cookie rotation
alone does not establish them. See the
[candidate contract](target-admission-and-rpc-execution.md#candidate-credential-epoch).

Authentication does not encrypt RPC transport. Qualify the private-cluster
management path and endpoint identity. Updated API, leaf-specification, and
inventory fixtures precede enabling managed RPC.

## RBAC and finalizers

Controllers need target/resource reads, exact-name credential access, their
own status writes, Events, and the reviewed reservation permissions. Updating
metadata finalizers requires patch/update on the primary custom resource;
status or `/finalizers` permissions alone do not authorize ordinary metadata
patches. RBAC cannot restrict primary writes to individual fields. Immutable
action schemas and controller ownership conventions serve separate purposes.

If Leases are retained, state their get/list/watch/create/update/patch scope
honestly. Labels constrain informer contents, not RBAC. Ownership checks
protect normal behavior but do not narrow a compromised ServiceAccount's
authorization. Restricted-ServiceAccount tests must exercise finalizer
addition/removal as well as the exact rendered-RBAC allowlist.

## Faults, observation, and acceptance

Initial centralized production relies on the
[qualified management path](steady-state-operation.md#fault-traffic-and-production-control).
Record control failure separately from peer isolation and process failure.
Autonomous production under complete external isolation remains deferred.

Observation records baseline generations, selected targets, overrides,
requested/acknowledged effects, cleanup, identity transitions, and gaps. It
never authorizes production, holds reservations, or issues mutations.

Before implementation:

- Close applicable R1–R4 gates and freeze replacement schemas, limits,
  credentials, and execution contracts for each enabled capability.
- Define the Bitcoin role API migration and consumer compatibility.
- Qualify bootstrap, multi-target selection, partial faults, lost control,
  policy changes, and outstanding RPCs beyond client deadlines.
- Test every reorganization cleanup exit and actual finalizer permissions.
- Keep historical fixture checks separate from current-direction and future
  non-vacuous schema/runtime tests.

## References

- [Bitcoin RPC security](https://github.com/bitcoin/bitcoin/blob/master/doc/JSON-RPC-interface.md)
- [Leader-election limitations](https://pkg.go.dev/k8s.io/client-go/tools/leaderelection)
- [Finalizer implementation](https://book.kubebuilder.io/reference/using-finalizers)
