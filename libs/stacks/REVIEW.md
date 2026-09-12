# Slice 3 review ledger

The portable protocol subset has no Kubernetes or operator dependencies. Network
workers and observation clients consume it through independent module boundaries.
No external SDK source was adapted.

| Boundary | Evidence |
| --- | --- |
| Clarity consensus and resource bounds | Pinned SDK vectors, invalid/truncated/noncanonical input tests, depth/byte limits and decoder fuzz target |
| Transaction authorization | Byte-for-byte transactions and TxIDs for compressed/uncompressed keys, both chain versions, uint64 boundaries and publications/calls |
| PoX and SIP-018 | Structured signature vectors, independent curve recovery, chain-domain separation and complete PoX-4 enrollment/extension transaction vectors |
| Native RPC | Local HTTP fixtures for complete/incomplete fields, uint128 precision, exact submission/inclusion, redirects, cancellation, invalid paths and oversized responses |
| Runtime portability | Existing secp256k1 and x/crypto dependencies only; no Kubernetes/operator imports or runtime Node calls |

The library does not qualify live node execution, deployment compatibility for every
Clarity version, PoX enrollment/renewal, legacy canonical header queries, faucet or
worker lifecycle. Those checks remain at their consuming slice boundaries. RPC
submission errors conservatively preserve ambiguity; consumers retain submission
and retry policy. Expected sender/network identity
must be checked by consumers; `identity.CompressedPrivateKey` aligns transaction keys
with existing identity derivation.

Validation completed on 2026-09-11: `go test -race ./...`, `go vet ./...`, the pinned
SDK fixture drift test, and a 10-second two-worker decoder fuzz run (937,936
executions) passed. HTTP fixture tests required local-listener sandbox escalation.
The root `make verify` remains part of the aggregate handoff. This ledger records
implementation evidence, not an independent specialist-review approval.
