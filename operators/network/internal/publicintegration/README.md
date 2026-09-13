# Public API live qualification

This opt-in Go test drives the `v1alpha2` baseline through public Kubernetes APIs.
It loads the documented [30-actor
declarations](../../../../docs/design/public-api/examples/30-actors.yaml),
creates a fresh namespace, and applies reusable declarations before the network.
No existing namespace is adopted. The default `minimal14` variant selects three
Bitcoin nodes, one miner, two signer nodes, two signers, two stackers, production,
traffic, the sBTC contract set and faucet. `full30` retains all 30 actors and their
management participants. Unselected reusable definitions remain reusable objects.

The fixed assertion sequence is:

1. All frozen initialization gates complete and `Initialized`/`Operational` become true.
2. Every current Bitcoin and Stacks node reports fresh native height advancement;
   attributable Bitcoin generation and successful native traffic inclusion advance.
3. An optional explicitly selected operator Deployment rolls while worker Pod UIDs
   and process nonces remain unchanged.
4. Pause is acknowledged; managed Bitcoin receipt counters remain stable while
   workers survive. Resume restores operational status and native progress.
5. Terminal Stop is acknowledged, root deletion completes, and all reusable
   declarations retain their original UIDs before namespace cleanup.

Native progress uses the controllers' public native observations, checked against
current process, genesis and execution bindings. The optional Chaos stage additionally
makes independent unauthenticated HTTP probes. Pause does not assert chain quiescence:
previously submitted transactions may complete. Protocol gate deadlines remain unchanged;
harness timeouts do not extend the 120-second legacy ceiling hold. A root `Failed` immediately
ends normal qualification and starts bounded cleanup.

## Run

Install the replacement operator and required images in the selected cluster first.
The harness does not install or reconfigure the operator. Use an explicit kubeconfig,
context, unused namespace, actor image versions and the exact operator Deployment identity:

```bash
STACKS_PUBLIC_LIVE=1 \
STACKS_PUBLIC_KUBECONFIG="$KUBECONFIG" \
STACKS_PUBLIC_CONTEXT=kind-stacks-k8s \
STACKS_PUBLIC_NAMESPACE=public-live-unique \
STACKS_PUBLIC_OPERATOR_NAMESPACE=stacks-network-system \
STACKS_PUBLIC_OPERATOR_NAME="$OPERATOR_DEPLOYMENT" \
STACKS_PUBLIC_OPERATOR_UID="$OPERATOR_DEPLOYMENT_UID" \
STACKS_PUBLIC_BITCOIN_IMAGE=bitcoin/bitcoin:31.1 \
STACKS_PUBLIC_STACKS_IMAGE=stacks-core:iteration2-f9b022 \
STACKS_PUBLIC_CADENCE=5s \
make -C operators/network test-live-public
```

The signer image defaults to the explicitly supplied Stacks image; override it with
`STACKS_PUBLIC_SIGNER_IMAGE`. Empty images and `:latest` are rejected. The installed
operator selects its fixed worker image; this test does not change that profile.

