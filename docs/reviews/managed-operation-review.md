# Managed operation review ledger

Status: Ready for independent review; implementation and qualification complete.
Base HEAD: `c85ccd6fbd8ed3efba3f1b4456d4850c68dfe020`.
The user's staged PoX-5 delivery is preserved. Review the complete working tree,
including the unstaged refactor and new files. No commit is requested.

## Contract

`StacksNetwork` owns supported perpetual desired operation through separately
reconciled capabilities. Its aggregate compiles resources; external agents own
experiments. Genesis funding does not enroll accounts. Epoch schedules select
protocol activation; standard provisioning always includes sBTC artifacts.
Go owns runtime behavior; JavaScript remains offline SDK encoding/signing.

## Delivery

| Area | Change | Evidence |
| --- | --- | --- |
| API and compilation | Managed account, contract-set and stacking-participant resources, shared static admission, immutable authority/artifacts and retained child UIDs | Generated CRDs and real API-server lifecycle tests |
| Account authority | CAS before one submission, monotonic ordinal, exact TxID/nonce, consumer receipt acknowledgement before reuse | Unit/race conflict, restart and accounting-loss tests |
| Contracts | Pinned external sources in immutable ConfigMaps, deterministic dependency order, source comparison and explicit bridge initialization | Native RPC fixture tests; real source deployment in live iteration |
| Stacking | Separate holder/admin/consensus roles, direct PoX-4/PoX-5 enrollment and renewal, transition guard | Timing regressions and live transition experiments |
| Bitcoin initialization | Watch-only wallet preparation and initial single-block dispatches on the existing executor; latched startup dependencies | Wallet retry and startup-stage regressions; live startup |
| Transfer activation | Explicit minimum burn height, applied only before new authorization | Admission and receipt-continuation regression |
| Legacy receipts | Bounded callback listener; admitted source Pod, signed bytes and canonical legacy ancestry; CAS accounting before HTTP acknowledgement | Callback regressions and live PoX-4 receipt |
| Packaging | Separate managed worker, Service and exact RBAC; shared SDK image and Go rendering | Chart policy checks and image builds |

## Review focus

- No competing Bitcoin generator or external bootstrap authority remains in a
  standard managed declaration. Old external commands reject managed networks.
- No unknown transaction is silently retried, replaced, or attributed by balance
  change. Execution failure retains evidence and blocks the capability.
- Startup acknowledgements do not turn subsequent protocol degradation into an
  implicit Bitcoin pause. User pause settings remain desired policy.
- Secret references and source digests are admission inputs; configuration and
  signing authorities remain separated from actor Pods.
- Legacy callbacks are a trusted ingress-service profile. They are not a new
  attestation mechanism for compromised node services; delivery gaps fail closed.
- Worker placement is separate from capability semantics. This delivery uses
  namespace-scoped worker Deployments, not stacking sidecars in signer Pods.

## Qualification and limits

See [live qualification](../network-operator/managed-operation-qualification.md)
for the preserved failed iterations and final evidence. Prior external-bootstrap
qualification was not used as proof of these controllers. The successful run
reached Bitcoin 405 / Stacks 287, traversed PoX-5 reward cycles 13–20, and renewed
the holder lock from 500 to 620. The pause/restart check preserved account
identity while Bitcoin continued. The successful namespace was removed in order.

## Final checks

| Check | Result | Scope |
| --- | --- | --- |
| `make verify` | Pass | Full repository verification with a temporary populated index; user's index unchanged |
| Focused `-race` tests | Pass | Account authority, managed contracts/stacking, callbacks, production and transfers |
| SDK tests | Pass | Offline JS tests and Go SDK integration; no new JS package |
| Envtest | Pass | Shared CEL, immutable identities, compilation, remove/re-add, independent topology and finalized deletion |
| Generated drift, RBAC, workload/Helm | Pass | Included in full verification |
| `make vuln`, `make docker-check` | Pass | No dependency upgrades; SDK image contents changed |
| Live startup, renewal, pause/restart, teardown | Pass | Pinned real sources on selected three-node kind cluster; details in qualification record |

The live images contain the final runtime. Subsequent changes were regression
tests, documentation and a generated field-description clarification. Enrollment
amount edits apply to future enrollment; existing stakes are not resized.
Known unsuccessful executions remain recorded and block the affected capability;
this delivery does not implement a general transaction repair policy.

Two failed development fixtures are retained paused/suspended for review. The
successful fixture was removed, and no new cluster was created. No hosted CI,
bridge daemons, delegated pools, mixed-image matrix or chaos requalification is
claimed. No commit was created; the original staged changes remain staged and
this refactor remains unstaged.

Use the [Fable prompt](managed-operation-fable-prompt.md) for the independent pass.
