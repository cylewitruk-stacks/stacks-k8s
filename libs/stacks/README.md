# Stacks protocol library

Independent Go module with no Kubernetes, operator or JavaScript runtime dependency.
Consumers require a version and use the repository-local `replace` pattern used by
`apis/network`. Curve operations use Decred secp256k1; address construction uses
protocol-prescribed HASH160. No external SDK implementation source was copied.
Repository MIT licensing applies; dependencies retain their licenses.

| Package | Supported behavior |
| --- | --- |
| `identity` | Existing regtest identities, validated c32check address encoding/decoding and explicit compressed private-key normalization |
| `clarity` | Consensus tags 0–14: signed/unsigned 128-bit integers, buffers, principals, responses, optionals, lists, sorted tuples and ASCII/UTF-8 strings |
| `rpc` | Native info/account/PoX, contract source/variable/read-only observations, one-shot transaction submission and exact canonical inclusion |
| `transaction` | Standard P2PKH single-signature transfers, contract calls and versioned Clarity 1–6 publications; explicit chain ID, version, nonce, fee and post-condition mode |
| `signing` | SIP-018 RSV signatures, PoX-4 signer authorizations and enrollment/extension arguments, PoX-5 direct-manager grants |

`identity.FromPrivate` retains its compressed identity behavior. Transaction signing
matches the SDK's wire semantics: a 32-byte scalar selects an **uncompressed** sender;
a trailing `01` selects a compressed sender. Use `identity.CompressedPrivateKey` before
constructing a transaction for an address obtained from `identity.FromPrivate`.
Private keys are validated against the curve order; signing uses deterministic low-S
ECDSA and returns the protocol-specific recovery-byte order.

Clarity codecs default to a one-MiB byte limit and depth 32, with explicit alternative
limits available (maximum depth 256). They reject trailing data, invalid integer
widths, malformed strings/principals, duplicate or unsorted tuple keys, unsupported
tags and truncated lengths. Contract publications are limited to 128 KiB of ASCII
source. Transactions are limited to one MiB and 1,024 call arguments. Post-condition
lists are empty, with an explicit Allow or Deny mode; sponsored/multisig signing,
transaction decoding, ABI discovery, HD wallets, indexer APIs and general address
format conversion are outside this subset. PoX reward addresses use explicit mode
and hash bytes; workers resolve human-readable Bitcoin addresses.

RPC requires an explicit HTTP(S) origin, request timeout and response-byte bound.
Redirects, proxy discovery and connection reuse are disabled; POST requests have no
replayable body or automatic retry. Errors never include raw node response bodies.
Account balances and thresholds retain uint128 precision; nonce/height observations
use uint64. Chain identity is returned for callers to validate. Successful inclusion
records an observation of canonical membership, without claiming permanent finality.
An unsuccessful submission acknowledgement remains ambiguous; this subset does not
classify node rejection envelopes. Nonce ownership, account identity binding,
retry/recovery policy, authorization, timing and protocol maintenance belong to callers.

[Protocol oracle fixtures](../../contracts/stacks-protocol-v1/README.md) independently
compare consensus bytes, TxIDs and signatures against pinned Stacks.js 7.6.0. Existing
[identity vectors](../../contracts/stacks-identity/v1/README.md) remain unchanged.
Unit tests also cover malformed inputs, transport bounds and signature recovery;
`clarity.FuzzDecode` checks bounded canonical decoding. RPC implementation reuses the
repository's existing native-client request/observation approach without importing
its runtime module. These are library checks; node/contract qualification belongs to
the consuming worker slices. Stacks.js is a test-only oracle.

```bash
GOWORK=off go test -race ./...
GOWORK=off go vet ./...
GOWORK=off go test ./clarity -run '^$' -fuzz FuzzDecode -fuzztime 10s
```

`rpc.Submit` returns `*rpc.SubmissionRejection` only for a complete native HTTP 400
rejection bound to the exact submitted TxID. `Reason()` exposes `FeeTooLow`,
`BadNonce`, `ConflictingNonceInMempool`, `NotEnoughFunds`, or `Other`; raw details
are discarded. `Definite()` identifies only the four named validation refusals.
This settles that submission attempt, not permanent non-execution of its bytes.
Consumers own nonce and retry policy. Other reasons and transport failures remain
uncertain.
