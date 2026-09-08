# Fable review prompt

Review the complete working tree in
`/Users/cylwit/Code/github.com/cylewitruk-stacks/stacks-k8s` against HEAD
`c85ccd6fbd8ed3efba3f1b4456d4850c68dfe020`. It includes a previously staged
PoX-5 delivery plus an unstaged controller refactor and new files. Preserve the
user's index and worktree; do not stage, commit or change retained environments.

Read `AGENTS.md`, `docs/design/managed-network-operation.md`,
`docs/reviews/managed-operation-review.md`, and
`docs/network-operator/managed-operation-qualification.md` first.

The intended contract is perpetual supported network operation owned by
`StacksNetwork`, compiled into focused capability resources. Controllers handle
initialization, contract prerequisites, direct stacking and renewal. External
agents own experiments. Funding does not enroll an account, and the epoch
schedule determines PoX-5 activation. Go owns runtime behavior; the existing JS
package performs offline SDK encoding/signing only. Separate worker Deployments
are deliberate; stacking administration is not a consensus-signer sidecar.

Prioritize correctness over style:

- Account/consumer identity, exclusive nonce ownership, concurrent CAS, lost
  acknowledgements and restarts. Unknown work must not be resubmitted.
- Exact receipt accounting and consumer acknowledgement, including the bounded
  legacy callback path, current ingress/source checks and canonical ancestry.
- PoX-4 activation/unlock boundaries, PoX-5 manager registration and renewal,
  contract dependencies, source equality and initial registry ownership.
- Startup gates versus ongoing health: completed stages must not silently stop
  Bitcoin during later protocol failures. User pause remains desired intent.
- Immutable genesis and extra unused funded accounts; remove/re-add behavior,
  topology independence, deletion/finalizers and teardown.
- Shared CEL/generated schemas, precise RBAC, language/module boundaries,
  workload packaging and documented compatibility/evidence limits.

Run focused Go/race and SDK tests, real API-server tests and chart checks.
For `make verify`, use an isolated copy with a populated index or a temporary
`GIT_INDEX_FILE` initialized from the reviewed working tree: generated-drift
checks must compare against that tree without altering the user's staged base.
Do not run live mutations unless separately authorized. Distinguish tests you
ran from inspected live evidence, and identify any unverified claims.

Return severity-ordered findings with concrete triggers, consequences,
file/line references and proportional fixes; then verification limits and a
commit-readiness verdict. The ledger is a review aid, not proof of correctness.
