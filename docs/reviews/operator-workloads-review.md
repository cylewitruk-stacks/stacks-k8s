# Operator installation and capability workloads — review ledger

Status: **implemented and verified; ready for independent review**.

Base HEAD: `c85ccd6fbd8ed3efba3f1b4456d4850c68dfe020`. This follow-up is the
unstaged working-tree change over the user's staged managed-operation delivery.
No commit or staging operation was performed. Full verification used a temporary
populated index and checked that the real index remained unchanged.

## Delivery

| Area | Final behavior |
| --- | --- |
| Installation | One operator watches all namespaces by default; optional single-namespace scope and installation-local leader election. |
| Bitcoin | Operator owns policy scheduling; each target ledger owns a scoped RPC Deployment. |
| Transfers | Each transaction capability owns a worker with its account profile mounted. |
| Protocol maintenance | Contract and stacking capabilities own separate workers and exact assigned key mounts. |
| Receipts | Each managed network owns a keyless receiver and network-specific Service, filtered by network UID. |
| Authority | Workers bind namespace/name/UID; existing uncached admission, CAS authorization, nonce ownership and receipt rules remain authoritative. |
| Status | `WorkerReady` reports configuration and Deployment availability independently of protocol status. |
| Credentials | Same-namespace policy references replace installation-specific credential values; managed workers verify mounted key digest/account identity. |
| Disposal | Operator lifecycle controllers abandon retained Bitcoin/transfer/account ledgers without requiring an execution Pod; unknown effects remain unknown. |
| Withdrawal | Retained managed workers remain repairable for observation; withdrawn signing mounts are explicitly removed. |
| Language | Runtime remains Go; offline SDK adapters retain their narrow existing role. |

The operator provisions namespaced RBAC and therefore holds the permissions it
must delegate. Workers have namespace-bounded reads and named ledger/account
writes. Neither operator nor workers gain Secret API reads, exec, wildcard,
bind/escalate or cluster-RBAC mutation permissions. The chart installer creates
its own cluster-level role/binding. This is a trusted administrator installation,
not an untrusted tenant boundary.

## Expert review disposition

A fresh-context Kubernetes and Go reviewer inspected operator architecture and
implementation, then reviewed the corrections. It reported no remaining
actionable findings after the follow-up. Its review was static; it inspected
reported test logs but did not independently rerun the live qualification.

| Finding | Resolution and regression evidence |
| --- | --- |
| Worker outage could wedge environment deletion | Added operator-side `ledgerlifecycle` controllers, registered independently of provisioning switches. Unit tests retain ledgers on live parents/API errors. Envtest deletes finalized ledgers and parent without execution Pods, observes Abandoned plus retained dispatch/TxID/nonce, then proves final deletion. |
| Private image credentials no longer reached workers | `workers.pullSecrets` configures namespace-local Pod references. Rendering tests cover every worker component; no copying or Secret API reads. |
| Withdrawn capabilities could lose observation-worker recovery | Retained pinned accounts/capabilities can recreate keyless contract, stacking and receipt workers. Envtest verifies unchanged outstanding account evidence. |
| Existing signing mounts survived SSA omission | The reviewer requested an existing-Deployment assertion, which reproduced this issue. An ownership-checked CAS patch now replaces the credential projection before sparse SSA. Envtest asserts both volumes and mounts disappear before testing recreation. |
| Capability switches seemed to disable existing execution | Reviewer withdrew the runtime finding after checking the documented provisioning contract. Changed condition reason/help to `ProvisioningDisabled`, documented the Bitcoin scheduler distinction, and live-tested disable/delete/re-enable while another worker continued producing. |
| Live receipt Role update conflict | Force only the sparse declared SSA projection after exact ownership/deletion checks and an RV precondition. Envtest checks permission growth and repair of a changed owned Role; foreign-resource refusal remains tested. |

## Checks

| Check | Result / record |
| --- | --- |
| Full `make verify` after runtime corrections | Pass, including generated drift, module tests/vet/race, both envtests and chart policy; `/tmp/stacks-workers-verified-final.log` |
| Worker envtest with actual chart SA and RBAC authorizer | Pass under race; `/tmp/stacks-workers-final-envtest.log` |
| Focused lifecycle/workers/network tests | Pass; `/tmp/stacks-workers-review-unit2.log` |
| Exact chart RBAC, Helm and workload checks | Pass; `/tmp/stacks-workers-review-chart.log` |
| Vulnerability checks | No vulnerabilities reported; `/tmp/stacks-workers-vuln.log` |
| Container policy | Pass; `/tmp/stacks-workers-docker-check.log` |
| Final operator and SDK builds | Pass; `/tmp/stacks-workers-operator-r5-build.log`, `/tmp/stacks-workers-sdk-r5-build.log` |
| Markdown and documentation links | Pass, 28 changed Markdown files and contract suite; `/tmp/stacks-workers-final-markdown.log`, `/tmp/stacks-workers-final-doc-contract.log` |
| Two-network protocol progress | Both initialized, entered PoX-5 and renewed locks 500→620; `/tmp/stacks-workers-observe.log` |
| Final-image rollout and replacement | All five workers recreated in one namespace, operator replaced, ledger evidence unchanged, peer worker identities unchanged; `/tmp/stacks-workers-lifecycle-final.log` |
| Ordered environment disposal | Networks, retained ledgers and namespaces removed before shared operator uninstall; disposable cluster removed; `/tmp/stacks-workers-teardown.log` |
| Provisioning gates | Existing peer kept confirming; deleted paused worker remained absent until re-enabled; receipt identity preserved; `/tmp/stacks-workers-gates.log` |

