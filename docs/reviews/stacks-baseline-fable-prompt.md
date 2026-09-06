# Fable review prompt

Copy the following into a fresh Fable session:

```text
Review the uncommitted steady-state Stacks implementation in:
/Users/cylwit/Code/github.com/cylewitruk-stacks/stacks-k8s

Base commit: d8af4aa0650d51f1f21bb660da569be8a81b0c3a
Subject: feat(network): add steady-state Bitcoin block production

You have no prior session context. Read AGENTS.md, README.md, then:
- docs/reviews/stacks-baseline-review.md
- docs/network-operator/stacks-production.md
- docs/network-operator/stacks-qualification.md
- docs/design/current-state.md
- docs/design/steady-state-operation.md
- docs/design/m0-remediation-plan.md (initial scope and Requirement 14)
- docs/design/roadmap.md (M1/M2)

Inspect git status and the entire diff against the base, including staged,
unstaged and new files. Review only; do not edit, commit or deploy changes.
Do not assume that staged files are the whole patch.

Product intent:
Modular disposable regtest networks for ordinary development, liveness,
reliability, resilience and efficiency testing, and trusted defensive research.
This slice provides normal productive Stacks operation, not adversarial actions.
StacksNetwork declares topology and ongoing desired behavior. It compiles owned
capability resources; separate controllers execute those capabilities. Agents or
external helpers sequence bootstrap. Observation stays passive and independent.

Prior baseline already implements fixed-cadence Bitcoin production with a durable
UID-pinned ledger and fail-closed response-loss behavior. Do not reinterpret that
existing design as newly introduced by this patch.

New delivery:
- StacksTransactionProduction: one ingress, one exclusive funded test account,
  tiny stx-transfer with explicit amount/fee and desired interval.
- Separate Deployment/ServiceAccount and locked offline Stacks SDK signer.
- Direct current identity admission; txindex/native RPC preflight.
- CAS exact TxID/nonce reservation before a single nonreplaying POST.
- Matching native rejection retains a bounded reason as Blocked, without releasing
  the nonce; malformed/unmatched responses remain ambiguous.
- At most one outstanding transaction; observe exact canonical inclusion before
  advancing the nonce; no blind retry or nonce-only success inference.
- Parent pause/update/removal, restart and unknown submission behavior.
- External fresh-environment Bitcoin funding, PoX-4 enrollment, and a bounded
  signer-renewal helper inspired by historical normal Hacknet configuration.
- APIs, generated CRDs, chart/RBAC, docs, unit/envtest/SDK/live coverage.

Review correctness and proportionality. Prioritize actionable defects over
requests for deferred machinery. Check:
1. Account/nonce exclusivity, CAS conflicts, lost write/POST acknowledgement,
   concurrent workers, restart, deletion, and remove/re-add identity pinning.
2. Exact inclusion semantics, failed execution, reorg/nonce drift, pending
   backpressure, mutable policy and paused/removed policy observation.
3. Current uncached target/config/runtime identity independent of global health.
4. No credential leakage into public resources or unintended actor mounts;
   least-privilege actual chart RBAC, readiness permissions and finalizers.
5. Native Core compatibility, fixed-length nonreplaying POST, offline signing,
   version-specific bootstrap, finite signer lock and renewal uncertainty.
6. Modular boundaries, generated artifacts, supported profile limits and honest
   documentation/qualification claims. Offered cadence is not a block guarantee.

Do not demand credential epochs, an execution-aware adapter, an in-cluster
bootstrap workflow, general PoX-5 support, or full M2 closure for this constrained
profile. Flag a defect if an enabled behavior relies on those deferred features.

Suggested checks, from the repository root:
  make verify
  make vuln
  make docker-check
  git diff --check
  git diff --cached --check

Focused checks can use go -C operators/network with GOWORK=off for transactions,
network, rbac and contract packages, plus make -C operators/network test-integration
and npm --prefix operators/network/transactions test. Markdown lint should cover
changed repository Markdown, excluding node_modules. Report what you independently
ran; do not repeat the author's live results as your own verification.

The author's dedicated local kind environment uses:
  kubeconfig: /tmp/stacks-m2-20260906.kubeconfig
  context: kind-stacks-m2-20260906
  namespace: stacks-m2-final
  network: stacks
Do not use your default kubeconfig or touch other clusters. Read-only inspection
of this explicit demo is acceptable if available. Do not run the live mutation
or abandonment suites during this review without separate authorization. Private
/tmp provisioning manifests contain keys and are not review inputs; do not print
or copy them. Public evidence locations are in the qualification record.

Return findings ordered by severity with file/line references, concrete failure
conditions, consequences and a minimal recommended correction. Distinguish
blocking defects from optional refinements. If no findings, say so explicitly.
Finish with the checks performed, limitations and readiness-to-commit verdict.
```
