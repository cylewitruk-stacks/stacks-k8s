# Shared contracts

This directory contains versioned wire-contract fixtures shared by independent
operators. A fixture is an interoperability boundary, not a shared runtime
implementation.

[`inventory-v1.json`](inventory-v1.json) pins the canonical admitted-inventory
payload and digest. Both operators decode the same bytes into their own native
types and verify the digest through their production implementation.

[`leaf-spec-v1.json`](leaf-spec-v1.json) pins typed leaf declarations against
the unstructured API representation verified by the observability operator.

[`image-id-v1.json`](image-id-v1.json) defines immutable runtime image-digest
extraction from kubelet container-status values.

[`actor-ports-v1.json`](actor-ports-v1.json) pins each actor kind's Service
interface across workload rendering and independent observation.

[`action-lifecycle-v1.json`](action-lifecycle-v1.json) pins the normative
custom-action API group, lifecycle phases, common conditions, correlation
label, cleanup finalizer, core fields, timestamps, stable reasons, spec
immutability rule, timeout wire/validation contract, and forbidden
orchestration fields before the first action CRD is introduced.

[`bitcoin-actions-v1.json`](bitcoin-actions-v1.json) pins the two initial
Bitcoin action kinds, their exact mechanism fields and typed RPC surfaces,
hard safety limits, fail-closed ambiguity vocabulary, topology credential
dependency, and finite cadence modes.

[`bitcoin-block-production-v1.json`](bitcoin-block-production-v1.json) pins
mutable continuous block production, its bounded status, fixed and
uniform-random cadence modes, ambiguity behavior, and bounded-action yield
rule. It intentionally does not use the atomic-action lifecycle.

[`bitcoin-reservation-v1.json`](bitcoin-reservation-v1.json) is the shared
per-Bitcoin-node Lease contract used by continuous production and both bounded
action kinds. Keeping its constants neutral and separate prevents the two API
groups from defining conflicting exclusion protocols.
