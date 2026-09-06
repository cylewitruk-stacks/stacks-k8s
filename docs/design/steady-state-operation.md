# Steady-state operation amendment

## Status and authority

**Direction agreed; implementation contracts open.** The
[M0 remediation plan](m0-remediation-plan.md) adopts this amendment as the
current product and ownership direction. It supersedes the topology-only
boundary and single-target production model of M0.3/M0.4. Its broader contracts
remain open; the constrained [implemented Bitcoin
baseline](../network-operator/bitcoin-production.md)
is an initial delivery slice, not completion of this amendment.

[`steady-state-operation-v1.json`](../../contracts/steady-state-operation-v1.json)
records these decisions and implementation gates.

## Desired operating state

`StacksNetwork` declares baseline topology and supported ongoing behavior for
a useful regtest network. Independently reconciled capabilities maintain that
state. A functioning network is the desired outcome, not a prerequisite for
starting every capability needed to reach it. Explicitly paused, partially
configured, and externally driven networks remain valid uses.

| Resource | Responsibility |
| --- | --- |
| `StacksNetwork` | Declare actors, relationships, baseline production, and transaction demand. |
| `BitcoinNode` | Run Bitcoin Core and expose qualified interfaces; no mining role or cadence policy. |
| `StacksNode` | Run a Stacks node, including its miner role and supported miner configuration. |
| `StacksSigner` | Run a signer with its configured identity and behavior. |
| `BitcoinBlockProduction` | Apply emission timing and target-selection policy across referenced Bitcoin nodes. |
| `StacksTransactionProduction` | Ongoing Stacks demand; one-account fixed-interval transfers through one ingress are implemented. |
| Bounded action/override | Apply one temporary, independently observable intervention. |

The aggregate controller compiles owned resources; it does not issue mining
RPCs, generate transaction traffic, or execute a bootstrap plan. Permanent
changes go through `StacksNetwork`. Each child has one spec writer and one
controller responsible for its status and effects.

Baseline operation must be available without enabling chaos, bounded-action,
or observability controllers. Deployment, chart, API-module, and ServiceAccount
ownership remain packaging decisions. Standalone capability resources need
explicit ownership/overlap rules before becoming a supported advanced API.

## Bitcoin production

Emission timing and target selection are separate dimensions. Intended
capabilities include fixed cadence, cadence with jitter, and weighted random
selection across referenced `BitcoinNode`s. Each emission requests a block on
the selected node's local chain. The controller does not choose a winning
branch or rebalance activity after observing a partition.

Cadence describes offered generation opportunities for the policy. Weights
distribute those opportunities; they are not measured hash power or a promise
about canonical chain growth. Repeatable inputs do not imply repeatable
execution.

One aggregate-owned policy per network with a bounded target set is the
recommended initial shape. Exact schema, jitter distribution, weight bounds,
destination mapping, mutability, and uniqueness enforcement remain open.
Overlapping policies must not silently multiply baseline rates; a per-target
Lease alone cannot enforce this property.

Unavailable or reserved targets must not cause silent redistribution of their
weight. Skip-and-report is the recommended initial behavior; API review must
freeze it with backpressure and timing semantics. Restart must not cause
catch-up bursts. Production remains mutable desired operation with bounded
status, not a terminal action. The [Bitcoin design](bitcoin-lifecycle.md)
owns its execution and recovery gates.

## Stacks mining and transaction demand

Keep mining as a `StacksNode` role/property. Proposal timing is supported actor
configuration, qualified against its image; accepted block cadence also
depends on tenure, signers, processing, and protocol rules. Changes may roll
the actor unless the image offers a qualified live-update capability.

`StacksTransactionProduction` supplies steady demand independently of the
current miner. Submission targets may include follower RPC endpoints. Exact
empty-mempool behavior is image-specific; offered demand does not guarantee a
proposal or block in every interval.

The [initial transfer profile](../network-operator/stacks-production.md) implements
a separately deployed worker, one exclusive account/ingress, fixed offered
intervals, and exact inclusion accounting. External helpers own funding and
PoX-4 enrollment/renewal. Its static account isolation is qualified; rotation
means withdrawal followed by a fresh account/environment, with no hot rotation
or protocol revocation claim. Broader contracts below remain open.

The recommended execution model uses dedicated workers reconciled by a small
controller. M0.6 must define transaction profiles, ingress references, offered
rate, timing, pause/update behavior, funding prerequisites, signing authority,
and one nonce owner per account across producers. Outstanding work,
backpressure, retries, ambiguous submissions, status, and restart behavior
must be bounded and explicit.

M0.6 Requirement 14 must resolve signing-authority isolation:
administrator-selected account/credential profiles, designated worker access, rotation and
revocation, and exclusion of observers, actors, and unrelated producers from
signing credentials. M2 validates that contract before enabling transaction
production; signing material never enters public specs, status, or evidence.

Distinguish submitted, accepted, rejected, pending, and confirmed transactions.
Do not increase offered traffic automatically to conceal a throughput drop.
Funding and signer registration need their own bounded primitives or external
helpers; transaction production does not acquire a bootstrap workflow.

## Temporary interventions

A temporary cadence or pause override is narrowly typed, target-bound,
time-bounded, and explicit about conflicts. It does not patch and later restore
a saved baseline spec. One controller applies the effective behavior from the
current baseline and admitted override; expiry or cancellation resumes the
latest baseline, including edits made during the intervention.

