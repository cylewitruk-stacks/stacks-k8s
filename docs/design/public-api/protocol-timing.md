# Bootstrap timing and evidence

Status: source-grounded proposed profile, **not live qualification**. This is fixed
initialization convergence, not a user-authored sequence of actions. The worker image
must pin the same protocol behavior before this profile can be implemented/qualified.

## Source and height conventions

This derivation uses the clean Core checkout at revision
[f9b022bff5550d1e9938e1f40805ea354381a59e](https://github.com/stacks-network/stacks-core/tree/f9b022bff5550d1e9938e1f40805ea354381a59e).
The local example image tag is not proof that it contains this revision; a release
must record/qualify its actual image digest and source revision together.

Regtest first burn height F is 0. For cycle length L=20 and prepare length P=5,
cycle C is floor((height-F)/L). Nakamoto signing starts at F+C*L; classic rewards
start one block later. Core's prepare predicate uses offsets greater than L-P,
plus offset 0 for classic preparation. The PoX-5 contract freezes next-cycle edits
at offset L-P **inclusive**. RPC blocks_until_prepare_phase follows that earlier
contract boundary. These are different observations, not interchangeable clocks.
See Core's [cycle predicates](https://github.com/stacks-network/stacks-core/blob/f9b022bff5550d1e9938e1f40805ea354381a59e/stackslib/src/burnchains/mod.rs#L615-L743)
and [PoX-5 freeze rule](https://github.com/stacks-network/stacks-core/blob/f9b022bff5550d1e9938e1f40805ea354381a59e/stackslib/src/chainstate/stacks/boot/pox-5.clar#L2945-L2959).

The first Nakamoto cycle uses the legacy anchor's reward set. Its anchor can be at
or before prepare-end minus P, not a lock discovered immediately before epoch 3.0.
See [anchor selection](https://github.com/stacks-network/stacks-core/blob/f9b022bff5550d1e9938e1f40805ea354381a59e/stackslib/src/chainstate/burn/db/sortdb.rs#L2542-L2554)
and [initial signer-set selection](https://github.com/stacks-network/stacks-core/blob/f9b022bff5550d1e9938e1f40805ea354381a59e/stackslib/src/chainstate/coordinator/mod.rs#L316-L349).

PoX tip RPC switches strictly after activation height. The first PoX-5 waterfall
cycle is the cycle after the one containing activation, not necessarily the cycle
in which epoch 4.0 begins. See [activation and cycle dispatch](https://github.com/stacks-network/stacks-core/blob/f9b022bff5550d1e9938e1f40805ea354381a59e/stackslib/src/burnchains/mod.rs#L389-L450)
and [first waterfall block](https://github.com/stacks-network/stacks-core/blob/f9b022bff5550d1e9938e1f40805ea354381a59e/stackslib/src/burnchains/mod.rs#L628-L642).

The initial managed cohort is captured from explicit network participants when
StacksGenesis is created. Only verified selected actors satisfy protocol prerequisites.
Later removal cannot shrink an unfinished gate's required cohort; it blocks that gate.
Late additions are not retroactive replacements, and completed gates never rewind.
Gate requirements are the public values in StacksGenesis.spec.bootstrap, including
stake amounts and cycle coverage. [Conflicting policy edits](lifecycle.md#bootstrap-policy-updates)
wait for their affected gates; controller restart never substitutes current intent.

## Illustrative schedule and gates

The [complete epoch schedule](resources.md#stacksepochschedule) provides a legacy
enrollment window for six holders between epoch 2.5 at 209 and epoch 3.0 at 252.
The window does not guarantee their inclusion rate.

| Stage / reported evidence | Bitcoin ceiling or release boundary |
| --- | --- |
| PrepareBitcoin: imported wallets, mature miner outputs, synchronized initial Core views | Reach 203, then start Stacks actors and workers. |
| EnrollPoX4: native PoX-4 active after 209; successful canonical enrollments with exact holder/key/amount covering cycle 12 | Ceiling 234 is a conservative one-block margin before the latest eligible anchor at 235; confirm these facts before release. |
| PrepareNakamoto: canonical prepared cycle-12 signer set matches the required cohort | Permit Core prepare 236..240; ceiling 251 guards crossing epoch 3.0 at 252. Never require this finalized set while still held at 234. |
| PreparePoX5: pinned real sBTC contracts and exact initialized registry | Deploy after canonical Nakamoto header observation confirms Clarity-3 support, not Bitcoin height alone; ceiling 281 protects crossing epoch 4.0 at 282. No direct-manager or PoX-5 enrollment requirement yet. |
| EnrollPoX5: actual epoch 4.0 boot state and PoX-5 tip RPC after 282; managers and exact cycle-15 enrollment | Hold at 284, within the reward phase, while Stacks blocks include management transactions. The contract's next-cycle edit cutoff is 295 exclusive; do not substitute the later Core prepare boundary. |
| PrepareWaterfall: canonical prepared cycle-15 signer set | Permit Core prepare 296..299; hold at 299 until the prepared set is observed, then allow signing/waterfall boundary 300. |
| Running: fresh canonical transaction inclusion and intended cohort production after 300 | Release bootstrap ceilings; continue maintenance. Merely seeing pox-5 in RPC is insufficient. |

Initial PoX-4 enrollments may occur on either side of a reward-cycle boundary.
Choose start/lock coverage from observed protocol state and verify **cycle 12**;
never require every holder's transaction to have the same submission cycle.
PoX-4 signing inputs must agree with the execution cycle; an enrollment crossing a
cycle boundary can be rejected. Detect the actual outcome, never silently resign or
replay a potentially submitted transaction. See [execution-cycle validation](https://github.com/stacks-network/stacks-core/blob/f9b022bff5550d1e9938e1f40805ea354381a59e/stackslib/src/chainstate/stacks/boot/pox-4.clar#L580-L605).
Desired
lockCycles must cover the required cycle; incompatible schedules/policies are
InvalidBootstrapWindow before activation. Generic locked > 0 does not satisfy this.

Before Nakamoto, a Stacks transaction needs Bitcoin advancement for inclusion. The
producer admits one confirmation block at a time within the remaining enrollment
window, with current actor readiness and outstanding authorized enrollment observed.
It does not stop all Bitcoin movement while waiting for a receipt that needs it.
Before the first Stacks anchor, fresh native empty-chain observations also authorize
single-block advancement within the same ceiling: the miner's commitment must be
confirmed before PoX becomes readable. Missing or failed observations do not grant
this allowance, and an established chain cannot regain it by losing its data.
At the legacy enrollment ceiling, persist reachedAt and allow 120s for already
possible inclusion/catch-up, including across controller restarts. In the proposed v1
policy this is wall-clock time: the deadline remains reachedAt + 120s during an
acknowledged network or worker pause. Pause/resume never resets or extends it. A pause
for inspection at this hold can therefore end in BootstrapWindowExhausted; observations
may establish success while paused, but Bitcoin advancement still waits for resume.
Here this means propagation/processing of a Stacks block already selected by a burn block at or below
234. A transaction still needing another burn-block sortition cannot gain inclusion
while held at 234. It must be submitted in time for that last permitted selection;
submitting near 233 is not an inclusion guarantee. The 120s adds observation time,
not another confirmation opportunity or an extension of the enrollment window. If required
inclusion remains unproven, report Failed/BootstrapWindowExhausted and withdraw new
initialization activity, retaining pending/unknown transaction truth; the observation
timeout does not cancel a submission. Do not cross because a transaction is pending. A required
cycle missed on canonical state reports
BootstrapWindowMissed; uncertain observations report BootstrapHold/Unknown. The user
can inspect and dispose of the failed attempt; there is no rewind or nonce replay.

Ceilings constrain every new baseline/action generation and serialized initialization
dispatch, including work across multiple Bitcoin targets. Bootstrap uses one active
initialization target and connected, converging views; conflicting tips or unknown
outstanding generation close advancement rather than racing a multi-target height
check. Weighted baseline opportunities begin after these initialization gates.
Bounded actions cannot bypass a hold or authorize overshoot. No ceiling fences an
external administrator or modified actor that independently generates blocks; those
inputs can invalidate bootstrap and must surface the missed boundary.

## What readiness proves

Every release uses current canonical observations and the captured identity bindings;
reorged-away enrollment or stale status does not release a gate. Do not roll back
already completed initialization after later experiment faults; report divergence.

PoX-5 enrollment verifies exact deployed manager source/holder binding, registered
consensus key/authorization, successful canonical stake outcome, holder coverage of
cycle 15, manager membership and delegated amount for that cycle. Prepared-set checks
use native [stacker-set RPC](https://github.com/stacks-network/stacks-core/blob/f9b022bff5550d1e9938e1f40805ea354381a59e/stackslib/src/net/api/getstackers.rs#L77-L113)
and compare the intended signer identities and protocol-derived weights, rather than
inventing weights from spec. Core also [computes signers during epoch 2](https://github.com/stacks-network/stacks-core/blob/f9b022bff5550d1e9938e1f40805ea354381a59e/stackslib/src/chainstate/stacks/db/blocks.rs#L5188-L5203),
so this observation does not intrinsically require Nakamoto first. Report
missing/NotAvailableYet as waiting. Manager
contracts require Clarity 6 and the epoch-4.0 boot trait, so they cannot gate entry
into that epoch. The pinned [sBTC manifest](../../../operators/network/internal/protocolcontracts/sbtc-contracts.json)
requires Clarity 3 and can deploy earlier; the [direct manager template](../../../operators/network/internal/protocolcontracts/templates/direct-signer.clar.tmpl)
uses the new boot trait. [Core language versions](https://github.com/stacks-network/stacks-core/blob/f9b022bff5550d1e9938e1f40805ea354381a59e/clarity-types/src/version.rs#L107-L112)
define the corresponding epoch gates.

The profile compiler derives gates from F/L/P, epoch schedule and supported contract
semantics. It rejects insufficient ordering/coverage and gate cycles that require
future observations. Heights above are an explicit example, not universal epoch-N
formulas. Qualify initial anchor selection, six-holder capacity, exact prepared-set
RPC availability before the release heights, contract deployment with Bitcoin held,
and uninterrupted production across the first waterfall boundary. If the pinned
image cannot supply those observations in time, revise/qualify the profile before
claiming support; do not silently weaken readiness.
