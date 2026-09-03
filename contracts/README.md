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
