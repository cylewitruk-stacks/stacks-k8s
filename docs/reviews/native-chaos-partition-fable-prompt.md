# Fable review prompt

Review the focused follow-up to the native actor-partition delivery in
`/Users/cylwit/Code/github.com/cylewitruk-stacks/stacks-k8s`.
You are starting without prior session context. Read `AGENTS.md`, root README,
`docs/design/current-state.md`, the M0.5 ledger and Requirements 6–8 in
`docs/design/m0-remediation-plan.md`, and `docs/design/chaos-mesh.md` first.
Then read `docs/reviews/native-chaos-partition-review.md` and
`docs/chaos/partition-qualification.md` for the delivery/evidence boundary.

Base HEAD is `6c5632767005b741d95a7ba1638fa9422ec14eb3`. Review all changes relative
to that base, including untracked files; confirm the actual index/worktree
boundary before testing. Do not stage, commit, change signing configuration or
alter running environments. Use an isolated snapshot and populated temporary
index for generated-drift checks if necessary. Report defects by severity with
file/line, trigger, consequence and minimal correction. Distinguish test-quality
gaps, product defects and explicitly deferred scope.

The review ledger's "Focused Fable follow-up" table maps your previous findings
to revisions. Prioritize the new mirrored RPC helper, captured initial-confirmation
gate, shared observer/proxy queries, PoX decoder and timeout diagnostics. Check
that absent fields become evidence gaps and that derived phase labels do not
conflate lagging node state with Core's current height. The fresh Stacks run
passed; the retained paused fixture intentionally produced an exit-1 baseline
timeout before injection. Neither result resolves the original stalled run.
The worker now copies action module manifests only and has a new actual build.
No public API, runtime, permission or dependency changes are in this follow-up.

Focus on:

- Independent mode enablement, six shared chart resources, exact additive RBAC,
  one existing-object quota, version prerequisite, and chart 0.2.0 enrollment/name
  upgrade ordering.
- CEL creation bounds versus UPDATE immutability and preserved legacy/de-enrolled
  cleanup. Check upstream defaults, optional fields, both selector sides,
  partition directions and rejection of mixed mechanisms or producer selectors.
- Live evidence that Bitcoin peers separate while both producer receipt paths
  work, then converge to the observed heavier branch after cancellation/expiry.
  Check stable-tip sampling and actor process identity assertions.
- A fresh Stacks confirmation after the test starts, and new confirmations after miner/Bitcoin link
  recovery in the initial run. The repeat expiry case failed at a reward-cycle
  transition after native cleanup; inspect its documented limitation and retained
  strict assertion. Do not treat it as a clean qualification or infer root cause
  from the log message. Native conditions alone are not protocol evidence.
- Administrator-only control-loss fixture: no preflight mutation; receipt truly
  returned by Core; drain exhausted with pending work; leader replacement waits;
  unchanged ambiguous dispatch and no extra Core blocks after native cleanup.
  Do not mistake this fixture's authority or receipt proxy for a served feature.
- Transaction-worker Dockerfile local-module closure and actual CI build coverage.
  No controller or JavaScript logic is added, and operator modules stay separate.
- Documentation claims versus automated assertions, dated live measurements and
  explicitly unqualified cases. M0.5 must remain open for wider scope.

Run `make verify`, focused chart-policy unit/race and admission envtests, Helm,
Markdown and whitespace checks as appropriate. The upstream CRD is checksum
verified; `STACKS_CHAOS_CRD_FILE` can name the matching cached file. Review the
recorded follow-up live logs read-only if available. Successful follow-up
fixtures are retired; the original stalled fixture remains preserved. Do not rerun live suites without
separate authorization: they mutate production policies, faults and the
control-test producer Pod, and the control fixture must be fresh. Do not print
private provisioning manifests or Secret contents. State exactly what you
independently ran and what evidence you only inspected.
