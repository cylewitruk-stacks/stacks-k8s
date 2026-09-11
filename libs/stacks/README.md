# Stacks protocol library

Independent Go module for portable Stacks protocol primitives. It has no Kubernetes
or operator dependencies. Consumers require a version and use the repository-local
`replace` pattern used by `apis/network`.

The delivered `identity` package generates secp256k1 keys and derives testnet Stacks
and Bitcoin addresses. It uses Decred secp256k1 and the protocol-prescribed HASH160
construction. No SDK implementation source was copied. Repository MIT licensing
applies; dependencies retain their respective licenses.

Transaction construction, Clarity codecs, SIP-018 and RPC extraction are subsequent
library slices. Existing runtime SDK adapters remain in place until replaced.

The [identity vectors](../../contracts/stacks-identity/v1/README.md) check every public
encoding against the pinned Stacks.js adapter. Raw public `pkh(key)` wallet descriptors
are supported; private, checksummed, ranged and other descriptor forms are rejected.

```bash
GOWORK=off go test -race ./...
```