| Option | Default | Purpose |
| --- | --- | --- |
| `STACKS_PUBLIC_RENEWAL` | Disabled | Set `1` to await a fresh settled cohort, then observe continuous native PoX-5 lock extension for every selected stacker and subsequent chain/traffic progress. |
| `STACKS_PUBLIC_REJECTIONS` | Disabled | Set `1` to induce a native traffic fee refusal, verify zero pending work and cooperative pause, then restore the fee and require inclusion in the same process. |
| `STACKS_PUBLIC_FAUCET` | Disabled | Set `1` to qualify two native faucet inclusions and retained no-resubmission observations. |
| `STACKS_PUBLIC_ACTIONS` | Disabled | Set `1` to qualify bounded native actions after optional faucet checks. |
| `STACKS_PUBLIC_CHAOS` | Disabled | Set `1` to qualify native delay/partition after optional actor/action checks. |
| `STACKS_PUBLIC_ACTORS` | Disabled | Set `1` for a late follower lifecycle/storage check in either variant. |
| `STACKS_PUBLIC_FRESH_JOIN` | Disabled | Set `1` for a new-storage follower joining after PoX-5 initialization, matching the cohort's canonical tip and advancing in the same process. Exclusive with the actor lifecycle/upgrade options. |
| `STACKS_PUBLIC_VARIANT` | `minimal14` | Select `minimal14` or `full30`. |
| `STACKS_PUBLIC_FIXTURE` | Documented YAML | Optional YAML or Kubernetes JSON `List`; `/tmp/stacks-iteration2-first-fixture.json` is supported when present. |
| `STACKS_PUBLIC_CADENCE` | `5s` | Explicit fixed Bitcoin cadence; no automatic acceleration. |
| `STACKS_PUBLIC_TIMEOUT` | `45m` | Total normal lifecycle observation budget. |
| `STACKS_PUBLIC_PROGRESS_TIMEOUT` | `5m` | Progress, pause acknowledgement and operator rollout bound. |
| `STACKS_PUBLIC_PAUSE_WINDOW` | `20s` | Observation after cooperative pause acknowledgement. |
| `STACKS_PUBLIC_CLEANUP_TIMEOUT` | `5m` | Separate bounded cleanup attempt. |
| `STACKS_PUBLIC_EVIDENCE_DIR` | New temporary directory | Public stage snapshots, latest observation and `events.jsonl`. |
| `STACKS_PUBLIC_NODE_MAP` | Unchanged fixture placement | JSON map from example hostnames to selected cluster hostnames. |

Every run requires `STACKS_PUBLIC_OPERATOR_NAMESPACE`, `STACKS_PUBLIC_OPERATOR_NAME`
and `STACKS_PUBLIC_OPERATOR_UID` for read-only evidence capture before fixture creation.
`operator-artifact` records the Deployment identity/requested images and the owned
ReplicaSet/Pod/container identities, including runtime-reported image IDs. Environment
variables and mounted data are excluded. Image IDs are reported observations, not
proof of source provenance; concurrent rollout may expose multiple image versions.
`genesis-artifact` records the exact frozen genesis UID/digest and bootstrap gates
as soon as the root publishes the artifact. Both events are saved in `events.jsonl`.

For an operator restart check, additionally set `STACKS_PUBLIC_OPERATOR_RESTART=1`.
Selecting its identity alone never requests a rollout. The test changes only
one Pod-template annotation, requires rollout readiness, and rejects changes to
its container configuration. It does not execute a shell command or change images.

Cleanup attempts Stop followed by background root deletion; the optional repeat
check directly deletes a running root with foreground propagation. Both retain
reusable declarations until namespace deletion. Cleanup also runs after an assertion
failure. SIGINT/SIGTERM delivered to the test process cancel the normal wait and
run cleanup with its independent timeout; forced termination cannot run cleanup.
Normal qualification stages fail immediately on confirmed loss/replacement of the
bound root or explicit Stop/deletion. Transient API failures remain retryable and
missing startup participant observations remain pending. No failed read counts as
successful progress. Cleanup never removes finalizers or automatically preserves a failed fixture.
If the public lifecycle cannot finish within the bound, the error identifies the
remaining namespace/UID and evidence path. Inspect that evidence before taking
further cleanup action. No Secret data or mounted signing material is collected.

## Optional faucet qualification

Set `STACKS_PUBLIC_FAUCET=1` to run faucet checks after initialization/native progress
and before pause/resume. The fixture must select exactly one faucet and retain the
resolved `traffic-recipient` account. Fresh reads bind the actual network,
participant, worker Pod/process and public account identities. The test creates two
independent user-owned requests for **1 micro-STX each**, using the public logical
`faucetRef` and account recipient fields; exact worker/participant UIDs are checked
in controller admission and worker execution, not invented in the request spec.

