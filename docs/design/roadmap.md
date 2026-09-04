# Architecture implementation roadmap

## Design-package status

| Area | Design status | Implementation status |
| --- | --- | --- |
| Current inventory and boundaries | Complete | Existing operators only |
| Atomic action API conventions | Complete | Contract fixture only; no action CRDs |
| Mutable topology | Ready for API review | Partially implemented |
| Bitcoin lifecycle actions | Ready for API review | Not implemented |
| Native Chaos Mesh profile | Ready for implementation planning | Not packaged here |
| Protocol-specific actions | Ready for API review, with stated open decisions | Not implemented |
| Passive telemetry and evidence | Ready for backend/API decision | Identity snapshot only |
| Agent, security, and packaging contracts | Ready for review | Partially implemented |

“Ready” means the implementation contract and unresolved decisions are
explicit; it does not mean an API has been approved or shipped.

The review-driven [M0 remediation plan](m0-remediation-plan.md) is authoritative
where it changes an API, dependency, access model, or milestone order described
below. Reconciliation of those sections is an explicit M0 deliverable.

## Planning rules

- Ship the smallest independently useful capability at each milestone.
- The external agent remains the only orchestrator.
- One action resource represents one bounded action.
- Do not block topology maturity on fault or observation products.
- Qualify facts and safety properties; never claim deterministic distributed
  outcomes.
- Every implementation milestone updates API, examples, operator guidance,
  compatibility, security, and release documentation.

## Dependency order

```text
API conventions
  -> mutable topology
       -> minimal passive journal
            -> Bitcoin lifecycle actions
            -> native Chaos Mesh profile
            -> full passive telemetry
                 -> evidence export and query
            -> protocol actions
  -> agent ergonomics and full qualification
```

Topology identity is the shared foundation. The minimal journal precedes action
qualification so later milestones can prove correlation and gaps. Observation
consumes actions without controlling or becoming a runtime dependency of them.

## M0: close API decisions and conventions

**Outcome:** review and freeze the alpha conventions required for independent
implementation.

Execute the complete [M0 architecture remediation plan](m0-remediation-plan.md),
starting with the shared network API-module spike. The plan also replaces the
target post-M0 milestone order; the existing M1-M8 sections remain design
inputs to be reconciled during M0.

- Retain the frozen custom-action API group, correlation label,
  condition/phase vocabulary, typed references, and immutable-from-creation
  policy from the normative action contract.
- Freeze exact structural schemas, defaults, condition/reason tables,
  finalizer names, terminal-generation behavior, and reference semantics.
- Approve `ActionSafetyPolicy` per-resource bounds and document the initial
  absence of a cross-kind atomic aggregate guarantee.
- Choose initial observation journal/storage and query protocol.
- Define supported version/skew posture and the first compatibility matrix.
- Add architecture decision records for unresolved choices that become final.

**Done:** every subsequent CRD has an approved API sketch, owner, permission
boundary, and non-goals; no open decision blocks its first vertical slice.

## M1: mutable multi-actor topology

**Outcome:** make `stacks-network-operator` independently useful for diverse
regtest networks and upgrades.

- Qualify multiple Bitcoin miners/followers and multiple Stacks miners.
- Implement dependency-aware add/remove/roll/suspend semantics.
- Add watched ConfigMap updates and documented immutable Secret references.
- Qualify persistent database retention across independent actor upgrades.
- Publish editor/viewer Roles that prevent ordinary direct leaf mutation.
- Decide and, if needed, implement `SbtcSigner` as a distinct lifecycle kind.

**Done:** [Mutable topology design](topology.md) definition of done passes with
selected real image/version pairs and no image-building behavior in-cluster.

## M2: minimal passive journal foundation

**Outcome:** truthfully correlate topology and future action lifecycles before
qualifying those actions.

- Implement `NetworkTelemetry` with one storage profile, audit/watch source
  coverage, immutable journal envelope, redaction, gaps, and bounded retention.
- Record topology, Kubernetes Event, and generic dynamic-resource lifecycles.
- Implement optional-CRD discovery and informer appearance/disappearance.
- Publish source coverage classes and prove read-only observer RBAC.
- Defer protocol polling, logs, metrics, query, and export to later vertical
  slices.

**Done:** a network rollout and an arbitrary test CRD lifecycle produce a
verifiable local journal with honest coverage/gaps through source outage and
controller restart; high-volume data remains outside etcd.

## M3: Bitcoin lifecycle and atomic-action foundation

**Outcome:** independently control normal regtest mining and bounded Bitcoin
state transitions.

- Add shared action identity/status/finalizer/Lease helpers without a generic
  action engine.
- Implement `ActionSafetyPolicy`, fail-closed per-resource admission, and exact
  RBAC.
- Implement `BitcoinMiningWindow`, then `BitcoinBlockRequest`, then
  `BitcoinReorganization`.
