# Fable review prompt

Review the current working tree in:
`/Users/cylwit/Code/github.com/cylewitruk-stacks/stacks-k8s`.

You are a fresh reviewer. Read `AGENTS.md`,
`docs/design/steady-state-operation.md`,
`docs/design/managed-network-operation.md`,
`docs/design/operator-workloads.md` and
`docs/reviews/operator-workloads-review.md` first.

The user staged the prior managed-operation delivery. This operator/workload
refactor and its corrections are unstaged/untracked on top of that baseline;
HEAD is `c85ccd6fbd8ed3efba3f1b4456d4850c68dfe020`. Review the final combined
working tree, distinguishing new regressions from staged pre-existing behavior.
Do not alter the user's index, commit, edit fixtures or mutate clusters.

The intended architecture is one operator installation watching independent
`StacksNetwork` resources across namespaces. The aggregate compiles actor and
capability CRs; resource-focused controllers provision scoped execution
Deployments. Go owns runtime behavior; Node.js remains offline SDK encoding and
signing only. Stacking administration remains separate from consensus signers.

Focus on concrete correctness and Kubernetes/Go design issues:

1. Cluster-wide versus optional namespace scope, leader election and independence
   of network identities under one install.
2. Sparse workload reconciliation, foreign-resource refusal, resource-version
   preconditions, default stability, Role updates and actual removal of old
   credential mounts from existing Deployments.
3. Per-worker namespace/name/UID binding, same-namespace key/pull-secret mounts,
   retained account assignment, exact chart/worker RBAC and lack of Secret API reads.
4. Preservation of Bitcoin authorization/receipt state and Stacks nonce/TxID
   authority through retries, replacement, rollout and worker loss.
5. Operator-side administrative abandonment/finalizer release when workers are
   absent, without claiming RPC cancellation or clearing unknown effects.
6. Withdrawal and pause: observation/receipt draining remains possible, withdrawn
   managed workers are keyless, and provisioning switches do not silently become
   a second execution pause policy. Bitcoin's switch also gates scheduling.
7. Small controller boundaries, Go idioms, independently versioned modules,
   generated API artifacts, docs and examples.

The fresh-context Kubernetes/Go reviewer already found lifecycle, registry and
withdrawal gaps; its resolutions and regressions are in the ledger. Independently
verify those claims rather than treating the earlier verdict as evidence.

Run focused tests plus full `make verify` in an isolated snapshot with a populated
index if feasible. The drift check needs a meaningful index; never stage the real
working tree to satisfy it. `internal/workerintegration` uses envtest with the
rendered chart ServiceAccount and RBAC authorization. SDK checks use the existing
locked dependencies. Clearly distinguish checks run from code/log inspection.

Use `docs/network-operator/operator-workloads-qualification.md` for live evidence
and limitations. Qualification used a disposable cluster, not the retained
stopped `stacks-k8s` fixtures. Do not print private environment manifests or keys.

Report severity-ordered findings with exact locations, trigger, consequence,
minimal recommended correction and regression test. Identify architectural
tradeoffs separately from defects. If no actionable findings remain, say so and
list verification limits. Do not make a commit.

## Focused follow-up to your F1–F5 review

Start with the ledger's final “Fable follow-up” section. Check these corrections:

- All three retained production resource kinds have unconditional lifecycle
  registration; scheduling remains gated. Root abandonment retains identity and
  inventory and removes only its own finalizer after status accounting succeeds.
- The Secret-volume default is explicit; unchanged worker applies send no extra
  credential merge patches. The request-count test uses a real API server, not
  resource-version equality. Existing credential removal must still work.
- Create remains ownership-safe under a raced, unowned foreign object. SSA
  creation was deliberately not introduced to address defaulting churn.
- Managed permission construction rejects empty account-name scopes.
- Worker conditions, receipt readiness, initialization, upgrade drain, withdrawal,
  removed chart values and CLI/import corrections match actual behavior.

New integration packages `internal/ledgerlifecycle` and `internal/workers` are
included in `make -C operators/network test-integration`. Run them alongside
`internal/workerintegration`, preferably under race. The disabled-production live
fixture/evidence is public and deliberately contains no credentials. It proves
real GC behavior, not RPC production. No new dependency or API schema changes.
