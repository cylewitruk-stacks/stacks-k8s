# Local Kubernetes 1.37 qualification

Tested 2026-09-07 with standalone kind 0.33.0, Kubernetes 1.37.0,
Docker Desktop on arm64, containerd and kindnet. The checked-in three-node
configuration created `stacks-k8s`; all commands selected its private kubeconfig.
No commands targeted the unrelated `local` cluster.

Cluster lifecycle, installation, Bitcoin actions, native Bitcoin faults and
ordinary Stacks transfers passed. **Stacks recovery after partition expiry
failed** despite completed native cleanup. This is not a blanket compatibility
or protocol-recovery qualification; M0.5 remains open.

`make verify`, image builds, Markdown lint and staged/unstaged whitespace checks
passed. Dependency and container checks passed in the preceding upgrade pass;
this live qualification changed documentation only.

## Inputs

Images were built from the staged Kubernetes upgrade and local-cluster helpers.
The observability image was built but not deployed in these live checks.

| Image | Local content ID |
| --- | --- |
| `stacks-network-operator:verify` | `sha256:f217a4782af22684026de8794de5d67c1ca6380e7aa8471b9fb3fee3783bb089` |
| `stacks-action-operator:verify` | `sha256:49c6216091419074c456093177b745b85e2cc9f2631a191d79b326e3f1d8aeb9` |
| `stacks-transaction-worker:verify` | `sha256:2ff097f8e2b5e7b85cf13ed6b137bde63fd2834bd6b67c9c282ecff953f33f72` |
| `stacks-core-attacknet:a12-normal` | `sha256:5b0283ee442bc02dd73807cc9b70ec94b6fd339b82d3de0d863a76c118272b81` |

Bitcoin used the pinned Core 31.1 image; Chaos Mesh used the checksum-verified
2.8.4 chart and checked-in kind/containerd values. The normal Stacks artifact
and its source-attestation limits are described in the
[initial Stacks qualification](network-operator/stacks-qualification.md#images-and-inputs).

## Live results

| Check | Result |
| --- | --- |
| `cluster-create`, `cluster-stop`, `cluster-start` | Passed; three nodes returned Ready and the local kubeconfig remained usable. |
| `cluster-chaos-install`, repeated | Passed; the same release advanced from revision 1 to 2. |
| Independent action controller restart | Passed; two receipts retained until lifecycle acknowledgement. |
| Finite Bitcoin generation | Passed; counts, baseline exclusion, latest policy and idle worker restart verified. |
| Bitcoin reorganization | Passed in correctly provisioned `local-reorg`; higher-work replacement and compensated deadline verified. |
| Native delay across workers | Passed cancellation and expiry, including Chaos controller restart; median RPC 58.74 ms → 1060.34 ms → 52.84 ms. |
| Native Bitcoin partition across workers | Passed cancellation and expiry; bidirectional RPC loss, independent production and heavier-chain convergence verified. |
| Stacks bootstrap and transfers | Passed; four exact inclusions, pause/resume, cadence edit and pending-worker replacement verified. |
| Stacks/Bitcoin partition cancellation | Passed; confirmed transfers advanced 26 → 27 after cleanup. |
| Stacks/Bitcoin partition expiry | Failed its 120-second protocol recovery wait; native cleanup passed. |
| Strictly ordered final cleanup | Passed in fresh `local-cleanup`: producers drained, network and ledgers removed, namespace confirmed absent, Chaos Mesh uninstalled, then cluster destroyed. |

The live suites are `TestLiveIndependentActionOperator`,
`TestLiveFiniteGeneration`, `TestLiveReorganizationAndCompensatedDeadline`,
`TestLiveNativeDelay`, `TestLiveBitcoinPartition`, `TestLiveStacksTransfers`, and
`TestLiveStacksBitcoinPartition`. Commands and fixture requirements remain in the
[operator](action-operator/qualification.md) and [native-fault](chaos/operations.md) guides.

## Stacks recovery limit

The fresh baseline advanced from 25 to 26 confirmed transfers before injection.
The 45-second expiry fault UID was `97f92088-62b0-4271-acb8-d056daf9b5db`.
At the recovery timeout Core was at height 320; both Stacks nodes reported burn
height 300. Transfers remained at 27 confirmed, with nonce 27 outstanding and
TxID `41f2ad57f3e8b0ca09859b76458a5dafb97521f3e8a3414b3a58416a4dde6a8b`.
Miner logs reported a missing canonical PoX anchor for cycle 15 while processing
burn block 301. No native faults remained. No test condition was relaxed.

The [earlier 1.36.1 run](chaos/partition-qualification.md#repeat-run-recovery-limit)
showed the same class of symptom at another reward cycle. Neither observation
establishes the cause or attributes it to Kubernetes 1.37.

## Execution corrections and evidence limits

The initial `local-actions` manifest omitted `--reorganization`. Its reorg request
correctly stayed unadmitted and expired; the combined run therefore failed.
The same suite passed in fresh `local-reorg` with the required RPC grants.
Its two preceding generation/lifecycle tests had passed in `local-actions`.

The initial teardown removed every network's ledgers and actor leaves, but
cluster deletion began before the final `local-stacks` namespace wait completed.
That wait failed. A fresh productive `local-cleanup` fixture separately passed
strictly ordered removal. Both test clusters are removed; the final Docker
inventory contains no `stacks-k8s` nodes.

Logs also contain a non-failing test-client logger initialization warning and
denied Bitcoin-worker leader-election Event writes. They are not evidence of
failed leader election; controller progress was observed.

Public API snapshots and actor/test logs are retained locally under
`~/Code/stacks-k8s-evidence/20260907-local-cluster-137-public/`, with SHA-256 checksums.
That archive excludes Secrets, private provisioning manifests, images and PVCs;
it is diagnostic evidence, not a restorable failed fixture. Private manifests
remain under `/tmp/stacks-k8s-local-live/`, outside Git. Raw run logs use the
`/tmp/stacks-k8s-local-live-*` and `/tmp/stacks-k8s-local-cleanup-*` prefixes.
