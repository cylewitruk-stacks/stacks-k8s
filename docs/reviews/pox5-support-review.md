# Direct PoX-5 support review ledger

This records the earlier external-helper delivery. The
[managed-operation ledger](managed-operation-review.md) supersedes its delivery
claims for the current controller refactor.

Base: `c85ccd6fbd8ed3efba3f1b4456d4850c68dfe020`. Delivery is the working-tree
diff; the user's index and commits are untouched. Qualification date: 2026-09-07.

## Scope and boundaries

| Area | Delivered behavior |
| --- | --- |
| Shared API | Optional immutable genesis PoX-5 token/registry and administrator principals; shared markers and regenerated CRDs. |
| Provisioning | `--pox5` produces distinct holder, manager administrator, consensus and bridge identities. Only actor-specific keys enter workloads. |
| Bridge initialization | Five unmodified external sBTC contracts, individually checksum-pinned, followed by explicit two-of-two registry initialization and readback. |
| Manager | One Go-rendered, non-custodial contract per participant, restricted to its holder's direct STX staking. Owner-only registration requires consensus-key authorization. |
| Bootstrap | External Go pauses/drains baseline Bitcoin production, deploys prerequisites before height 244, transitions, registers/stakes, and checks fresh production in the first PoX-5 reward cycle. |
| Maintenance | Direct PoX-4/PoX-5 renewal; exact canonical successful execution plus lock-extension evidence. No expired-lock reenrollment or ambiguous submission replay. |
| SDK | Existing package only: offline serialization, grant signing, contract publication and call signing. No HTTP, Kubernetes operations or workflow decisions. |

No new controller, CRD kind, webhook, RBAC permission, runtime module, npm
package or dependency version. Kubernetes minimum remains 1.32. Genesis
principals are immutable; bridge membership and aggregate-key rotation remain
protocol state. A future bridge-daemon handoff requires supported rotation,
not simply starting daemon Pods.

The sBTC sources remain external under their upstream license. The repository
contains only [pins](../../operators/network/internal/protocolcontracts/sbtc-contracts.json)
and its own minimal manager implementation. The pinned source is
`stacks-network/sbtc@d31b0780cf1ae91b6ef4ed7ee89400c796aea913`; the token source
was independently fetched from that upstream revision and matched its pin.

## Automated evidence

| Check | Result |
| --- | --- |
| Initial full `make verify`, isolated indexed snapshot | Pass; `/tmp/stacks-pox5-verify.log`. |
| Focused Go unit and race tests | Pass: bootstrap, environment, profiles. |
| Offline npm and Go SDK tests | Pass; explicit fees/nonces, role separation, PoX-4/PoX-5 signing and publication. |
| Real API-server integration | Pass; both shared genesis consumers accept valid bindings and reject malformed or changed ones. |
| Dependency scan | Pass; `/tmp/stacks-pox5-vuln.log`. |
| Container checks | Pass; `/tmp/stacks-pox5-docker-check.log`. |
| Final full `make verify`, isolated indexed snapshot | Pass; `/tmp/stacks-pox5-final-verify.log`. |
| Markdown lint, relative links and whitespace | Pass. |

Regressions cover public/private role mismatches, manager/deployer
binding mismatches, changed actor identities, altered contract pins, canonical
transaction-byte identity, unsuccessful execution and exclusive evidence paths.
SDK tests prohibit network discovery. The manager's actual Clarity analysis and
registration are qualified against Core, not inferred from serialization tests.

## Live qualification