Both requests must report worker `Completed` / `Included` evidence with distinct
TxIDs and native inclusion block IDs. Request/status snapshots are saved before
normal deletion. Metadata reobservation must preserve both exact outcomes; after
deletion, the same worker must retain exactly two additional offered/included/
completed counters through a fresh heartbeat beyond the configured pause window.
This is a bounded no-resubmission observation, not a claim of transaction finality
or indefinite future behavior. The test does not expect deleted completed requests
to populate `LastDeletedOutcome`, which serves outstanding deleted-request work.
No raw signing key, Secret value, or direct submission endpoint is used by the test.

## Optional Bitcoin action qualification

Set `STACKS_PUBLIC_ACTIONS=1` after installing the v1alpha2 action chart in the
fixture namespace and enabling generation and reorganization in the network
operator. Installation remains external to this test. Missing CRDs fail before
request creation; a controller that has not observed its request fails after 30s.

The test creates a two-block generation request, then a depth-one reorganization
with explicit boundary opt-ins. Each request pins the root UID and logical Bitcoin
node; admission must match the participant, actor process, configuration, credential
reference and execution record captured immediately before creation. The payout
comes from a ready wallet's public observation. No key material or direct RPC is used.

Completion requires exact receipts, current lifecycle conditions and native chain
observations. Reorganization additionally requires a contiguous replacement suffix,
greater chainwork and acknowledged reconsideration of the captured invalidity marker.
After reservation release, terminal accounting must remain stable and the action
must not reacquire execution authority for one observation-freshness window. Baseline
receipts may continue. This bounded observation does not claim indefinite replay
prevention or canonical membership for every generation receipt. Public request,
status, execution and native-view evidence remains in the evidence directory.

The action qualification selects one baseline production target and waits for other
targets to settle outstanding RPCs or expire unsent opportunities before creating requests. Reorganization
uses target-local exclusion; independently producing peers can invalidate its captured
tip. The original baseline is restored after successful action checks.

## Deliberate bound-worker eviction

Use a separate fresh fixture for this terminal-failure qualification:

```bash
STACKS_PUBLIC_LIVE=1 STACKS_PUBLIC_WORKER_EVICTION=1 \
go -C operators/network test -tags=live ./internal/publicintegration \
  -run '^TestPublicBoundWorkerEviction$' -count=1 -timeout=60m
```

Supply the same explicit cluster, namespace and image inputs as the baseline test.
Eviction mode skips the baseline success test. After initialization and native
progress, it selects a fresh active transaction-production worker and submits one
`policy/v1` Eviction with an exact Pod UID precondition. It requires root failure,
retention of the original participant/worker binding, and no replacement Pod or
process restart. Once exact termination is observed, execution status must remain
unchanged throughout `STACKS_PUBLIC_PAUSE_WINDOW`. This is a bounded observation;
in-flight settlement before termination is permitted.

Public root, participant, Pod and termination evidence is retained before normal
Stop/root-deletion/namespace cleanup. A missing Pod without retained termination
evidence reports `TerminationUnknown`; the test never clears finalizers or claims
that disappearance proves termination. Cleanup errors identify the retained
namespace and evidence directory.

## Optional native Chaos qualification

`STACKS_PUBLIC_CHAOS=1` requires external Chaos Mesh 2.8.4 and the compatible
`stacks-chaos-profile` chart with delay and partition enabled in the fixture
namespace. Enroll that exact namespace with label
`network.stacks.org/chaos-profile=network-faults-v1` and annotation
`chaos-mesh.org/inject=enabled` while initialization runs. The harness verifies
current policy compilation and admission; it does not install or enroll anything.

The selected Stacks actor image must provide `curl`, and the test host must provide
`kubectl` with permission to exec into the selected actors. A diagnostic image may
add curl to the exact pinned Core image without changing its Core binary; that is
not mixed-version qualification. No ephemeral container, key mount or Secret read
is used. The bounded progress check requires a baseline cadence of at most 10s;
the reference 5s cadence is supported.

