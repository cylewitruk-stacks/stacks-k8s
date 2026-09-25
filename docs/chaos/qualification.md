# Native delay qualification

The 2026-09-07 results below are historical legacy-runtime evidence obtained with the now-retired
stacks-k8s fault profile. The legacy fixture does not qualify the replacement
runtime. Current experiments use [native Chaos Mesh](operations.md) directly.

Requalified 2026-09-07 in the task-owned `kind-stacks-chaos-20260906` cluster,
namespace `chaos-live`, network `chaos`. Existing demo clusters were untouched.
This qualifies a narrow native `NetworkChaos` delay profile, not all of M0.5.

## Platform and inputs

| Component | Qualified value |
| --- | --- |
| Kubernetes | kind, `kindest/node:v1.36.1`, one control-plane node |
| Node userspace / kernel | Debian 13 / LinuxKit `7.0.12-linuxkit`, arm64 |
| Container runtime / CNI | containerd `2.3.1` / kindnet |
| Chaos Mesh | External chart and images `2.8.4`, namespace filtering enabled, dashboard/DNS disabled |
| Controller image ID | `ghcr.io/chaos-mesh/chaos-mesh@sha256:9e48285bb44c63be80f2fe4468c54b0f2d7cdc447ba5f5bc9ded4ab6a973cd37` |
| Daemon image ID | `ghcr.io/chaos-mesh/chaos-daemon@sha256:0d28dbd95b033279f65605ea88c354d25fe902409197274016dd2efc28ef25cd` |
| Network operator | Source HEAD `6de2a10`, local image `stacks-network-operator:chaos-20260906`, build ID `sha256:4905560b4255872e6dc652c5697a12aea85c5ecc34d83527f2ade3b74d53517a` |
| Bitcoin | `bitcoin/bitcoin:31.1@sha256:da25cedc66b1daefff9f412ee196c901a899c3fa68a33b20849c3e08b5c40d63`, two regtest actors, weighted fixed 3 s baseline |
| Selected actors | `bitcoin` → `bitcoin-2`; logical labels on both sides, `mode: one`, `direction: to` |
| Fault | 500 ms delay, zero jitter/correlation, 30 s duration |

The immutable observed image IDs above record the actual run. Upstream values
pin version tags; they do not claim digest-locked images on every architecture.
The downloaded chart SHA-256 is
`ae4abd385649771300e4d33a44627c0df3618be0780c385bf30cc2fdf2ad93fa`.
The unmodified upstream NetworkChaos CRD SHA-256 is
`d151d3d38cfb4e906df457a2927a8bad8872241655b0267e08e3315b1b615974`.