Status binds baseline generation and override identity. Override kinds,
precedence, expiry enforcement, and the boundary with finite generation remain
M0.4/M0.6 decisions. No generic action list or mechanism registry is introduced.

## Identity, readiness, and progress

Preserve the implemented complete inventory as an aggregate snapshot contract.
Do not relabel stale or partial inventory as complete. Design a separate
current target-validation contract for declaration, ownership, UID, generation,
runtime/configuration identity, and required endpoint capability. Wire changes
require fixtures and consumer compatibility work.

Bitcoin production must start while Stacks awaits Bitcoin progress. An
unrelated actor failure must not automatically suspend a valid target.
Observation must continue recording unhealthy actors and identity transitions.
Readiness neither certifies protocol correctness nor permits replacement
targets to inherit admission. Endpoint identity at mutation time and
cross-system races are not solved solely by uncached Kubernetes reads.

## Fault traffic and production control

The initial direction is centralized Bitcoin production with a qualified
management path. Supported protocol-network faults preserve that path unless
control failure is explicitly the subject of the test.

| Traffic | Default fault treatment |
| --- | --- |
| Bitcoin peer traffic | Eligible for selected disruption. |
| Stacks node to Bitcoin RPC | Eligible for selected disruption. |
| Production controller to Bitcoin RPC | Preserved by the qualified protocol-fault profile. |

The RPC paths can share a port. A second Service or an allow NetworkPolicy does
not bypass a packet-level Chaos Mesh fault. Qualify actual source, target, and
direction behavior, including replacement Pods.

Peer isolation, loss of generation control, and process unavailability are
different observations. Isolation need not stop local mining; actual behavior
also depends on templates, software connectivity checks, and pool dependencies.
Do not infer mining cessation from an unreachable control endpoint.

Central production cannot promise continuity through arbitrary Pod-wide
isolation or node failure. Autonomous local production is deferred. A local
proxy alone cannot solve lost control connectivity: a local worker needs
already-admitted authority and reviewed ownership, expiry, and reservation
semantics. Never silently switch from central selection to local policies.

## Review ledger and implementation gates

These are design findings, not claims of implemented vulnerabilities. Recording
a correction does not close its implementation gate.

The [R1/R2 proposal](target-admission-and-rpc-execution.md) supplies concrete
admission and execution recommendations with local evidence. Its conservative
profile blocks conflicting mutations while a request remains unresolved;
the [initial delivery scope](m0-remediation-plan.md#initial-delivery-scope)
puts R1 publication next and uses fresh isolated environments after ambiguity.
Credential rotation and explicit reset/readmission are deferred. R3 still
defines basic RPC authority separation; R4 gates reorganization cleanup.

| ID | Required resolution | Owner | State |
| --- | --- | --- | --- |
| R1 | Target admission independent of whole-network health, including endpoint identity and bootstrap/fault cases. | M0.3/M0.4 amendment; M0.6 | Open |
| R2 | Server-side quiescence or an explicitly weaker RPC contract; client deadlines and Lease expiry do not fence execution. | M0.3/M0.4 amendment | Open |
| R3 | Separate observation/mutation authority, Secret access, and server-enforced RPC permissions; freeze concrete profiles. | M0.3 amendment; M0.7 | Open |
| R4 | Invalidation cleanup for every reorganization exit, including cancellation, timeout, ownership loss, and unresolved cleanup. | M0.3 amendment | Open |
| R5 | Primary-resource metadata patch permissions for finalizers, verified with restricted ServiceAccounts. | M0.2 clarification; implementation | Direction recorded; validation open |
| R6 | Delegated workload/Secret access through topology editing and credential isolation. | M0.7/M0.8 | Direction recorded; qualification open |
| R7 | Query backend reachability and actor trust; proxy RBAC does not authenticate direct backend connections. | M0.7 | Open |
| R8 | Ordinary regtest, resilience/liveness, performance/resource, and reported-behavior acceptance exercises. | M0.8 design; M8 cross-product qualification | Planned |

## Acceptance

- Produce Bitcoin blocks while Stacks is unready and while an unrelated actor
  fails, without weakening target identity.
- Qualify timing and weighted selection without silent redistribution,
  catch-up bursts, or overlapping baseline writers.
- Distinguish protocol isolation from lost control and process failure on the
  supported local platform.
- Exercise transaction demand with account ownership, backpressure, and honest
  outcome attribution.
- Change a baseline during an override; removal resumes the latest baseline.
- Close applicable gates and freeze replacement schemas/fixtures before
  enabling controllers. Runtime acceptance requires implementation evidence.

## References

- [Bitcoin regtest](https://developer.bitcoin.org/examples/testing.html#regtest-mode)
- [Bitcoin mining](https://developer.bitcoin.org/devguide/mining.html)
- [Stacks miner/signer configuration](https://github.com/stacks-network/stacks-core/blob/main/docs/signing.md)
- [Chaos Mesh network faults](https://chaos-mesh.org/docs/simulate-network-chaos-on-kubernetes/)
- [Kubernetes finalizers](https://book.kubebuilder.io/reference/using-finalizers)
- [Delegated workload access](https://kubernetes.io/docs/concepts/security/rbac-good-practices/#workload-creation)
