# Resolution, execution and destruction

Status: proposed public contract, not runtime qualification. Read
[composition](composition.md) for definitions, instances and reference meanings.

## Namespace scope and singleton

StacksNetwork has the canonical name network, enforced by static admission. Its
cleanup finalizer prevents overlapping roots in a namespace. Reusable definitions,
accounts, wallets and schedules have no networkRef and survive root deletion.
There is no execution/run registry, account lease or scenario controller.

Account and wallet resolvers may run without a network; generated credentials are
immutable and never silently regenerated after loss. Shared keys are permitted.
One worker serializes its own same-address spending; independent workers/external
clients coordinate themselves or encounter visible conflicts. Accepted submission
advances the local nonce; uncertainty holds the pending stream without a replay.

## Resolve, validate and freeze

1. Apply definitions and a network in any order. Allocate generated participants for
   its explicitly named entries; resolve their complete effective configurations.
   Identity/config resolver Jobs may run, but no actor or mutation worker activates.
   Definitions validate direct inputs independently; instance wiring checks use the
   selected participant graph. An unrelated parked definition does not block it.
2. Profile, schedule, genesis allocations/settings and root defaults are immutable
   from creation. operation and participants remain mutable under the rules below.
   Resolve all initial participant dependencies, identity keys, public contract
   principals and optional faucet funding. Report current inputDigest and errors.
3. With operation Running and a complete valid graph, recheck root UID/generation,
   deletion/control, participant UIDs and source/dependency identities. Optional
   expectedInputDigest must match this candidate. Each initial participant's complete
   public policy must already be durably recorded in status.admission; incomplete
   runtime bindings do not permit activation. Create deterministic network-owned
   StacksGenesis, capturing chain inputs, initial participant UIDs/config digests and
   the public values required by each bootstrap gate together. This single creation
   is the freeze point.
4. After a lost create acknowledgement, read and verify that same artifact; never
   create an alternative. Publish its name/UID/digests. Activation requires it and
   currently valid instance admission. A concurrent edit can occur after the final
   read: the captured valid artifact remains authoritative. A newly observed pause,
   stop or deletion prevents subsequent activation, even if genesis already exists.
5. Evaluate the fixed profile initialization gates against spec.bootstrap in that
   artifact, not current desired policies or an unavailable historical definition.
   Late additions use the same genesis; they cannot replace missing initial cohort
   members or shrink unfinished gates.
   Completed stages never rewind. Deliberate removal can make unfinished bootstrap
   impossible; report that instead of restoring membership or changing genesis.

Once status.genesisRef is published, inspection must treat inputs/cohort as frozen,
even if phase still reads Uninitialized. Actual artifact creation precedes publication;
a missing status ref does not authorize the controller to rewrite an existing artifact.

inputDigest covers semantic candidate inputs, keys and images, excluding lifecycle
controls, expectedInputDigest, UIDs and timestamps. genesisDigest covers only canonical
public chain configuration; provenance is excluded. The artifact keeps captured
inputDigest while root status may show newer candidate inputs. Neither digest promises
deterministic runtime outcomes. expectedInputDigest becomes fixed at freeze and gates
initial capture only. Oversize genesis (900 KiB total) is rejected before activation.

Equal address/amount allocation aliases count once; conflicting amounts are invalid.
Public contract principals resolve without deployed contracts, avoiding a startup
cycle. Nodes consume one verified genesis through rendered config; missing, replaced
or corrupt published genesis fails the network, never regenerates from current intent.

## Resolved bindings and live updates

Each instance admission pins its participant UID, definition UID when referenced,
transitive participant/account/wallet/Secret UIDs and public fingerprints. Record
complete policy inputs and digest; never private keys. Domain controllers validate
complete candidate policies before admitting them. Workers execute admitted state,
not an arbitrary combination of root/definition informer snapshots.

A changed protected binding reports RequiresReplacement; preserve the previous whole
policy only while its captured dependencies still exist and remain eligible. Root
entry omission always withdraws it, regardless of stale admission. Image/config
changes permitted by that workload admit a new complete policy and roll that actor;
this does not authorize changing immutable genesis or silently rebinding a Secret.
An explicitly selected, validated new config.secretRef may replace the configuration
Secret UID during a supported actor roll while preserving protected credential
identities; a same-name Secret replacement is not automatically admitted.
Operational controls remain independent of a deferred substantive update.