- Expose only typed regtest RPC methods and ambiguous-effect handling.
- Correlate resource, policy, identity, and RPC lifecycle through the M2
  journal without making observation a mutation dependency.

**Done:** each resource satisfies its own section in
[Bitcoin lifecycle design](bitcoin-lifecycle.md), can run with other action
controllers disabled, and composes with a separately submitted native
partition.

## M4: native Chaos Mesh agent profile

**Outcome:** agents directly use qualified upstream fault resources safely.

- Pin and qualify initial Chaos Mesh/platform matrix.
- Publish direct examples for Pod, Network, DNS, IO, Time, and Stress chaos as
  supported per platform.
- Add least-privileged agent RBAC and admission limits; deny Workflow/Schedule.
- Standardize logical actor targeting and correlation metadata.
- Require immutable specs and static namespace/selector admission; document
  the deferred live-enrollment check and logical-target versus Pod-UID
  divergence semantics.
- Verify injection, cancellation, expiry, recovery, and M2 journal gaps.

**Done:** [Native Chaos Mesh integration](chaos-mesh.md) passes without a
wrapper CRD or dependency from the network operator.

## M5: full passive telemetry

**Outcome:** continuously retain a bounded, truthful recent record around agent
activity.

- Extend the M2 journal rather than create a second event path.
- Collect typed Bitcoin, Stacks, signer, workload, log, and metric facts.
- Add source-specific watermarks, sampling, backpressure, and backend health.
- Keep high-volume data outside etcd and prove observer read-only RBAC.

**Done:** the `NetworkTelemetry` definition of done in
[Passive observability](observability.md) passes through source outage,
controller restart, network rollout, and concurrent independent actions.

## M6: evidence export and agent query

**Outcome:** let agents efficiently inspect and preserve relevant retained
facts.

- Implement bounded, paginated read-only query APIs and stable cursors.
- Implement `EvidenceExport`, content-addressed manifest, and completeness/gap
  reporting.
- Publish client schemas and thin CLI commands for query/follow/export.
- Enforce namespace/network authorization and query resource limits.

**Done:** an agent can retrieve and verify a bounded incident window, including
mutation/action history and gaps, without any replay or diagnosis endpoint.

## M7: protocol-specific action families

**Outcome:** expose narrowly bounded Stacks mechanisms that generic
infrastructure faults cannot provide.

Recommended order:

1. `ApplicationClockOffset`;
2. `SignerBehavior`;
3. `MinerBehavior`;
4. `ProtocolInputInjection` after its fixture/endpoint design; and
5. `ActorDiskPressure` only if native platform qualification proves a need.

Each kind receives a separate controller package, schema, RBAC slice, example,
reference page, real-image negative control, and observation correlation.

**Done:** every implemented kind satisfies [Protocol-specific atomic actions](protocol-actions.md)
and a normal image or unsupported platform is rejected before mutation.

## M8: agent ergonomics and full qualification

**Outcome:** make independent capabilities efficient and safe for real agentic
investigation.

- Validate discovery, printer columns, status/reason stability, server-side
  dry-run, watches, and query pagination.
- Publish agent-oriented tasks without fixed scenarios or expected outcomes.
- Run security/threat tests, supply-chain checks, upgrades, storage recovery,
  telemetry failures, and concurrent action safety.
- Publish compatibility matrices and independently versioned charts/images.

**Done:** a least-privileged client can adaptively alter topology, submit
independent actions, inspect facts, and export evidence using only documented
interfaces. No operator chooses the next action or claims root cause.

## Deferred backlog

| Item | Reason |
| --- | --- |
| Managed-cluster and multi-architecture matrix expansion | Qualify after local vertical slices stabilize. |
| `SbtcSigner` | Await executable/lifecycle requirements. |
| Advanced Chaos Mesh kinds | Require a concrete use case and platform contract. |
| Git build/provenance service | External concern; operators consume OCI digests only. |
| Automated replay, scenario, reduction, or diagnosis | Explicitly out of scope; external agent owns these activities. |
| Deterministic network execution | Not realistic or necessary for the product goal. |

## Cross-cutting release checklist

Every milestone must include:

- structural schemas/CEL and generated-file currency;
- idempotent controller unit and envtest coverage;
- live qualification proportional to side-effect risk;
- exact rendered RBAC and admission negative controls;
- observability and capture-gap behavior;
- upgrade/delete/restart/identity-drift behavior;
- API, example, operations, compatibility, and release documentation; and
- signed/scanned independently versioned artifacts where published.

## Open decision ledger

The detailed documents own their domain decisions. Before implementation,
maintainers should resolve at least:

1. observation API names and query protocol;
2. exact action per-resource impact vocabulary and policy defaults;
3. Bitcoin mining destination model;
4. actor testing-capability protocol;
5. initial Chaos Mesh/platform matrix;
6. observation journal, audit integration, query protocol, and retention;
7. evidence destination/encryption model; and
8. product release and compatibility policy.
