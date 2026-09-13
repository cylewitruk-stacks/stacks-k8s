# Stacks protocol oracle v1

`vectors.json` contains test-only oracle data produced by pinned Stacks.js 7.6.0.
The Go consumers use their production codecs, transaction construction and signing;
no expected-byte decoder or digest implementation is shared with the generator.

The fixture covers 23 Clarity values, 36 ordinary signed transactions, 24 structured
signatures and 12 complete PoX-4 enrollment/extension transactions. Cases include
signed/unsigned 128-bit boundaries, uint64 nonce/fee/amount boundaries, compressed
and uncompressed authorization, mainnet/testnet versions, contract principals,
Clarity publication versions 1–6, tuple ordering, Unicode and optional signatures.
Keys are public test scalars. The generator denies network requests and verifies the
installed SDK versions before writing this file.

Regenerate from the repository root after installing the pinned transaction package:

```bash
node operators/network/transactions/protocol-oracle.mjs
(cd libs/stacks && GOWORK=off go test ./...)
```

The generator lives in the existing JavaScript test integration directory. Fixtures
are consumed by Go tests only; no production binary reads this directory. Changes to
the pinned node/contracts still require renewed live qualification. SDK agreement
alone does not establish node execution, enrollment or renewal correctness.

Protocol references are [SIP-005](https://github.com/stacksgov/sips/blob/main/sips/sip-005/sip-005-blocks-and-transactions.md)
and [SIP-018](https://github.com/stacksgov/sips/blob/main/sips/sip-018/sip-018-signed-structured-data.md).
No external SDK implementation was adapted; no additional source attribution applies.