Source deletion or identity loss withdraws dependent authorization and reports
DefinitionUnavailable/IdentityUnavailable. It does not silently select another source
or erase existing instance evidence. Actor/worker destruction follows explicit entry
removal or network deletion. A same-name replacement source/dependency never inherits
admission. Unrelated instances continue when their own prerequisites hold.
Explicit and discovered peer seeds are startup hints, not ongoing mandatory admission
dependencies: loss of an admitted seed peer does not withdraw another actor's admission.

Changes to reusable definitions fan out as candidates to their consumers; only affected
instances reconcile. Whole-policy admission is per instance, not a cross-resource
transaction. In-flight work keeps its original admitted policy. Unsupported fields
are reported, never partially applied. New peer membership affects newcomers' seed
resolution; it does not automatically roll every existing peer.

### Bootstrap policy updates

StacksGenesis.spec.bootstrap retains the exact public requirements of every initial
gate, including amounts and cycle coverage. Completion and reachedAt timestamps are
durable root status; a controller restart reconstructs the remaining work from these
two resources. No definition history or process-local policy snapshot is required.

Until a gate completes, a candidate policy that conflicts with any of its captured
requirements sets the participant condition PolicyDeferred=True with reason
BootstrapPending. Keep the previous complete admitted policy; apply none of the
candidate's substantive fields. The worker continues
to pursue the captured requirements. Re-evaluate the latest candidate after the last
gate it affects completes; do not queue or replay intermediate edits. Compatible
image/resource changes remain eligible under the workload's update rules.
Set PolicyDeferred=False when the latest candidate is admitted or no longer deferred
by bootstrap.

For example, a post-freeze stake-amount edit cannot change the amount required by
either unfinished PoX enrollment gate. Its new amount becomes eligible only when all
affected gates complete. Pause, stop, deletion and membership removal still take
effect independently: they may delay or make bootstrap impossible, never silently
change its requirements. Missing pinned dependencies withdraw authorization rather
than allowing the retained policy to run against replacements.
If a freeze/admission race leaves no complete retained policy satisfying the captured
requirements, latch root Failed=True with reason BootstrapPolicyUnavailable and
withdraw new mutation authorization. Recovery requires network recreation; edits
cannot clear the failure, even before activation. Existing bounded cleanup follows
the failure contract. Do not reconstruct a mixed policy or move the frozen gate.

## Membership removal and re-add

**Removing an entry is destructive.** Withdraw new admission, drain/settle supported
in-flight work, terminate its workload and delete its generated participant/config
resources. Storage follows declared retention. No stopped participant archive,
retirement lifecycle, DestroyNetworkParticipant CRD or evidence-export gate is added.
Reusable source definitions and their keys survive; inline-generated identity keys
belong to the destroyed instance. On-chain transfers/contracts/stacking are not undone.

A participant name is single-use once its generated instance UID is recorded. Root
status.identities keeps at most 1000 lifetime instance allocations, including destroyed
ones: participant name/UID and any bound worker identity/disposal acknowledgement.
This minimal safety ledger contains no old specs, telemetry or nonce history. It
prevents lost acknowledgements or remove/re-add races from resurrecting an instance.
A new participant requires a new name, even if it reuses the same definition. Allocate
no new instance after the bound; a fresh network supplies fresh capacity.

Removal is recorded with a conflict-checked root write before intentional deletion.
Quick re-add does not cancel a recorded removal. New same-name input reports
NameAlreadyUsed. References pinned to the removed UID do not automatically follow a
replacement; new/compatible peer policies must select its new name explicitly.
A direct deletion of a generated instance while still desired is InstanceLost and
withdraws its authorization; the operator does not recreate it under the old entry.

Before intentionally destroying a Bound Stacks worker, read its exact Pod directly.
Confirmed prior absence, terminal exit or UID mismatch latches Failed; read errors
are Unknown, not clean shutdown. The worker first stops new sends, reports outcomes
and acknowledges shutdown; then confirm process termination. Unknown/unsettled sends
cannot authorize another worker: if orderly disposal cannot establish settled state,
latch Failed and allow only stop/disposal. Finalization may finish after termination
with an honest unconfirmed outcome; it must not wait forever for chain inclusion.
An observation/delete race remains possible; this is not server-side crash fencing.

New stacker/faucet/traffic/contract participants can start only in an eligible network,
with current chain reads and their own new instance identity. They do not import old
request/nonce state or retry old requests. Contract/stacking initialization observes
existing source/registry/manager state before any write, and reports conflicts rather
than repeating deployment blindly. Genesis identities cannot be replaced: a new
contract-set must agree with frozen principals/source hashes; new faucet allocations
cannot alter genesis (its requested balance must be zero or match an existing frozen
allocation). Later funding remains an explicit operation.

