# Shared contracts

This directory contains implemented wire-contract fixtures, reviewed but
unimplemented design baselines, open design amendments, and superseded
historical baselines. Each fixture's authority is identified below. A fixture
is not a shared runtime implementation; a reviewed design does not imply a
served API or controller.

[`inventory-v1.json`](inventory-v1.json) pins the canonical admitted-inventory
payload and digest. Both operators decode the same bytes into their own native
types and verify the digest through their production implementation.

[`leaf-spec-v1.json`](leaf-spec-v1.json) pins typed leaf declarations against
the unstructured API representation verified by the observability operator.

[`image-id-v1.json`](image-id-v1.json) defines immutable runtime image-digest
extraction from kubelet container-status values.

[`actor-ports-v1.json`](actor-ports-v1.json) pins each actor kind's Service
interface across workload rendering and independent observation.

[`target-declarations-v1.json`](target-declarations-v1.json) pins the implemented
generation-bound compiled catalog using the existing complete leaf-spec
vectors. It is independent of the complete admitted-inventory wire contract.

[`action-lifecycle-v1.json`](action-lifecycle-v1.json) is a reviewed,
unimplemented design baseline. It pins the normative
custom-action API group, lifecycle phases, common conditions, correlation
label, cleanup finalizer, core fields, timestamps, stable reasons, spec
immutability rule, timeout wire/validation contract, and forbidden
orchestration fields before the first action CRD is introduced.

[`steady-state-operation-v1.json`](steady-state-operation-v1.json) records
agreed ownership and capability boundaries plus open review gates. It is
explicitly **not an implementation-ready schema**. Its authority is
[Steady-state operation](../docs/design/steady-state-operation.md), adopted by
the [M0 remediation plan](../docs/design/m0-remediation-plan.md).

The following fixtures retain the superseded M0.3/M0.4 design payloads:

- [`bitcoin-actions-v1.json`](bitcoin-actions-v1.json): finite actions,
  attribution, credentials, and cadence;
- [`bitcoin-block-production-v1.json`](bitcoin-block-production-v1.json):
  single-target production, status, and action priority; and
- [`bitcoin-reservation-v1.json`](bitcoin-reservation-v1.json): shared
  per-target Lease protocol.

Each carries `designStatus`, `sourceDocument`, and `supersededBy` metadata.
Their exact historical payload checks continue against the
[archived document](../docs/design/history/bitcoin-lifecycle-m0.4.md); passing
them does not validate the reopened execution or API design. Do not implement
their global-readiness, shared-credential, single-target, or deadline-recovery
rules as current authority. The [initial implemented production profile](../docs/network-operator/bitcoin-production.md)
and generated `bitcoin.stacks.org/v1alpha1` schema define the constrained
baseline slice. They do not implement the historical action/reservation APIs;
broader replacement schemas still need review and explicit migration.
