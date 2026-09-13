# Public API delivery and qualification backlog

Iteration 2's replacement implementation is delivered. It includes composable
participants, controller-managed bootstrap and maintenance, pure-Go workers,
bounded actions, updated observation identities and retirement of the old runtime.
The [qualification record](../../network-operator/public-api-qualification.md)
identifies measured behavior and failures by image and fixture. Implementation
completion does not mean every original combined acceptance scenario passed.

Controllers can initialize from empty storage through PoX-5. One later node joined
from new storage and advanced on the cohort's canonical chain. Earlier full-cohort
runs exercised enrollment, renewal, actions and faults; those results belong to
their recorded images and sequences, not to one universal final-image qualification.
The small fixture is test input, not a product mode or external bootstrap requirement.

## Remaining qualification work

These are bounded follow-ups to the existing architecture, not a planned iteration-3
redesign. Select one question and define its acceptance before starting a run.

| Priority | Question and scope | Completion evidence |
| --- | --- | --- |
| Next | Bootstrap and fresh-node catch-up reliability: isolate the earlier missing-anchor/enrollment failures without assuming the ceiling correction caused their resolution. | A bounded reproduction or discriminating comparison, recorded image/schedule/topology and a supported operating envelope. Retain unsuccessful outcomes; passing retries alone do not identify a cause or establish reliability. |
| Next | Combined topology and independent-upgrade qualification under [M3](../roadmap.md#m3-multi-actor-topology-and-independent-upgrades): cover the full cohort and supported add/remove/roll/suspend sequences on current images. | Explicit image pairs, retained/new PVC expectations, exact identities and fresh protocol progress after each operation. Do not combine stages from different runs into a single pass. |
| Targeted | PoX-5 enrollment boundary: the corrected live run enrolled around 285, without holding at 294. | A deliberately slow-enrollment test of native acceptance before cutoff, hold at 294 and deadline expiry. Source/unit evidence remains sufficient to describe the current algorithm, but is not live boundary evidence. |
| Later | Broader compatibility and release profiles under [M0.8](../m0-remediation-plan.md) and the [roadmap](../roadmap.md): images, storage, Kubernetes platforms and fault combinations. | A declared support matrix backed by the corresponding runtime and protocol checks; no compatibility inference from Pod readiness alone. |

Detailed historical symptoms remain in the qualification record. New ledgers should
reference that evidence and update the relevant row when its question is resolved.
No failure is attributed to Core or to the operator without supporting evidence.

## Boundaries that remain deliberate

Bound Stacks management-worker loss is terminal; automatic nonce recovery is not
pending qualification. Shared-key experiments have no cross-worker coordination.
Deterministic replay, arbitrary fault recovery and historical-run retention are not
promised. Broader telemetry, instrumented actors and other product capabilities
remain in the roadmap; closing iteration 2 does not close M0 or all M1–M8 milestones.