The v1 profile admits at most one instance of each maintenance singleton kind at a
time. A replacement waits for its predecessor's confirmed disposal and settled work,
even if the old entry has already left spec. Bitcoin production adopts the network's
retained initialization and target records with unchanged payout/initialization
identity; never repeats funding. Deselecting production stops baseline scheduling,
while already-admitted actions and cleanup retain their obligations. Removing a
Bitcoin node similarly retains records and its control path until bounded cleanup
finishes or is recorded uncertain. Ambiguous targets remain closed; a new name is
not permission to bypass the Bitcoin execution contract.

## Startup protocol prerequisites

[Protocol timing](protocol-timing.md) owns the source-derived gates: PoX-4 inclusion,
prepared Nakamoto cohort, real sBTC prerequisites, PoX-5 manager/enrollment and first
waterfall signer set. Stage status includes targetCycle, bitcoinCeiling and unmet
prerequisites; height alone is not readiness. Production must not wait for aggregate
Operational, and actors must not wait for peers/signers Ready before starting.

Legacy pending transactions receive bounded confirmation opportunities within their
window; Nakamoto can progress with Bitcoin held. Every baseline/action generation
honors the current initialization ceiling. Unknown outcomes close advancement.
Completed stages never rewind after later faults; unsupported reorg changes to
managed nonce/history may fail the experiment. Bitcoin cleanup never restores the
former best chain.

## Worker identity and failure

Stacks mutation workers are standalone Pods with restartPolicy Never. One deterministic
candidate name per participant UID avoids duplicate candidates after uncertain creates.
A candidate starts inactive. The domain controller reports it; the network controller
persists the exact Pod UID under status.identities before activation. Lost writes are
read back. Unexpected extra candidates must be proven inactive and removed, otherwise
report Conflict. Before binding, an inactive candidate create can be retried.

Once Bound, unexpected missing/terminal/different-UID Pod means Failed, never
create-on-missing. Recorded intentional shutdown follows the removal/stop contract;
it does not turn every acknowledged clean exit into an unexpected worker failure.
Control/API read errors report Unknown, not proof of loss. Operator restart and
watch reconnect preserve live workers. Root Failed stays latched; changing operation,
source definitions or participant names cannot admit new workers. Root state loss or
corruption fails closed. Bitcoin control-worker restart retains its distinct durable
execution contract; ordinary actor StatefulSet replacement is not a Stacks-worker crash.

## Network operation and observed lifecycle

spec.operation is declarative desired state, not a command queue. Running is the
default. Paused and Running may alternate; Stopped is terminal and CEL rejects any
transition out of it. Deletion can begin from every state. Failed is a separate
latched experiment condition: no edit can authorize a new worker after failure.

| Desired operation | Before protocol activation | After activation |
| --- | --- | --- |
| Running | Resolve, freeze genesis and initialize automatically. | Continue initialized operation, or resume unfinished initialization and existing paused processes. |
| Paused | Resolve/validate for inspection; do not freeze/start. If genesis already won a race, retain it without activation. | Cooperatively hold new baseline/faucet submissions; observe pending work and retain processes. Already-admitted bounded actions and consensus may continue. |
| Stopped | Create no further runtime; keep existing resolved artifacts for inspection. | Stop new activity, settle/disposition pending work, terminate worker/actor processes and confirm termination. Retain instance declarations/config and PVCs until removal/deletion. No resume. |

Stopped does not preserve container logs, memory or ephemeral files. Actor stop scales
its StatefulSet to zero; support Deployments drain then scale to zero; standalone
workers acknowledge shutdown then exit. Confirm exact process identities. Failed
workers cannot provide a clean acknowledgement; preserve Unknown outcomes instead
of inventing completion. Root stop permits bounded Bitcoin cleanup before shutdown,
subject to its existing disposition limits. Root deletion performs the same shutdown
obligations before removing objects. Source/participant edits while Stopped do not
activate anything; remove entries or delete the root to destroy retained resources.

