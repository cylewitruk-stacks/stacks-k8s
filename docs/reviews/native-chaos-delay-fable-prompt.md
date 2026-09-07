# Fable review prompt

```text
Please independently review the entire uncommitted native Chaos Mesh delay delivery.

Repository:
/Users/cylwit/Code/github.com/cylewitruk-stacks/stacks-k8s
Expected base HEAD: 6de2a10bd1a765875d5d3821207475f4086cd07a
User-staged initial delivery tree: 5a319dbeeecd8b17a5d680ca277667d2678206db
Follow-up boundary: unstaged diff on top of that staged base.

Read AGENTS.md first, then:
- docs/reviews/native-chaos-delay-review.md
- docs/chaos/operations.md and qualification.md
- charts/stacks-chaos-profile/README.md
- docs/design/chaos-mesh.md and the relevant M0/roadmap changes

Confirm HEAD, index and working-tree boundaries before testing. Include all
untracked files. Preserve the user's index; do not stage, commit or modify code.
Use an isolated snapshot or temporary populated index for generated-drift checks.

Context: external agents orchestrate investigations. Operators expose composable
capabilities; generic faults use upstream CRDs directly. This delivery adds no
wrapper CRD or runtime controller. It closes previously reviewed initial M0.3/M0.4
bookkeeping and implements one optional, bounded native NetworkChaos delay profile.
Broader M0.5 work is explicitly still open.

This follow-up addresses your prior review. Check creation-only constraints
(oldObject != null), retained update immutability, cleanup of faults created
before installation, actual CI job/trigger wiring, fresh quota input, complete
rendered match checks, checksum diagnostics and consistent status vocabulary.
The legacy cleanup regression uses real API-server deletion/finalizers with
simulated controller writes; it does not run a native controller in envtest.

Inspect:
1. Static admission: complete source/target selectors, namespace confinement,
   exact actor labels, direction/modes, duration/latency bounds, immutable specs,
   no alternate targeting or seeded lifecycle state, upstream webhook defaults.
   Live tests require current-generation CEL type checking without warnings;
   envtest has no type-checking controller and cannot supply that evidence.
2. Namespace binding stays effective after de-enrollment; existing metadata,
   native status writes and deletion remain possible. Upstream namespace filtering
   is a separate cleanup prerequisite. NetworkChaos 2.8.4 has no status subresource.
3. Agent Role/binding exactness, additive-RBAC limits, no Workflow/Schedule or other
   mutation grants, and one-existing-object quota rather than active-fault count.
4. Test evidence: envtest uses a checksum-verified unmodified upstream CRD; live
   tests measure real actor latency, producer receipts during injection, cancellation,
   expiry, controller replacement and real quota rejection. Distinguish test claims
   from manual dependency-removal evidence and unqualified cases.
5. Independent module/chart boundaries, disabled defaults, pinned dependency
   prerequisites, no vendored upstream CRDs, and accurate operational teardown.
6. M0.3/M0.4 closure remains limited to initial reviewed profiles; broader recovery
   gates and other native kinds/platforms/observation capabilities stay deferred.

Run make verify, relevant tagged vet/race/envtest and Markdown/link checks if
available. Envtest downloads the pinned upstream CRD, or accepts its exact bytes
through STACKS_CHAOS_CRD_FILE. Do not substitute a toy schema or weaken checks.

Live tests are opt-in and mutate a specifically provisioned disposable cluster,
including restarting its native controller. Do not run them against unrelated
contexts. Review recorded evidence first and state any live-check limitations.
No need to repeat unchanged Docker builds merely to inspect the patch.

Report severity-ordered findings with concrete triggers, consequences and file/line
references. Separate correctness findings from optional refinements. Verify that
new tests assert behavior, not just implementation-shaped transitions. Give an
explicit commit-readiness verdict and list the checks you actually ran.
```