Shared cluster: `stacks-k8s`, three kind nodes, Kubernetes 1.37.0. Bitcoin:
`bitcoin/bitcoin:31.1`. Stacks node and signer: official **4.0.1**, reporting
`62e03cc`, Linux arm64. Both binaries came from the official
[release archive](https://github.com/stacks-network/stacks-core/releases/tag/4.0.1),
verified against its SHA-512 checksums, and were packaged with Ubuntu 24.04.
The release requires glibc 2.39; Debian bookworm was rejected during the image
smoke check. No modified attacknet binary is used.

Public evidence and logs:

- `/tmp/stacks-pox5-third/bootstrap-evidence.json`: complete deployment,
  registry initialization, manager registration and direct stake.
- `/tmp/stacks-pox5-third/renewal-evidence.json` and
  `/tmp/stacks-pox5-third/sustained-observations.json`: completed two-node
  reward-cycle and renewal qualification.
- `/tmp/stacks-pox5-final-live.log`: final-source bootstrap qualification.
- `/tmp/stacks-pox5-final/bootstrap-evidence.json`: final pause/drain and
  first-PoX-5-cycle completion checks.

The private `environment.json` files alongside these records contain signing
keys and RPC credentials. Do not publish or copy them into review artifacts.
Native `genesis_chainstate_hash` is recorded as a node observation; it is not
a substitute for the tool's resolved-genesis digest or the Kubernetes network UID.

Two diagnosed authoring errors were preserved before proceeding: height 241
violated Core's cycle-boundary constraint, and the initial manager omitted a
required `try!` inside `as-contract?`. The latter produced a canonical failed
publication; bootstrap stopped without registering or staking. Both are fixed.
One attempt stopped before mutation due to a loopback-port collision; its
height-zero environment was reused with distinct ports and a new evidence path.

The 905.6-second sustained run collected 46 observations. Both nodes advanced
from reward cycle 13 to 22, with the ingress burn height 265 → 446 and Stacks
height 91 → 364. Exact transfer confirmations increased 21 → 112. Renewal
transaction `43fdfe503dcc5f721205348608aac1bf2a2d7f3fc0369a3e78350bf4f886384a`
executed canonically and extended unlock height **500 → 620**.

The final-source bootstrap used a separate fresh namespace, `pox5-final`, and
final operator/worker images. It recorded drained Bitcoin pauses at heights
230 and 246, verified the direct stake, and completed at height 260 in the first
PoX-5 reward cycle with 16 confirmed transfers and Stacks height 76.

The sustained run used the preceding bootstrap/image build; the final run
additionally qualified the final role checks, pause-tip synchronization and
first-reward-cycle completion gate. Both used the same official actor binaries
and final manager contract implementation.

| Final image | Image ID |
| --- | --- |
| `stacks-core:4.0.1-pox5` | `sha256:2f55ab44a8f179d94392e9c2ffd22b617f714f10c08f5f50283fa23f46a25efb` |
| `stacks-network-operator:pox5-final` | `sha256:2f1eb30efddfdc59a4ae45ef68f802c859dc4b664167a46d5a3aa69e0dfd74f0` |
| `stacks-transaction-worker:pox5-final` | `sha256:3cc4457dc3221b853a6bcc1f96edbe5c609b9f392c74a143d3b386274e6dbce8` |

The actor image's Ubuntu base ID was
`sha256:33ceb71981b602c1a7443a53469e4dba065f7503eab3078a2d7a57a2ab987517`.
Host tooling: Go 1.27.1 and Node 25.6.1. Explicit arm64 image archives avoided a
Docker multi-platform-store import error in kind.

Namespaces `pox5-first`, `pox5-second` and `pox5-third` were removed after
archiving evidence. The suspended second fixture retained an ambiguous transfer;
it was removed through ordinary administrative network deletion, with no
finalizer overrides or recovery claim. Cleanup logs are
`/tmp/stacks-pox5-teardown.log` and `/tmp/stacks-pox5-teardown-final.log`.

`pox5-final` is retained with both policies paused and every actor StatefulSet
scaled to zero. Final readback confirmed Bitcoin dispatch `Idle` and no
outstanding transfer. Both final nodes had reached PoX-5 cycle 18, burn height
376 and Stacks height 250 before suspension. Public Kubernetes, Pod identities
and node observations are archived alongside the bootstrap evidence.

The shared cluster is stopped; its fixture and private kubeconfig are retained.
Use `make cluster-start` to inspect it. Actors and producers remain explicitly
suspended/paused after cluster startup. This is an initialized environment;
do not rerun its bootstrap command. The existing baseline and maintenance
interfaces control further operation. Cluster shutdown is recorded in
`/tmp/stacks-pox5-cluster-stop.log`.

## Limits

This qualifies the built-in direct-staking profile and one consensus signer.
It does not qualify delegated pools, historical PoX-1–3 workflows, multiple
consensus signers, mixed-version networks, sBTC DKG, bridge daemon ownership
handoff, deposits/withdrawals, or adversarial faults. Bootstrap and maintenance
are exclusive external clients and stop on uncertain mutation outcomes.

## Review prompt

```text
Review the unstaged direct PoX-5 delivery in:
/Users/cylwit/Code/github.com/cylewitruk-stacks/stacks-k8s
Base commit: c85ccd6fbd8ed3efba3f1b4456d4850c68dfe020.

Start with AGENTS.md, docs/network-operator/pox5.md and
docs/reviews/pox5-support-review.md. Inspect the actual diff and untracked files.
Keep the user's index untouched. Do not commit or mutate the qualification
cluster. Treat private environment manifests as credentials, not review output.

Check shared immutable genesis bindings and CRD parity; role/key isolation;
external Go ownership versus offline-only JS; exact pinned contract deployment
and successful execution evidence; baseline pause/drain and activation ordering;
manager authorization and holder restrictions; PoX-4 compatibility; direct
PoX-5 enrollment/renewal and no replay after uncertain submission; and whether
completion genuinely observes production in the first PoX-5 reward cycle.

Verify the bridge initialization boundary leaves contract identities reusable
for a future real bridge without claiming DKG or daemon takeover. Check docs,
source/image pins and recorded qualification limitations against the code.

Run focused Go/race/SDK and API-server checks. If running make verify, use an
isolated indexed snapshot so generated-drift checks are meaningful and the
user's index is preserved. Separate checks you ran from inspected live evidence.
Report severity-ordered findings with concrete triggers, effects and source
locations; distinguish correctness defects from optional refinements.
```