Sources: [upstream 2.8.4 release](https://github.com/chaos-mesh/chaos-mesh/releases/tag/v2.8.4),
[pinned CRD](https://github.com/chaos-mesh/chaos-mesh/blob/v2.8.4/helm/chaos-mesh/crds/chaos-mesh.org_networkchaos.yaml),
[network fault semantics](https://chaos-mesh.org/docs/simulate-network-chaos-on-kubernetes/),
and [namespace filtering](https://chaos-mesh.org/docs/configure-enabled-namespace/).

## Automated evidence

`TestLiveNativeDelay` passed against the installed upstream controller, daemon,
webhook and CRD, with the profile's Deny binding and real ResourceQuota enabled.
The test samples three actor RPC calls per measurement and compares medians.
It asserts native injection, measured latency increase, a new destination ledger
receipt during injection, quota rejection of a second object, and measured
recovery. The expiry case also replaces the Chaos Mesh controller and verifies
that delay persists before expiry recovery. Destination Pod identity stays fixed.

| Case | Actor RPC median before | During | After | Destination receipts during injection |
| --- | --- | --- | --- | --- |
| Explicit deletion | 76.60 ms | 1069.68 ms | 75.50 ms | 261 → 262 |
| Duration expiry, including controller replacement | 76.60 ms | 1097.20 ms | 59.26 ms | 262 → 263 |

The injected source Pod UID was `0735c45d-b058-424b-a97d-e0e90aab7894`;
destination UID was `954c032a-8ac3-43ef-9b27-2db8da7f7316`. Native fault UIDs were
`1b8207a0-c713-4bb0-b27d-9c900cc93663` (deletion) and
`7e145bc9-dec3-4af2-b924-f5c5ce2fddae` (expiry). The post-restart controller Pod UID
was `e1ef3d0c-519a-48fd-ba64-182f82f73c06`.

The control-path evidence is acknowledged production progress from the
unselected producer Pod during measured actor-link delay. It is not a claim of
constant control latency or successful production through a total network outage.

Envtest separately checks the rendered CEL against the checksum-verified
upstream schema, permitted native creation and deletion through the agent
identity, denied permissions, malformed and cross-namespace selectors, timing
bounds, immutable specs, and cleanup writes after de-enrollment. The follow-up
also seeds nonconforming faults before policy installation and
verifies simulated controller cleanup writes and real finalized deletion while
spec edits and new unsupported objects are rejected. This is API-server lifecycle
evidence, not live injection/recovery of those legacy faults. The real quota
controller is absent from envtest; quota enforcement is asserted in the live test.
The live suite also requires current-generation CEL type checking with no
expression warnings; envtest has no type-checking controller. Structured status
fields use explicit presence checks. The upstream webhook's empty
`status.experiment` default is explicitly covered;
this CRD has no separate status subresource.

## Manual dependency-removal check (initial delivery)

On 2026-09-06, after the initial suite left no native faults, the Chaos Mesh release was
uninstalled. Destination receipts advanced from 184 to 185 while the native
controller/daemon were absent; the network retained `Ready=True` and
`ProductionConfigured=True`. The pinned release was then reinstalled. This
checks baseline independence, not a new observability qualification.

## Limits and retained artifacts

Other kinds, network fault modes, Stacks-to-Bitcoin combinations, multiple nodes,
other CNIs/architectures, actor replacement, daemon/node failure, management-path
loss, passive journal correlation, and automatic divergence/capture-gap handling
remain unqualified. Native expiry requires functioning cleanup infrastructure.
The requested duration is not a hard wall-clock recovery guarantee.

The live suite leaves no active native faults. After qualification, the task
network was retained with `spec.bitcoinBlockProduction.paused=true`; actors and
the pinned Chaos Mesh installation remain available for review. Set that field
to false before repeating the live suite. Its task-local log is
`/tmp/stacks-chaos-followup-live.log`; API admission, full verification, dependency and
container logs use the `/tmp/stacks-chaos-` prefix. These temporary files are
review aids, not a shipped evidence-retention service. Reproduce using the
[legacy live-suite guide](operations.md#opt-in-live-checks).

## Direct native PodChaos smoke — 2026-09-24

On the shared three-node kind/Kubernetes 1.37.0 cluster with Chaos Mesh 2.8.4,
a disposable namespace was annotated `chaos-mesh.org/inject=enabled`. A single
synthetically labeled `pause:3.10` Pod was selected by actor-role and network-UID
labels. A 20-second
native `PodChaos` `pod-failure` with the same fault-object network UID and a
correlation ID passed server dry-run and creation without any stacks-k8s profile
release, admission policy or agent Role. Chaos Mesh reported `AllInjected=True`;
its Events recorded successful apply and recovery, and finalized deletion
completed. The Pod retained UID `dad79eee-e259-4ba0-8aa3-f1c3e3321b79` and was
Running after recovery. Fault UID was
`47463c99-b705-4284-b031-6ef995830dbd`. The namespace was deleted afterward.

This checks direct native admission and cleanup on that cluster. It does not
qualify protocol impact, agent RBAC, or the expanded observability recorder
binary, which was not rolled out for this smoke test.
