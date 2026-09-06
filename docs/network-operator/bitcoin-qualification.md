# Initial Bitcoin baseline qualification

Local qualification on 2026-09-06 used Linux/arm64 Bitcoin Core 31.1 under
kind v0.32.0 with Kubernetes v1.36.1. This records the initial baseline slice,
not completion of M1 or a cross-platform support matrix.

| Artifact | Identity |
| --- | --- |
| Bitcoin image index | `bitcoin/bitcoin:31.1@sha256:da25cedc66b1daefff9f412ee196c901a899c3fa68a33b20849c3e08b5c40d63` |
| Bitcoin arm64 manifest | `sha256:1d3085b1057b004af3c5e5af40d353156c95a1d37c500f29b52ad524ed794403` |
| Tested local operator image ID | `sha256:5ce3d6becdfe9a2b6849b3e29d29d4771eb869e9fb3897e34fbe59306861f163` |
| Test-only delayed-receipt image ID | `sha256:30fd9d1a52f5695c585548fb4c1c0e61957582188c27044f0b87d88094379010` |
| Test-only dropped-receipt image ID | `sha256:28360c7cf30ac2b2ffcf27f80940ec2cb68ffbf672e1c5f3c2b7cadd4f03be7d` |

The [live suites](../../operators/network/internal/integration/bitcoin_live_test.go)
ran against dedicated namespaces in context `kind-stacks-baseline-20260905`,
using `/tmp/stacks-baseline-20260905.kubeconfig`:

| Test | Observed result |
| --- | --- |
| `TestLiveBitcoinBaseline` | Automatic production; pause; two-second cadence update; continued production from 7839 to at least 7842 acknowledged blocks with an unrelated unready Stacks node; successful producer rollout. |
| `TestLiveBitcoinDelayedReceipt` | A real Core receipt delayed 15 seconds was accounted exactly once under its original dispatch ID, with future production paused. |
| `TestLiveBitcoinInFlightShutdown` | The fixture confirmed Core had returned its receipt before producer Pod deletion. Manager logs confirmed one pending collector during SIGTERM drain; the same dispatch was accounted once and a replacement producer became ready. |
| `TestLiveBitcoinAmbiguousReceipt` | The fixture closed the response connection after Core returned. The ledger retained its original `Armed` dispatch with zero acknowledged blocks and remained `Blocked` across producer replacement. |
| `TestLiveBitcoinAbandonment` | Deleting the owning network released the unresolved ledger finalizer through the chart's actual RBAC. Chart and namespace removal followed. |

Separate manual RPC checks against the dropped-receipt fixture confirmed
actual Bitcoin height 1. The observer could read height but received HTTP 403
for generation; the producer received HTTP 403 for an unlisted read method.
Height remained 1 after these permission checks. These are direct RPC evidence,
not assertions made by the live Go suites.

Unit/race tests cover receipt retention through an accounting outage exceeding
five seconds, original receipt timestamps, collector capacity, shutdown drain
and exhaustion, lost local evidence after restart, competing producers, lost
API-write acknowledgements, accounting idempotency, and non-replaying HTTP
transport. Topology tests cover optional production failures without blocking
actor updates or pruning, independent policy updates, disabled-controller
status, and lifecycle-preserving watch filtering. Envtest covers parent CEL
retarget rejection, remove/re-add ledger identity, and actor updates after
ledger deletion. `make verify`, `make vuln`, and `make docker-check` passed;
the changed fixture also passed Docker build checks.

This replaces the 2026-09-05 receipt-loss qualification, which used a delayed
response exceeding the former client deadline. Delays and closed connections
now have separate tests. The ordinary baseline rollout alone does not prove
that shutdown overlapped a dispatch; the deliberate in-flight test does.

This does not qualify multi-target selection, finite actions, Stacks bootstrap,
transaction demand, arbitrary Chaos Mesh faults, hard server-process fencing,
or in-place recovery. See the [operating profile](bitcoin-production.md) for
admission, recovery, and teardown limits.
