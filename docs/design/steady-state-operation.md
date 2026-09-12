# Steady-state network operation

## Status and authority

The [composable public API](public-api/README.md) defines the implemented network
contract. This document summarizes its product boundaries; generated CRDs and the
resource/lifecycle specification determine individual fields and transitions.

## Desired operating state

`StacksNetwork` describes a functioning regtest network, including topology and
ongoing behavior. Reusable definitions are inert until selected as participants.
The aggregate resolves and admits; scoped workers converge initialization and
maintain declared operation. An external bootstrap command is not required.

## Network genesis and configuration

Account resolution is independent of networks. Genesis explicitly allocates funds
and freezes epoch/PoX inputs, public identities, contract sources and initial
requirements before runtime starts. Extra funded accounts may remain unmanaged.
Generated nodes and workers consume the same artifact. Config rendering is Go-owned
and Secrets remain outside the operator process.

## Bitcoin production

Bitcoin nodes have no mining role. Production selects targets and timing through
separate policy. Shared per-target reservations serialize baseline and bounded
actions. Durable Armed uncertainty cannot be cleared by a client timeout or by
assuming a restarted process reconstructed an unavailable receipt.

## Stacks mining and transaction demand

Stacks nodes retain mining configuration. Independent transfer workers supply
transaction demand, and stackers enroll/renew holder participation. Consensus
signers run separately. A configured transfer interval influences demand; it does
not promise exact block cadence or deterministic consensus outcomes.

## Temporary interventions

Bounded actions and native faults are separate from baseline policy. Temporary
timing overrides resume the latest baseline after expiry/cancellation. Reorganization
cleanup removes its local invalidity marker, not the resulting best chain. There
is no ordered experiment/playbook resource or restoration of an old network spec.

## Identity, readiness, and progress

Admission requires each capability's current dependencies and exact runtime bindings.
Kubernetes readiness, completed initialization, recent protocol progress and action
completion are distinct facts. Optional actors, faucets and telemetry do not become
implicit prerequisites for unrelated capability admission.

Stacks management workers retain process-local nonce/submission state. A lost bound
worker fails the experiment; no replacement reconstructs or replays uncertain work.
Shared accounts are legal experiment inputs, without coordinated nonce ownership
between independent workers.

## Fault traffic and production control

Native protocol-fault selectors include exact network and participant UIDs plus
actor role on both ends. Control access is qualified separately. Losing RPC access
does not prove an actor stopped producing blocks. Fault cleanup likewise does not
prove the Stacks network resumed useful consensus.

## Review ledger and implementation gates

Qualification names the topology, images, schedule, identities and observed results.
Unit/envtest assertions establish controller/API behavior; live checks establish the
selected profile's kubelet, storage, GC and protocol behavior. Broader fault, image,
observability and release matrices remain explicit roadmap work.

## Acceptance

A baseline must initialize from public declarations, keep producing useful protocol
progress, expose distinct failure/health facts and support documented cleanup.
Optional actions/observability must remain independently installable. Operators
provide capabilities and evidence; users/agents investigate and draw conclusions.

## References

- [Runtime guide](../network-operator/public-api-foundation.md)
- [Protocol timing](public-api/protocol-timing.md)
- [Lifecycle](public-api/lifecycle.md)
- [Current state](current-state.md)
