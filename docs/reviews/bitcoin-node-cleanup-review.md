# Bitcoin node cleanup review

## Boundary

Base: `75ae35d89b00c2d7a9edce344c31ef78230635ff`.
The cleanup is staged separately from the subsequent action-operator work.

## Review ledger

| Area | Delivered | Evidence |
| --- | --- | --- |
| API and compiler | Bitcoin nodes and templates have no mining role; Stacks roles remain. | Schema assertions and API-server leaf round trips. |
| Workloads | Bitcoin omits the role environment variable. | Renderer and Bitcoin leaf tests; Stacks role assertion retained. |
| Identity | Bitcoin wire identities omit role; Stacks identities require it. | Both independent inventory consumers, shared vectors, observer read-path regression. |
| Examples and design | Bitcoin instance naming; production owns block emission. Superseded role contracts and archive removed. | Current contract suite, relative-link checks, Markdown lint. |
| Qualification | Generated artifacts, independent modules, unit/race/envtest, charts, dependencies and containers. | `make verify`, `make vuln`, `make docker-check` all pass. |

No live cluster was changed or requalified for this cleanup. This unreleased
API change has no compatibility shim. The optional role property in shared
status schemas still serves Stacks identities.

## Fable prompt

```text
Review the staged BitcoinNode cleanup in this repository as a fresh reviewer.
Read AGENTS.md, docs/design/README.md, and this ledger first. The base is
75ae35d89b00c2d7a9edce344c31ef78230635ff. Subsequent unstaged action-operator
work is a separate delivery; review the staged snapshot in an isolated copy
without modifying the user's index.

Confirm that BitcoinNode and aggregate Bitcoin templates have no mining-role
semantics, while Stacks roles still work. Trace compiler -> workload -> leaf
identity -> aggregate inventory -> independent observability decoding. Check
canonical digest fixtures and generated CRDs, including absent versus empty
role values. Review deleted contract tests against the deleted superseded
fixtures: current production/action and identity coverage must remain real.
Check examples and current roadmap/design for contradictions or stale links.

Run relevant tests and make verify against the snapshot where feasible.
Report severity-ordered findings with paths, concrete triggers, consequences,
and minimal fixes; state the exact snapshot and validation limitations.
Do not stage, commit, or change a running environment.
```