Two non-mining Stacks actors are selected by exact network UID, participant UID,
actor name and `role=actor` on both selector sides. The test observes native
`AllInjected` plus a 500ms delay increasing median HTTP round-trip time by at least
350ms, then a bidirectional partition causing curl transport failures from both
actors. It separately requires fresh worker API/RPC and actor RPC samples, unchanged worker
identities, and Bitcoin receipt advancement across a hold of at least 20s. Unavailable
canonical Stacks observations are recorded and resampled within the fixed stage
bound; they never count as successful reads. Identity mismatches fail immediately,
and persistent gaps time out. Successful samples do not prove uninterrupted control
availability.
The fault affects actor-pair IP traffic, not individual ports; HTTP round trips do
not prove independent one-way packet behavior or chain divergence.

Delay expires after 90s with native `AllRecovered`; partition is cancelled through
normal UID-bound deletion. Both require finalized deletion and restored HTTP
reachability/latency. A new recovery baseline is captured after cleanup, followed
by newly advancing Stacks canonical heights and successful traffic inclusion.
Fault cleanup alone never counts as protocol recovery. Native fault/status and
public probe/control snapshots remain in the evidence directory. Assertion failures
attempt bounded fault cleanup before the harness disposes the network.

## Local checks

These checks do not use a live cluster:

```bash
GOCACHE=/tmp/stacks-foundation-corrections-cache \
STACKS_PUBLIC_LIVE=0 go -C operators/network test -tags=live ./internal/publicintegration -count=1
make -C operators/network test-public-integration
make -C operators/network test-public-contract
```

`test-public-contract` is included in normal verification. It compiles the complete
live harness and runs local contracts with live access explicitly disabled.

The envtest regression separates participant creation from root identity publication.
During that gap, real status writes report `AllocationPending`, no worker is created,
and root failure is not latched. Publishing the ledger and admission then converges
to one candidate and one durable worker binding. No native process runs in envtest.

## Optional actor lifecycle qualification

`STACKS_PUBLIC_ACTORS=1` runs after optional faucet/actions and before root
pause/resume. Set `STACKS_PUBLIC_ACTORS_FIRST=1` to run the identical actor
assertions immediately after initialization/restart, isolating lifecycle qualification
from accumulated renewal, scheduling and reorganization experiments. Record the
selected ordering; success in the early case does not qualify historical catch-up
after the longer sequence. Both variants reuse `follower-01` with the resolved `traffic-recipient`
account, which receives transfers but supplies no existing peer identity. The test
rejects an already selected peer using that account. It adds a non-mining follower through the
reusable definition and public account ref, with the selected Stacks image and a
1 GiB persistent claim. No new genesis allocation or key read is involved.

The follower must publish current native synchronization and advance beyond the
previous network height. Suspension must acknowledge termination of its exact Pod
process while retaining its PVC. Resume must retain participant/config/PVC identity,
create a new Pod and show native progress. Removal must leave the single-use
participant UID in the root ledger while deleting its actual participant, Pod and
StatefulSet. The first claim is deliberately retained for the next check.

A second logical participant uses the same reusable definition/account. Its actual
participant UID, Pod UID, PVC name/UID and bound PV name must all differ while the
old claim still exists. The second participant and its non-retained claim are then
removed. Stage checks compare the frozen genesis object and every unrelated
actor/worker Pod UID and container ID, rejecting any unrelated roll or restart.
Public actor/PVC evidence is saved; normal namespace cleanup also removes the first
retained claim. By default this checks one selected image. Its waits use the normal
total and per-stage progress timeouts.

Set `STACKS_PUBLIC_UPGRADE_IMAGE` to a distinct preloaded image to roll the late
follower before suspension. The same participant and PVC/PV must survive, with a
new Pod, a different actual image ID, and fresh native progress. Other actors and
workers must keep their process identities. Record both source revisions/builds
alongside this evidence; changing a tag alone does not establish mixed versions.

