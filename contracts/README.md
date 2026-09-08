# Shared contracts

This directory contains implemented wire-contract fixtures, reviewed but
unimplemented design baselines and open design amendments. Each fixture's authority is
identified below. A fixture
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

[`action-lifecycle-v1.json`](action-lifecycle-v1.json) is the shared lifecycle vocabulary
checked against the served bounded-action schemas. It pins the normative
custom-action API group, lifecycle phases, common conditions, correlation
label, cleanup finalizer, core fields, timestamps, stable reasons, spec
immutability rule, timeout wire/validation contract, and forbidden
orchestration fields.

[`steady-state-operation-v1.json`](steady-state-operation-v1.json) records
agreed ownership and capability boundaries plus open review gates. It is
explicitly **not an implementation-ready schema**. Its authority is
[Steady-state operation](../docs/design/steady-state-operation.md), adopted by
the [M0 remediation plan](../docs/design/m0-remediation-plan.md).

The [implemented production profile](../docs/network-operator/bitcoin-production.md)
and generated `bitcoin.stacks.org/v1alpha1` schema define baseline execution.
[Finite generation](../docs/network-operator/bitcoin-generation.md) and
[local reorganization](../docs/network-operator/bitcoin-reorganization.md) define
bounded actions sharing that executor. Broader production policies remain design work.
