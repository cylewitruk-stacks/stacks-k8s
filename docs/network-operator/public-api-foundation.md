# Public API foundation

The first implementation slice of the [composable public API](../design/public-api/README.md)
provides structural/CEL schemas, independent identity resolution, participant composition
and ownership, and immutable genesis capture. It is installed through the separate
[foundation chart](../../charts/stacks-network-foundation/README.md).

## Delivered behavior

- Fifteen `v1alpha2` CRDs represent the network, participant instances, genesis, epoch and
  block schedules, eight participant definition kinds, Stacks accounts and Bitcoin wallets.
- Definitions remain reusable. A canonical `StacksNetwork/network` explicitly selects
  participants through typed reference/inline entries and typed overrides. Admission rejects
  kind changes while an entry exists.
- Compilation merges explicit network defaults, definition values and entry overrides,
  then fills absent profile values. Lists replace inherited lists. Mutually exclusive
  storage, peer and schedule sources clear their inherited alternative.
- Generated participant names bind the network UID and entry name. The root records each
  participant UID before resolving instance-owned inputs. Names are single-use within the
  network; removal deletes the instance, and a missing recorded instance fails the root.
- Accounts resolve independently of networks. Default node/faucet accounts belong to the
  reusable definition, or to the participant for inline definitions. Key bytes stay in
  scoped resolver Jobs; controllers consume validated public reports and Secret metadata.
  Replacing a public report creates a new inspection Job bound to that report UID and the
  same immutable credential UID. It does not regenerate an established key.
- A complete public policy is durably admitted on each participant before genesis capture.
  The resolver checks participant kinds, account/wallet identities, signing availability,
  miner wallet wiring, signer/stacker attachments, initial funding, and profile timing
  windows. Shared signing accounts are legal experiment inputs; admission does not
  coordinate their nonces. A faucet balance of `"0"` adds no genesis allocation and allows
  later funding. Required baseline signing roles still need initial funds.
  The one-consensus-signer-per-node constraint also applies after freeze. Existing
  admissions retain their attachment; new contenders are considered in participant-name
  order. Stacker cohort uniqueness remains an initial-bootstrap check.
- Genesis captures canonical public allocations, epoch/PoX settings, pinned contract-source
  hashes, and independently evaluable initial-cohort requirements. Its chain digest excludes
  provenance and bootstrap requirements. The entire spec is bounded to 900 KiB and immutable.
  `inputDigest` covers compiled public configuration, logical names, keys and images; it
  excludes UIDs, UID-derived names, generations and lifecycle controls. Generated keys
  must resolve before their public fingerprints can contribute to a reviewed digest.
  Exact UIDs remain in provenance and admission bindings.

## Lifecycle boundary

| Observed phase | Foundation meaning |
| --- | --- |
| `Resolving` | Allocating or resolving; a paused, fully resolved root also stays here |
| `ResolutionError` | Inputs or a participant name are invalid/unavailable; correct the declaration |
| `Initializing` | Genesis exists; actor activation and initialization are not implemented |
| `Stopped` | Terminal desired stop; retained declarations/artifacts may be inspected or deleted |
| `Failed` | A recorded instance or genesis identity was lost/replaced; recreate the network |

`operation: Paused` permits resolution but prevents the first genesis create.
`operation: Running` permits that capture. A successful create remains the freeze boundary
even if pause or an API failure interrupts status publication. Reconciliation discovers
and publishes the existing artifact using its retained cohort policies. It never chooses
a second artifact name. Missing captured policy in this recovery path fails the root with
`BootstrapPolicyUnavailable`.

`Resolved` is `Unknown` during allocation, withdrawal and terminal stop, `False` on
resolution failure, and `True` after complete resolution or successful genesis capture.
A fully resolved paused root therefore reports `Resolved=True` without starting a network.
Recovering an existing genesis reports `GenesisRecovered/Unknown` until current intent is
validated; publication recovery alone does not resolve a newer spec generation.

The foundation never reports `Initialized=True`, `Running=True` or `Operational=True`.
It creates identity resolver Jobs, not actor or transaction workloads. Stopped roots do not
resume. Participant removal remains destructive while stopped, and new entries are not
allocated. Deleting the root ends the instance and preserves independently owned definitions.

An entry-specific error blocks that participant and invalid dependent inputs, while unrelated
admissions and removals continue. Complete valid inputs are required before the first freeze.
Ordinary definition/account reads use the cache; freeze re-resolves public inputs through
uncached reads. Current identity checks and destructive operations also use uncached reads.
Admission and freeze independently verify current dependency UIDs and deletion state,
including Secret metadata behind accounts and the account behind a derived wallet.
Retained participant dependencies must still have their pinned definition and admitted
inputs; a candidate-policy error alone does not invalidate that older policy. Checks
reuse observations only within one validation pass; the final freeze uses a fresh pass.
No Secret data is read and these runtime bindings do not enter the semantic input digest.
Settled roots advance through watches rather than periodic full-network polling.

## Deliberate slice limits

The existing network operator and its runtime remain unchanged in behavior. The preview's
CRDs overlap their group/resource names and cannot be installed alongside the old schemas;
no conversion or upgrade path is claimed.

Actor configuration escape-hatch validation (`config.overrides` and `config.secretRef`) is
not delivered. Such entries
remain unresolved rather than contributing unverified genesis. Imported wallets currently
accept only raw public `pkh(key)` descriptors. Other descriptor formats remain later work.
The original SDK signing adapters continue serving the existing runtime; the independent
`libs/stacks` module currently implements public identity primitives only.

Supported substantive changes to a captured initial participant's configuration report
`PolicyDeferred=True` with reason `BootstrapPending`, retaining the entire prior admission
and its dependencies while initialization is unfinished. If that retained policy's own
dependencies are unavailable, the participant reports `Resolved=False/DependencyUnavailable`;
the admission remains as evidence and the root does not latch `Failed`. Supported controls
project independently even when composition or protected policy changes are rejected.
Protected binding changes instead
report `RequiresReplacement`, with `PolicyDeferred=False`; correcting the candidate recovers
without changing the instance or genesis. Bitcoin wallet attachments are mutable. Bitcoin
production payout and initialization remain fixed by the captured bootstrap requirements,
including across participant replacement. The foundation cannot finish those gates, so
it does not qualify selective actor rolling or post-initialization capability updates.
Workload placement, execution of suspension/cooperative pause, runtime readiness, faucet requests,
bounded actions, protocol transactions, and Chaos qualification remain later slices.
Actor image strings in compiled configuration are desired inputs, not built or qualified
artifacts.

## Evidence

The API-server integration suite strictly admits all 76 objects in the proposed 30-actor
example, resolves its accounts, captures 40 participant requirements and 19 allocations
with a total of 17,012,000,000,000,000 µSTX, and checks immutability, publication recovery,
removal, name reuse, root deletion and reusable-definition retention. Additional cases cover
post-freeze policy edits, independent participant progress during errors, stopped removals,
instance loss, stale-cache rejection at freeze and production replacement. A second capture
admits shared signing accounts and a zero-allocation faucet (18 allocations).

A real manager runs with the rendered chart's RBAC. Its informer-driven account controller
creates the scoped resolver resources. The test executes the resolver entry point using
that Job's ServiceAccount and verifies both permitted access and denied unrelated Secret
reads/Pod creation. The suite also checks filtered Job/report caches and `ResolverFailed`
reporting from a terminal Job status. Envtest has no kubelet or garbage collector: this is API/controller/RBAC
evidence, not a live Pod, GC, or Stacks-protocol qualification.

The [review ledger](public-api-foundation-review.md) records the final check results and
remaining delivery work.