| Observed phase | Meaning / exit |
| --- | --- |
| Uninitialized | No protocol activation yet. With operation Paused and valid inputs, Resolved=True but Initialized=False; genesis may exist after a freeze/pause race. Running requests initialization. |
| Resolving | Initial graph/input resolution is in progress or waits for missing dependencies; no protocol activation. |
| ResolutionError | Initial composition is invalid; conditions name the cause. Corrected inputs return to Resolving. Not a terminal experiment failure. |
| Initializing | Genesis is fixed and initial protocol convergence is in progress. Completion sets Initialized=True and enters Running. |
| Running | Initialization completed and running operation is requested. Operational may still be False/Unknown after faults. |
| Pausing / Paused | Runtime was activated; waiting for / holding acknowledged cooperative pause. Initialized may be False if startup was interrupted. |
| Stopping / Stopped | Terminal shutdown requested; waiting for / confirming process termination. Initialized remains a historical observation, not permission to resume. |
| Failed | Latched worker/identity/protocol failure. New mutation authorization is withdrawn; existing evidence observations and bounded cleanup may continue. Stop or delete for shutdown/disposal. |
| Destroying | deletionTimestamp is present; cleanup and garbage collection are in progress. |

Derive one phase in this order: deletion; requested stop; latched failure; preactivation
resolution/error/inspection; active pause acknowledgement; initialization; running.
Before the first reconcile, absent phase means unobserved, not Running. Stopped does
not clear Failed=True. Late resolution errors stay on the affected instance and root
conditions; they do not send an already initialized network back through bootstrap.

