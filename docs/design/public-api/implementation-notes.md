# Implementation notes

Status: proposed constraints for implementing the public contract. The user-facing
[operators guide](operations.md) describes visible workloads, status and permissions.
No implementation or qualification is claimed by these formulas and requirements.

## Go protocol library and runtime boundary

`libs/stacks` is an independent Go module with no Kubernetes/operator dependencies.
Keep its dependency graph independent (GOWORK=off, no root go.work) and register it
with repository module/verification gates on implementation. Consumers declare an
explicit module requirement and a relative local replace, following apis/network
(for operator modules: github.com/cylewitruk-stacks/stacks-k8s/libs/stacks =>
../../libs/stacks). Each consumer keeps its own go.mod/go.sum; no dependency-graph
unification. When adding the module, add libs/ to AGENTS.md's directory guidance as
shared non-Kubernetes protocol libraries, alongside the existing apis/ and tools/
boundaries. Update the JS exception when runtime adapters are removed.

The library provides native node HTTP RPC, Clarity values/codecs, address/key encoding,
transaction construction/signing and SIP-018 structured signing for the supported
profile using standard single-signature authorization. Include only needed transfer,
contract-call/deployment and PoX helpers;
exclude indexer clients, mnemonic/HD wallets, sponsored/multisig transactions and
scenario orchestration from this delivery. Protocol integer widths, chain IDs and
transport configuration are explicit; testnet restrictions and experiment policy
belong to consumers. Reject unsupported encodings and invalid keys explicitly;
bound decoder input size/depth. Mutating submissions have no automatic retry.

Extract existing Go RPC functionality and selectively adapt reviewed routines from
`icon-project/stacks-go-sdk` at a recorded source revision. Do not depend on that SDK
or copy its whole package/dependency structure. Preserve copyright/license notices
for adapted code. Use established secp256k1 primitives; own only Stacks encoding and
signature-format adaptation. Keep enrollment/renewal decisions, nonce ownership and
sBTC/signer-manager administration in workers, outside the library. Reuse in other
operators must not merge their independent Kubernetes wire-contract verification.

Replace the offline JS adapters incrementally. Stacks.js 7.6.0 remains a pinned
**test-only oracle**, with no Node/JavaScript in operator, resolver or worker runtime
images. Compare serialized bytes, TxIDs, Clarity values and structured signatures;
add malformed/boundary-input and decoder fuzz tests. Adapted code and its original
are not independent correctness oracles. Qualify transfers, publications/calls,
PoX-4 and PoX-5 against pinned Core and contracts before removing runtime adapters.
A frozen oracle does not detect upstream protocol changes by itself; profile updates
require renewed qualification. This is a supported subset, not a general Stacks SDK.

## Generated names

Runtime root names have a **52-character maximum**, leaving room for generated
children. Start with `n-<network-token>-<kind>-<participant-name>-<purpose>`, where the token
is the first 12 lowercase hex characters of SHA-256 of the full network UID. Keep
at most its first 39 characters (trim trailing hyphens), then append `-` and 12 hex
characters of SHA-256 over the compact JSON string array
`[networkUID, participantUID, kind, participantName, purpose]`. This suffix is always present;
full UIDs remain in labels/status and are verified on every ownership check. Kind
tokens are lowercase API kind names; purpose uses lowercase DNS-label characters.
Config artifact purposes include the hexadecimal content digest without a colon. A collision is
an error, never adoption.

Actor StatefulSets use ordinal 0, so their Pod hostname/name is at most 54 characters.
The fixed claim-template name data gives `data-<root>-0`, at most 59. Deployment names
are also capped at 52, leaving room for ReplicaSet/Pod suffixes; Job roots use the same
limit. Services are at most 52. No user-supplied ordinal or claim-template prefix is
supported. A future naming change must validate the longest derived names against
their consumers' limits. This avoids treating a valid root name as proof that its
Pod hostname or identity label will be valid.

Status publishes actual endpoints; clients never invent DNS or follow a stable
definition-name alias into the next network. Selectors include full network UID. Reusable
identity names use `<faucet>-account` or `<node>-identity`, with no network prefix;
if over 63 characters, use the first 50 (trim trailing hyphens) plus `-` and 12 hex
characters of SHA-256 of that full candidate. These are CR/Secret names, not
StatefulSet root names; resolver workload roots still follow the 52-character limit
using the identity source UID/name in place of participant UID/name and an empty
networkUID when resolved independently. Generated participant CR names use
prefix `n-<network-token>-participant-<participant-name>`, truncated/trimmed to 39
characters, followed by `-` and 12 hex characters of SHA-256 over the compact JSON
array [networkUID, participantName, "participant"]. No participant UID is used, because
it is not known until creation. Root status records that created
UID before any runtime activation; same-name reconstruction after loss is forbidden.

Inline default accounts/keys use the runtime naming formula with their participant
UID and purpose identity or faucet-account, and belong to that participant. They do
not use reusable-definition name suffixes or adopt an existing same-name account.

Full reusable source names remain in annotations/status, as specified by the
[runtime metadata contract](operations.md#permissions-and-admission-surface); only
source kind/UID enter provenance labels. Verify metadata generation with legal source
names longer than 63 characters. Do not truncate a source name into an ambiguous label
or impose the participant-name limit on reusable definitions.

## Chaos profile transition

The [current profile template](../../../charts/stacks-chaos-profile/templates/profile.yaml)
uses network-name/actor-name selectors. Version its creation rules, rendered checks
and examples for the proposed UID/role contract while preserving legacy fault
status/finalizer/deletion operations. Existing live evidence does not qualify these
new selectors; qualify both endpoints and control-path exclusions together.
