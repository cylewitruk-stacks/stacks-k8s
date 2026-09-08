# Operator and capability workload qualification

Date: 2026-09-08 (Europe/Stockholm). Local Kubernetes 1.37.0, arm64, one-node kind
cluster `stacks-workers-qualification`. One Helm release in
`stacks-network-system` managed two independently generated networks named `stacks`
in namespaces `workers-one` and `workers-two`.

The separate cluster qualified cluster-wide installation without adopting the
retained fixtures in the stopped `stacks-k8s` cluster. Those fixtures were untouched.

## Artifacts and scope

- Final operator: `stacks-network-operator:workers-r5`.
- Final SDK worker: `stacks-transaction-worker:workers-r5`.
- Actors: `stacks-core:4.0.1-pox5`, self-reported Stacks 4.0.1, revision `62e03cc`;
  `bitcoin/bitcoin:31.1` at the repository's pinned digest.
- sBTC sources: revision `d31b0780cf1ae91b6ef4ed7ee89400c796aea913`, checked by
  the environment renderer against the source manifest.
- Each network had its own genesis accounts, configuration and credentials, four
  actor Pods and five capability worker Deployments. No external bootstrap or
  renewal command, manual mining, nonce reset or transaction replay was used.

Initial startup used worker image revision `workers-r2`; the live Role update fix
was rolled out as operator `workers-r3`. Both networks reached PoX-5 and renewal
before the final `workers-r5` rollout. Final-image qualification covers continued
operation, replacement, isolation and disposal; it is not a second fresh-genesis
startup run.

## Observed behavior

| Check | Evidence |
| --- | --- |
| Independent identity | Network UIDs `cdee371c-2523-4004-8f40-627111b6c2e8` and `c995ad68-395c-49b3-8045-de4d2b21eac5`; same resource names in separate namespaces. |
| Managed initialization | Both Bitcoin ledgers progressed through initialization; both contract sets became Ready and Stacks produced confirmed transfers. |
| Protocol transition | Both nodes reported `pox-4`, then `pox-5`; each stacking participant observed an initial PoX-5 unlock height of 500. |
| Ongoing renewal | Both participants subsequently observed unlock height 620, with continuing native Stacks chain progress. |
| Pause and final-image rollout | Networks paused at Bitcoin heights 407/408, 95 confirmed transfers each. The final rollout preserved account transactions, transfer nonce/TxID/count, Bitcoin dispatch/receipt fields and ledger UIDs. |
| Worker and operator replacement | Deleted all five worker Deployments in `workers-one` and the operator Pod. The operator recreated all workers; ledger evidence stayed unchanged. `workers-two` worker Pod UIDs stayed unchanged. |
| Provisioning switches | Disabling transfer provisioning left an existing peer confirming. A deleted paused worker stayed absent for ten seconds, then was recreated after re-enabling; TxID evidence survived and policy resume confirmed another transfer. |
| Independent resume | Resuming only `workers-two` advanced its confirmed count 95→96 while `workers-one` remained unchanged. Resuming `workers-one` then advanced its count 95→96. |

Public observation and lifecycle snapshots are under
`/tmp/stacks-workers-qualification/`. Private `environment.json` manifests in its
namespace directories contain signing material and must not be published.

The initial lifecycle harness checked Deployment readiness before the controller
had projected the final image, so its replacement assertion failed. The corrected
harness also waited for the requested images and disappearance of the old worker
Pod UIDs, then passed against the same paused ledgers. No network recovery or
recreation was used to satisfy the assertion.

## Regression and permission evidence

The API-server suite runs the controllers as the chart's actual ServiceAccount,
with RBAC authorization enabled. It verifies independent provisioning, scoped
worker writes, forbidden Secret reads/cross-namespace access/foreign ledger writes,
stable defaulted resources, Role repair, removal of signing mounts from existing
withdrawn workers, keyless reconstruction and worker-independent ledger disposal.
These are envtest assertions, not live RPC cancellation experiments.

The live startup exposed Create/Apply ownership conflicts as receipt account
permissions grew. Regression coverage now checks changing Role permissions and
credential removal from existing Deployments, including Kubernetes field ownership.
The final operator enforces its sparse desired projection after ownership and
resource-version checks; credential projection removal uses explicit CAS patches.

## Evidence files

| Evidence | Local record |
| --- | --- |
| Startup and chain observations | `/tmp/stacks-workers-observe.log`, `observations.jsonl` |
| Final rollout, replacement and independent resume | `/tmp/stacks-workers-lifecycle-final.log`, `lifecycle.json` |
| Provisioning switch qualification | `/tmp/stacks-workers-gates.log`, `provisioning-gates.json` |
| API-server regression | `/tmp/stacks-workers-final-envtest.log` |
| Full repository verification | `/tmp/stacks-workers-verified-final.log` |
| Dependency and container checks | `/tmp/stacks-workers-vuln.log`, `/tmp/stacks-workers-docker-check.log` |

JSON filenames above are relative to `/tmp/stacks-workers-qualification/`.
Registry authentication against a real private registry, multi-node placement,
crash fencing and protocol fault injection were not qualified in this slice.
Image pull references are covered by rendering tests; existing unknown-effect
recovery restrictions still apply.

## Disposal

Both networks were paused and drained, then deleted. Retained Bitcoin production,
transfer and account ledgers were released before namespace removal. The shared
operator release was uninstalled after both namespaces were gone. Records:
`/tmp/stacks-workers-final-pause.log`, `/tmp/stacks-workers-teardown.log` and
`/tmp/stacks-workers-qualification/disposal.json`.

The disposable cluster was then removed successfully
(`/tmp/stacks-workers-cluster-delete.log`). The original three `stacks-k8s` node
containers remained stopped and unchanged.

## Focused disabled-production disposal follow-up

A separate lifecycle-only check used Kubernetes 1.37.0 cluster
`stacks-fable-lifecycle` and operator `stacks-network-operator:workers-fable`.
The fixture was suspended, its Bitcoin policy paused, and credentials intentionally
unprovided; no RPC or Stacks protocol qualification was attempted.

With scheduling enabled, the controller recorded production-policy retention and
one target ledger/worker. After upgrading with `bitcoinProduction.enabled=false`,
the live parent still retained the policy. Network deletion then produced
`Abandoned` and released production retention. An extra test finalizer kept the
record inspectable; removing only that test finalizer allowed Kubernetes GC to
remove the policy, target and worker. Namespace disposal and operator uninstall
succeeded, and the disposable cluster was removed.

Policy UID `909ae118-0f35-4196-ae0c-3baa7e16691b` and its retained target inventory
were unchanged through abandonment. Evidence:
`/tmp/stacks-fable-lifecycle/evidence.json`, `/tmp/stacks-fable-lifecycle.log`,
`/tmp/stacks-fable-cluster-delete.log`. This closes the disabled-scheduler/real-GC
coverage gap; it does not expand the earlier protocol qualification claims.