Conditions Resolved, Initialized, Running, Operational and Failed carry reasons,
observedGeneration and transition times. Initialized records completion of the fixed
gates and stays true through pause/stop/later faults. Running=True means the network
is initialized and currently in running operation; it does not mean every actor is
healthy or consensus advances. Running=False covers preactivation, initialization,
pause, stop and failure; Unknown covers insufficient current observation.
[Operational](#operational-condition) aggregates the profile's required observations.
Headlamp may display phase and these
conditions; UI convenience does not change the lifecycle contract.

Network pause is cooperative, not global chain quiescence or a forensic freeze.
Management control acknowledgement confirms no new baseline/faucet sends from the
reached workers. In-flight operations, independent clients and consensus can continue.
Admitted-but-unsent faucet requests keep their original deadlines. The bootstrap
120s observation window also continues during pause; it can expire during inspection
(see [protocol timing](protocol-timing.md)). During partial
startup, pause preserves already activated processes and holds subsequent activation.

### Operational condition

The network controller evaluates this predicate for regtest-pox4-pox5-v1. It uses
current selected, completely admitted participants and their exact runtime identities,
not the frozen initial cohort or every declaration in the namespace.

| Required observation | Scope |
| --- | --- |
| Bitcoin baseline is available and progressing | Current BitcoinBlockProduction is unpaused, has at least one eligible target, and reports acknowledged generation within its progress window. Individual unavailable targets retain their own conditions. |
| Stacks baseline is available and progressing | At least one verified enabled miner is Ready. Current unpaused StacksTransactionProduction reports exact successful canonical transfer inclusion within its progress window; fresh ingress observations agree with the frozen chain/contract bindings. |
| Required contracts remain valid | Current admitted StacksContractSet reports exact source/registry postconditions. A paused initializer can satisfy this with fresh observations of already completed initialization. |

Setting control.paused on required Bitcoin or transaction production makes
Operational=False, even while root operation remains Running; a paused baseline
is an unmet predicate, not excluded or Unknown.

Faucet, actions, faults, observability, extra followers and Unverified peers are not
aggregate health gates. Stacker/signer readiness and desired enrollment remain
per-instance observations; a late unfunded stacker does not itself make the root
non-Operational. Their actual effects on consensus appear in the canonical-progress
check. A suspended extra actor is not a failure of this predicate; losing the only
eligible miner or all Bitcoin targets is. The separate latched worker-crash contract
still applies to every bound management worker.

Use this precedence: known failure, deletion, non-Running operation or incomplete
initialization gives False; a known unmet required predicate gives False; otherwise
missing/stale required observations give Unknown; all required predicates met gives
True. A missing required singleton is False, while an unavailable membership read is
Unknown. A fresh report showing progress overdue is False; stale reporting alone
does not prove a stall. Reasons identify the unmet or unavailable predicate.

The release [observation policy](operations.md#observation-policy) defines freshness
and cadence-aware progress windows. Late additions/removals re-evaluate the current
predicate without rewinding Initialized. Operational is a bounded recent-progress
assessment, not a throughput guarantee or permission to dispatch: each capability
still checks its own prerequisites and never waits for aggregate Operational.

## Evidence and disposal

Destruction is not archival. Collect required volatile evidence before removing an
entry or deleting a network. Pausing preserves only the processes that support it;
it is not a global forensic freeze or chain-quiescence guarantee.

Optional observability may continuously collect configured telemetry through Loki,
OpenTelemetry or equivalent backends. Collection does not guarantee all evidence:
record coverage/gaps and backend retention. The network never waits for an export
acknowledgement and works without observability CRDs or collectors.

| Artifact | Collection / later availability |
| --- | --- |
| Logs, metrics, events and public runtime/config identity | Collect while available; external backend retention can outlive participant/network deletion. Uncollected or rotated data may be lost. |
| PVC contents | Inspect/export before destruction, or later only if retainOnDelete preserved the volume. Retention is not a snapshot or application-consistency guarantee. |
| Configuration | Public effective configuration/digests may be collected; runtime ConfigMaps/Secrets are deleted with their owner. Export required private files explicitly with appropriate access; passive observability never reads signing Secrets. |
| Ephemeral files / process memory | No retention promise after stop, Pod replacement or deletion; explicit capture must precede the destructive operation. |

For ordered disposal, use background cascading deletion (`--cascade=background`).
While the root finalizer remains, its children can serve cleanup. Root deletion
withdraws new activity, then finalizes dependent workers, Bitcoin
cleanup and actors before deleting shared genesis/config and releasing its finalizer.
Existing Bitcoin cleanup uses bounded disposition rules. An unreachable node cannot
prove old processes stopped; report Destroying/TerminationUnknown and keep the root
finalizer. Pod force deletion or administrative namespace removal is outside this
ordered contract. Foreground cascading can delete descendants before the root
finalizer completes; orphan propagation can leave unmanaged runtime. Both are outside
the ordered-disposal guarantee. Report interrupted/uncertain cleanup honestly and
verify surviving process identities before claiming termination; never infer settled
work from GC. Do not retain artifacts automatically to make these modes archival.
A failed network remains inspectable until explicit disposal. See
[Kubernetes cascading deletion](https://kubernetes.io/docs/concepts/architecture/garbage-collection/#foreground-cascading-deletion).

Network deletion preserves reusable CRs/keys. Participant-owned inline keys and
runtime artifacts are deleted. PVC retention is explicit and independent of reuse:
retained volumes have provenance labels and no cascading ownerRef; the replacement
network never mounts them automatically. Namespace deletion also removes reusable
resources and namespaced retained PVCs. Use root disposal first when cleanup facts matter.

### Administrative abandonment

TerminationUnknown has no timeout that turns it into proof of termination. If access
recovers and actual termination is established, normal finalization can finish, with
uncertain chain outcomes still reported honestly. Deleting a Node or force-deleting a
Pod object alone proves neither that its former processes stopped nor that an RPC
settled. [Forced Pod deletion](https://kubernetes.io/docs/concepts/workloads/pods/pod-lifecycle/#pod-termination)
can remove the API object before process termination.

For a disposable environment that cannot complete normal cleanup:

1. Export available root/participant identities, status and relevant evidence; record
   administrative abandonment and any unconfirmed outcomes outside objects being deleted.
2. Stop/remove the underlying failed kind node container, or otherwise establish that
   its former processes cannot continue. Stop remaining network workloads and prevent
   their controllers from restarting them (for example, scale them to zero). Confirm
   termination before attempting a replacement environment; API object absence is insufficient.
3. If cleanup still cannot progress, an administrator may remove the specific stacks-k8s
   cleanup finalizers from the old root and stuck owned resources. Inspect current
   UIDs/finalizer lists and remove only those keys; preserve unrelated finalizers. This
   is an explicit bypass of controller verification, not a successful cleanup receipt.
4. Clean up remaining scoped resources and then the namespace if desired. Namespace
   deletion alone is not an escape: remaining resource finalizers can keep it Terminating.

No new abandonment CRD, automatic finalizer timeout or evidence-export gate is added.
An abandoned outcome does not claim settled transactions, recovered protocol state or
preserved evidence. Do not restart the experiment while former processes may still run.

## Request binding and fresh repetitions

One-shot faucet requests, overrides and Bitcoin actions carry immutable networkUID
and participant name. Before admission, resolve the allocated participant UID and
record it; old requests never follow replacement instances. Single-use names prevent
a never-admitted old request from selecting a later participant in the same network.
A new root UID prevents rebinding across networks. Native faults use network and
participant UID labels. Exports instead bind retained telemetry UID/networkUID and
may complete after root deletion.

Reapply desired definitions and root after disposal for a fresh chain. Definitions
and reusable keys survive; new UID means fresh runtime names, credentials, records
and volumes. Restore source specs externally if the experiment changed them. Parallel
networks use separate namespaces with explicit credential provisioning where needed.
No edit replay, execution cloning, account locks or deterministic outcome guarantee.