The [qualification record](../network-operator/operator-workloads-qualification.md)
separates initial startup from final-image continuation, identifies the corrected
rollout-harness assertion and lists local evidence locations. Private environment
manifests contain keys and must not be included in review output.

## Review boundaries

- No promises of quiescence from Pod replacement, election or client timeout.
- Unknown submissions remain unresolved; rollout never resets nonce/dispatch state.
- Kubernetes 1.37.0 was exercised locally. This is not cross-version qualification.
- No new protocol fault injection, real private-registry authentication or
  multi-node placement test in this slice.
- Worker scheduling/resources currently use built-in defaults. Operator chart
  placement applies to the operator Pod, not to capability workers.
- Existing stopped `stacks-k8s` fixtures were not adopted, started or modified.

## Fable follow-up: lifecycle and reconciliation

The focused follow-up addresses Fable's F1–F5 without changing API schemas,
permissions granted, dependencies, Dockerfiles or the unpublished chart version.

| Finding | Final disposition |
| --- | --- |
| F1: disabled scheduler retained the policy root | Added `BitcoinBlockProduction` to unconditional lifecycle registration, sharing its finalizer name with the scheduler. Parent identity errors retain the record. Abandonment preserves policy identity and inventory before releasing only production retention. |
| F2: Secret defaults caused repeated credential patches | Render `SecretVolumeSource.defaultMode` explicitly as Kubernetes' existing `0644` default. Retain ownership-checked Create, sparse SSA and credential-removal CAS. Count actual patch calls after API defaulting: two unchanged reconciliations send zero merge patches and two SSA requests per mounted worker kind. |
| F3: worker status documentation | Document the four `WorkerReady` conditions and reasons; explicitly identify native Deployment status as the receipt-worker readiness source. |
| F4: initialization versus scheduling | Document that an existing Bitcoin worker can perform declared initialization without a scheduled opportunity; disabling scheduling is not a pause or revocation. |
| F5: help/imports/chart values | Correct component help, separate standard-library imports, document removed chart values and their replacements; keep local pre-release `0.1.0`. |
| Empty named account permission scope | Reject absent or empty account names before generating managed worker permissions. Tests cover each managed component and mixed valid/empty lists. |
| Upgrade and withdrawal nuances | Document policy pause/drain before execution-image changes and transfer-worker account-profile retention after withdrawal. |

The proposed switch to SSA creation was not adopted. A concurrent unowned object
must remain foreign even after an earlier NotFound read. The new create-race test
checks refusal without changing that object's labels or controller ownership.
Its first run accidentally reused a stored fake-client resource version; the
fixture now correctly renders a fresh create request.

Regression evidence:

- `ledgerlifecycle.TestDisabledProductionReleasesRootRetention` runs the aggregate
  with production disabled and no scheduler or workers. It verifies live-parent
  retention, Abandoned status, preserved UID/opportunity count, preservation of an
  inspection finalizer, then successful deletion against the API server.
- `workers.TestDefaultedWorkersAvoidCredentialMergeChurn` counts actual requests
  against envtest for Bitcoin, transfer, contract and stacking mounts. It also
  verifies removal and subsequent stability of existing credential projections.
- Existing workerintegration assertions still cover RBAC, Role repair, existing
  mount removal, keyless reconstruction and disposal of target/transfer ledgers.
- The three focused integration packages passed under race in
  `/tmp/stacks-fable-followup-envtest.log`; unit checks passed in
  `/tmp/stacks-fable-followup-unit.log`.
- Final full verification passed in
  `/tmp/stacks-fable-followup-verify-final.log`, using a temporary populated index.
  The real index remained unchanged. Changed Markdown passed lint in
  `/tmp/stacks-fable-followup-markdown.log`.
  The earlier run failed only on the corrected create-race fixture.

Live garbage-collection qualification used a deliberately suspended network and
paused Bitcoin policy in disposable cluster `stacks-fable-lifecycle`, with operator
image `stacks-network-operator:workers-fable`. The scheduler first recorded its
root finalizer and target inventory. After disabling production provisioning, the
live parent retained that finalizer. Deleting the network recorded Abandoned and
released production retention; removing only the test inspection finalizer then
allowed real GC to delete the root, target and worker Deployment. The namespace,
operator release and cluster were removed afterward.

This was a lifecycle-only fixture: credentials were intentionally unprovided, no
protocol execution was attempted, and no fresh PoX-5 qualification was claimed.
The original stopped `stacks-k8s` cluster was untouched. Public evidence and fixture:
`/tmp/stacks-fable-lifecycle/evidence.json`,
`/tmp/stacks-fable-lifecycle/fixture.json`; logs:
`/tmp/stacks-fable-lifecycle.log`, `/tmp/stacks-fable-uninstall.log`,
`/tmp/stacks-fable-cluster-delete.log`. No manual removal of a production finalizer
was used. The live policy UID was `909ae118-0f35-4196-ae0c-3baa7e16691b` and its
single retained target inventory survived abandonment.

Dependency/container checks from the delivery and Fable review remain applicable;
this pass did not change those inputs. Polling/cache optimization, independent Go
worker-image configuration and leader-election release timing remain deferred.
