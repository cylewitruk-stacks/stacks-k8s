# Faucet requests

The controller binds a user-owned `StacksFaucetRequest` to one network, faucet
participant, worker Pod, source account and native ingress. Destination resolution
is retained at admission. The immutable deadline is creation time plus timeout,
including subsecond precision. This release uses an explicit 3000 micro-STX fee.

Admission and lifecycle projection use one status field manager; the worker owns
only execution evidence. Both writes carry UID and resource-version preconditions.
An uncertain admission write may have granted authority and retains a local
capacity reservation until a confirmed decision is observed. At expiry, an admitted
request without an affirmative outcome is `Inconclusive`; a fresh worker refusal
before sending can establish `Expired`. Native exact transaction inclusion alone
establishes completion. A definite submission rejection retains nonce coordination.

The worker watches request metadata for notification, then reads the exact request
and current public dependencies before sending. It keeps one pending transaction,
bounded active request state, and original outcomes through lost acknowledgements.
Deleted unresolved requests keep their local coordination and a bounded participant
summary; no request is recreated. Persisted terminal requests release capacity,
so retained history does not impose a lifetime request limit. Controller admission
uses a paged active-request scan only when granting a new request; the worker caps
its own active and unpersisted state at 1000 entries.

Faucet requests do not use baseline API-outage authorization. The surviving process
can still observe its existing submitted transaction while request or participant
status writes are unavailable. Observation does not authorize discovery or sending.

Unit tests exercise admission identities, deadlines, capacity, deletion and nonce
uncertainty. Envtest verifies immutable schemas, separate SSA ownership, lost
admission acknowledgements and rejection of stale writes after request replacement.
Live native transfer qualification is separate from these controller checks.
