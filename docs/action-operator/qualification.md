# Initial action-operator qualification

Qualified on 2026-09-06 in a fresh local kind cluster, Kubernetes 1.36.1 on
Linux arm64, with Bitcoin Core 31.1. Existing clusters and CRDs were unchanged.

| Artifact | Qualified identity |
| --- | --- |
| Network image | `stacks-network-operator:action-split-20260906`, `sha256:eba32da3902b6a7497e14a6fafa1df255d03f444a30655b1fc752151658b6bdc` |
| Action image | `stacks-action-operator:baseline-20260906`, `sha256:5733be369d34b8f2dbd552c196ee360f42cc5967004bbefa3c6a50a3d616045e` |
| Bitcoin image | `bitcoin/bitcoin:31.1@sha256:da25cedc66b1daefff9f412ee196c901a899c3fa68a33b20849c3e08b5c40d63` |
| Cluster / namespace | `stacks-actions-20260906` / `action-operator-live` |

## Evidence

| Check | Result |
| --- | --- |
| Baseline without actions | Three baseline receipts observed before any action CRD or lifecycle Deployment was installed. |
| Independent lifecycle outage | Action Deployment scaled to zero after the first receipt; the worker retained two acknowledged receipts and the reservation. Restart projected `Completed`, then permitted release/finalizer removal. |
| Finite generation | Three receipts, baseline exclusion, latest pause/cadence, unrelated unready actor, worker restart between receipts and baseline resumption passed. |
| Reorganization | Higher-work local replacement, old branch retained as `valid-fork`, cleanup acknowledgement and a compensated deadline after two replacement receipts passed. |
| API-server authorization | Rendered generation-only chart Role permitted real admission/status/finalizer writes and restart; Secret reads, Pod deletion, topology/ledger writes and disabled-kind listing were forbidden. |
| Repository checks | `make verify`, `make vuln`, `make docker-check`, both operator image builds, and changed Markdown lint passed. |

The three live tests passed together in 238 seconds, including initial quota
discovery. Their automated assertions live in the network integration package;
the chart-authorized envtest lives in the action module. No controller/RPC
implementation is duplicated for the paired executor/lifecycle tests.

Reproduce the live assertions only in an explicitly provisioned disposable
network with both action capabilities enabled:

```bash
STACKS_NETWORK_LIVE_KUBECONFIG=/tmp/stacks-actions-20260906.kubeconfig \
STACKS_NETWORK_LIVE_CONTEXT=kind-stacks-actions-20260906 \
STACKS_BITCOIN_LIVE_NAMESPACE=action-operator-live \
STACKS_BITCOIN_LIVE_NETWORK=bitcoin \
GOWORK=off go -C operators/network test -tags=live,bitcoinproduction \
  -run '^(TestLiveIndependentActionOperator|TestLiveFiniteGeneration|TestLiveReorganizationAndCompensatedDeadline)$' \
  -count=1 -v ./internal/integration
```

These tests create fixed-name actions and require a fresh environment for a
repeat run. They mutate baseline settings and restart controller Pods or scale
the action Deployment. The qualification is one local platform/profile, not a
published compatibility matrix. Delayed/dropped-RPC live variants were not
rerun for this extraction; existing unit/race regressions still cover retained
ambiguity and cleanup boundaries. No new RPC mechanism was introduced.

## Startup prerequisite follow-up

The review follow-up adds pre-start validation of enabled action APIs. Unit
checks cover independent flags, the required served version and preserved
discovery failures. `TestExecutorActionPrerequisites` uses a real API server:
missing kinds fail setup before manager startup; baseline reconciles with no
action CRDs; generation-only, reorganization-only and combined managers perform
fresh reconciliations after the action CRDs are installed. Full `make verify`
and changed Markdown lint pass. This follow-up was not deployed to the live
cluster, and the image identities above still identify the original delivery.
