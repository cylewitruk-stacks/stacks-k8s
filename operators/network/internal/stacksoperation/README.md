# Stacks operation roles

Roles run in one bound, non-restarting worker Pod. The surviving process owns its
local account nonce and one pending submission. Shared accounts remain legal;
unresolved nonce conflicts hold the stream. Submission acknowledgement, matched rejection,
unknown delivery, exact canonical inclusion, and public-state convergence remain
separate facts. No observation failure authorizes resubmission.

A matching native FeeTooLow, BadNonce, ConflictingNonceInMempool or NotEnoughFunds
refusal clears the pending attempt while retaining its TxID and rejection counter.
The local nonce stays unchanged. A new baseline attempt requires fresh authorization
and at least five seconds of backoff; traffic also respects its cadence. Other
rejections remain uncertain and cannot release pending coordination.

The contract-set role loads all five pinned real sBTC sources from an external
directory, verifies their release hashes, and deploys them in dependency order
after the frozen epoch-3 activation. GPL-3.0 upstream sources are not embedded in
this MIT package. Initial registry rotation uses only the deployer's private key;
signer and aggregate keys are public inputs. The native wrapper requires at least
two signer entries and a strict majority threshold. Exact source bytes, ordered
signer keys, aggregate key, threshold and derived signer principal must agree at
one canonical index block ID. An unavailable tip is never treated as a missing
contract. Lost acknowledgements can settle through distinctly recorded source or
registry postconditions with the exact next nonce, without claiming TxID inclusion.

The PoX-4 role consumes mounted holder and consensus keys, admitted public account
and actor identities, and the frozen genesis cohort. It signs direct `stack-stx`
and `stack-extend` calls in Go with an explicit 3000 micro-STX fee. Initial
coverage must include the frozen cycle (12 in the supplied profile); enrollment
stops at the frozen ceiling (234). It observes exact direct holder, compressed
signer key, holder-derived P2PKH payout, amount and reward-cycle entry. Epoch-3
renewal uses the same nonce stream. The composite stacker preserves that stream
through the PoX-5 transition; unknown PoX-4 sends block all subsequent work. Exact
successful old transaction inclusion can close the old operation after the active
boot contract changes, without claiming an unavailable PoX-4 postcondition.

Legacy enrollment cannot rely on the Nakamoto-only `/v3/transaction` lookup.
`StateObserved` therefore requires the exact selected first/end cycles and native
reward-set state, plus canonical account nonce equal to the pending nonce + 1,
with account, PoX and contract reads pinned to one canonical index block ID.
The full header, consensus and burn-fork identities must remain unchanged across
the read bracket; an unchanged header hash alone is insufficient. It advances only the local
pending operation. It **does not prove that our TxID executed**: another writer
sharing the key could establish the same state. `PostconditionObserved` and
`LastPostcondition` retain this limit; `Included` and `LastInclusion` change only
for exact transaction observations. Higher/lower nonce or mismatched state keeps
the original transaction pending.

Pause permits observation and withdraws new offers. Drain performs only reads
within the worker framework's bound. API publication retries retain the original
facts. Unit tests cover ambiguous and rejected sends, competing nonces, exact
cycle/key/amount/payout checks, moving tips, pause, enrollment ceilings, native
Nakamoto confirmation, and renewal. Live enrollment throughput, canonical
prepared signer sets and transition timing require separate Core qualification.

PoX-5 deploys the holder-bound `direct-signer` source under the mounted
administrator only after native epoch-4 activation, using Clarity 6. It observes
exact source, registered consensus key and active grant before direct `stake` or
`stake-update`. The holder and administrator have distinct process-local nonce
streams; identical account addresses share the surviving holder stream. Separate
administrator submission facts appear in `administratorTransactions`.

Enrollment evidence requires exact account lock and unlock height, holder cycle
membership, manager signer-set membership and delegated amount for the frozen
cycle (15 in the supplied profile), all at one canonical index block ID. Manager
and stake operations can settle through the same distinctly attributed
`StateObserved` rule. Conflicting source or signer identity prevents mutation.
Renewal respects the contract's native edit window; an amount change waits for
unlock and then enrolls the current amount. No stake increase or delegated-pool
administration is performed. Tests cover the complete manager/register/stake
sequence, account sharing, lost responses, native mismatches, pause and renewal.
The current public resolver accepts the captured bootstrap cohort; late stacker
admission requires a separately qualified maintenance binding path.

When the worker framework explicitly authorizes an already-applied snapshot during
a transient Kubernetes API outage, roles reuse only their complete typed inputs
for that same policy digest. Mutable amounts are copied. Cold starts and different
or incompletely resolved policies cannot use this path; definite dependency loss
invalidates the retained typed input. Native state checks and the original pending
transaction remain authoritative throughout fallback.

The faucet role consumes immutable `StacksFaucetRequest` admissions through scoped
collection notifications. Each transfer requires fresh root, source, target,
genesis and exact request reads, with an absolute creation-derived deadline and
an explicit 3000 micro-STX fee. It never enables cached baseline authorization.
Duplicate notifications and lost writes retain the same local submission. Exact
native inclusion or one of the four validation refusals settles the attempt;
a refused request terminates Rejected and is never retried. Deleted unresolved requests retain nonce
coordination and bounded public outcome summaries. Read-only pending observation
continues through Kubernetes outages without discovering or sending new work.