### Reuse declarations for a second network

Set `STACKS_PUBLIC_REPEAT=1` to delete the first running root directly with foreground
propagation after its optional checks, retain the namespace and exact reusable
declarations, and create the
original network declaration again. The second incarnation must initialize and
produce fresh canonical progress with the same chain digest and new root, genesis,
participant, Pod, PVC and persistent-volume identities. Optional action/fault stages
run only in the first incarnation. This is semantic repeatability evidence, not a
claim of identical blocks or execution order. Allow enough total timeout for both
initializations; final cleanup disposes the second network and namespace normally.

## Optional maintained-operation qualification

Set `STACKS_PUBLIC_RENEWAL=1` to wait for all two (`minimal14`) or six (`full30`)
stackers to extend their original PoX-5 lock coverage. The test observes native
end-cycle/unlock-height growth with unchanged holder, manager, source, signer and
first-cycle identities, plus new settled worker activity. Exact worker Pod/process
bindings must survive, and native chain and traffic progress must resume afterward.
The evidence distinguishes canonical state convergence from exact transaction
inclusion; shared accounts do not establish exclusive authorship.

This read-only stage runs before optional faucet/actions and does not change lock
policies or cadence. Allow sufficient `STACKS_PUBLIC_TIMEOUT` for the declared
renewal threshold at the actual Bitcoin cadence. It consumes the normal test budget;
it never extends initialization gate deadlines. Baseline and renewed public facts
are retained in `events.jsonl` and the stage snapshots.

With actions enabled, `STACKS_PUBLIC_ALL_CADENCES=1` exercises two-block generation
with Fixed (2s), Immediate, Uniform (2..4s) and Explicit (3s) timing, then the
existing depth-one reorganization. Each action must retain exact current admission,
finite receipts, cleanup and release evidence. This proves native execution of each
mode; clock/distribution semantics remain separately covered by deterministic unit
and API-server tests.

## Optional baseline scheduling qualification

`STACKS_PUBLIC_SCHEDULES=1` selects the fixture's referenced `blocks` definition.
It exercises fixed timing on one target, uniform 5..7s timing on two targets weighted
1:2, then temporary fixed overrides with expiry and cancellation. While the expiry
override is active, it edits baseline timing to 7s and verifies that withdrawal uses
that latest baseline. The original definition is restored before final progress and
cleanup. This stage requires the documented `btc-01`/`btc-02` participants.

Each case requires four distinct native generation receipts under the current
producer policy and exact override identity, including a receipt from every selected
target. Receipt and current runtime identities must match. The bounded sample proves
both target paths execute; it does not claim a statistically measured distribution.
The expiry override lasts 180s; allow several additional minutes for the whole stage.
A failure proceeds to normal network cleanup rather than continuing later stages.

To reuse a fully qualified fixture for the terminal worker-loss check, set
`STACKS_PUBLIC_FINAL_EVICTION=1`. After normal checks and optional repeat, it runs
the same exact-Pod eviction assertions as the standalone eviction test, then disposes
the failed network normally. No later healthy-operation check runs on that failed
incarnation. This avoids a separate bootstrap solely to exercise terminal eviction.

## Optional shared definitions and keys

`STACKS_PUBLIC_SHARED_DEFINITIONS=1` adds two independent Bitcoin participants using
the already-selected `btc-07` definition and its `miner-wallet-01` wallet, which is
derived from the same Stacks miner account. Both actors must load that exact public
wallet and synchronize, while retaining distinct participant, Pod, PVC and volume
identities. Existing actor/worker processes and frozen genesis must remain unchanged.

The test then removes only the added participants, waits for actual runtime/storage
disposal and verifies that the reusable definition, wallet and account UIDs survive.
It records native progress with shared runtime present and after removal. This proves
reusable definitions and public key identities are not globally exclusive; it does
not promise nonce coordination among independent transaction writers sharing a key.
