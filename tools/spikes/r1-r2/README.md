# R1/R2 feasibility checks

These checks support the [decision proposal](../../../docs/design/target-admission-and-rpc-execution.md).
They are local design probes, not production controllers or release qualification.
No new Go module or runtime dependency is introduced.
The spike model is manual-only and is not run by make verify. It enumerates
selected serial state transitions; it does not prove concurrent execution,
Kubernetes durability, credential fencing, reset recovery, or R4 cleanup.
Withdrawal of proven-unsent authorization and qualified reset epoch retirement
are also unmodeled transitions.

## Run

```bash
GOWORK=off go test tools/spikes/r1-r2/exclusion_test.go
GOWORK=off go -C operators/network test ./internal/network \
  -run 'TestBitcoinDeclarationDigestTracksTargetInputs|TestInventoryIsCompleteStableAndWithdrawnOnPartialAdmission'
sh tools/spikes/r1-r2/bitcoin-rpc-check.sh
```

The Bitcoin probe uses an installed image, no external networking, published
ports, host mounts, or retained volumes. Cookie credentials and the disposable
wallet remain inside its temporary container filesystem. It queues one
generation behind a four-second read-only wait with one RPC worker.
Its generation client waits one second. The container is removed on exit.

The default image is bitcoin/bitcoin:31.1. Set BITCOIN_PROBE_IMAGE to check
another installed image or pin a registry digest. The probe fails
if the expected client timeout or final height is absent; it does not silently
skip unavailable Docker or incompatible images.

## Evidence recorded on 2026-09-05

| Check | Result | Limit |
| --- | --- | --- |
| Existing inventory withdrawal test | Pass | Fake Kubernetes client; complete inventory is withheld when a follower becomes unready. |
| Compiled Bitcoin declaration digest | Pass | Actual compiler/digest code; unrelated Stacks image edit leaves it unchanged, target image edit changes it. |
| Revised reservation/dispatch model | Pass | Selected serial CAS interleavings, process replacement, receipt accounting, dependent RPCs, and explicit release; not concurrency or recovery qualification. |
| Bitcoin 25.2 queued generation | Client timeout followed by height 0 to 1, with zero peers | One local Linux arm64 container and one request; not all cancellation or execution cases. |
| Bitcoin 31.1 queued generation | Client timeout followed by height 0 to 1, with zero peers | Same bounded check on the current release; not broader release qualification. |

Existing example-version image:

```text
bitcoin/bitcoin:25.2
sha256:14b4777166cba8de36b62ce72801038760a8f490122781b66d40592c8c69ebda
linux/arm64
Bitcoin Core version v25.2.0
Docker Engine 29.7.2
```

The SHA above is the local image ID, not a registry manifest digest.

Current-version image, pinned by Linux arm64 registry manifest digest:

```text
bitcoin/bitcoin@sha256:1d3085b1057b004af3c5e5af40d353156c95a1d37c500f29b52ad524ed794403
linux/arm64
Bitcoin Core daemon version v31.1.0 bitcoind
Docker Engine 29.7.2
```

The [31.1 release](https://bitcoincore.org/en/releases/31.1/) is the current-version
basis for this check. Existing repository example images are unchanged.

Abbreviated observed result on both versions (client diagnostics edited):

```text
peers=0
height_before=0
client_result: timeout reached
height_after=1
PASS: one queued generation completed after client timeout with zero peers
```

This demonstrates late execution despite client timeout, not a Bitcoin defect.
It also demonstrates regtest generation without peers; it makes no mainnet
miner or arbitrary network-partition claim.

## Source basis and remaining qualification

- [Aggregate observation](../../../operators/network/internal/network/reconciler.go)
  publishes full inventory only after all desired actors are ready.
- [Leaf observation](../../../operators/network/internal/workload/engine.go)
  reports leaf generation, revision, Pod, image, and configuration identity.
- [Aggregate compiler](../../../operators/network/internal/network/compiler.go)
  and [spec digest](../../../operators/network/internal/workload/config.go)
  supply the proposed declaration-catalog inputs.
- [Bitcoin Core v31.1 generation source](https://github.com/bitcoin/bitcoin/blob/v31.1/src/rpc/mining.cpp)
  shows synchronous generation using node interruption state, not a caller
  deadline. The runtime check independently demonstrates queued execution
  after client timeout.
- [client-go leader election](https://pkg.go.dev/k8s.io/client-go/tools/leaderelection)
  explicitly does not guarantee execution fencing.
- [Bitcoin Core 31.1 cookie generation](https://github.com/bitcoin/bitcoin/blob/v31.1/src/rpc/request.cpp)
  and [authentication setup](https://github.com/bitcoin/bitcoin/blob/v31.1/src/httprpc.cpp)
  support investigating per-process credential epochs. This is source evidence;
  the Docker probe above does not exercise rotation or reset recovery.

Future implementation tests must use restricted ServiceAccounts and a real
API server for CAS, stale reads, record loss, and status persistence. Real
endpoint/credential rotation, container restart, stale process resumption,
and supported CNI fault behavior remain qualification work. The in-memory
model deliberately supplies no time-based recovery transition.
The reset candidate still needs real process-termination and credential tests,
including old credentials against the replacement, static-credential fallback,
prohibited credential refresh, and missing/partitioned-kubelet evidence. The
model assumes receipt accounting and cleanup acknowledgements are truthful;
it tests their ordering, not their external implementation.
