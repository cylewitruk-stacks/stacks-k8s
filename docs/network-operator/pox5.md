# Direct PoX-4 and PoX-5 operation

`StacksNetwork.spec.operation` declares managed accounts, contract sets and
stacking participants. Separate capability controllers maintain those resources;
there is no PoX-5 network mode or required external bootstrap process.
The epoch schedule determines activation. Standard provisioning always includes
sBTC artifacts, even when the schedule defers Epoch 4.0.

## Ownership

| Role | Responsibility | Key location |
| --- | --- | --- |
| STX holder | Direct PoX-4 stacking, PoX-5 staking and renewal | Dedicated managed-account Secret |
| Manager administrator | Publish and register its holder's minimal manager | Separate managed-account Secret |
| Consensus signer | Sign blocks and authorize its PoX identity | Signer Secret and separate authorization Secret |
| sBTC deployer | Publish contracts and authorize initial registry rotation | Separate managed-account Secret |
| Bridge signers and aggregate key | Explicit disposable registry initialization | Private provisioning manifest; only public keys enter capability resources |
| Transfer producer | Independent steady-state transaction demand | Dedicated transfer-worker Secret |

A funded genesis account is not automatically managed. Each managed account has
one named consumer and an immutable ingress/key binding. Removed accounts and
capabilities retain their identities; removing and re-adding a name cannot reset
nonce ownership. Extra funded accounts remain available for later experiments.

The standard provisioner creates one consensus signer and separate holder and
administrator. Its direct manager accepts only its declared holder's stake,
holds no STX and implements no delegation or pool administration. Stacking
administration runs separately from the signer Pod, so signer lifecycle and
faults do not automatically control its administrator.

## Sources and activation

Use Stacks 4.0.1-compatible binaries and the sources at this pinned revision:

```bash
git clone https://github.com/stacks-network/sbtc.git /tmp/stacks-pox5-sbtc
git -C /tmp/stacks-pox5-sbtc checkout --detach d31b0780cf1ae91b6ef4ed7ee89400c796aea913
```

[Contract pins](../../operators/network/internal/protocolcontracts/sbtc-contracts.json)
record source hashes and language versions. Sources remain under their upstream
license; they are not vendored. Supply `--sbtc-contracts` to
[Stacks provisioning](stacks-production.md#provision-and-bootstrap). The Go helper
checks every file before rendering immutable ConfigMap artifacts. Programmatic
callers can supply explicit alternative artifacts, which require qualification.

`spec.genesis.pox5` fixes the token, registry and bond/pause principals identically
on every generated node. Contract sets separately declare exact sources,
dependencies and optional explicit initial registry membership. Sources are
observed and compared before deployment. Clarity 3 contracts become eligible at
Epoch 3.0; the PoX-5 manager uses Clarity 6 after 4.0.

The default provisioned schedule places Nakamoto at 223 and 4.0 at 244, in
separate reward cycles. Initial dependency holds allow PoX-4 confirmation blocks,
wait for contracts before 4.0, and wait for first PoX-5 enrollment. Completed
startup stages are durably acknowledged. They do not subsequently pause Bitcoin
because a signer or Stacks protocol becomes unhealthy. User pause settings are
never overwritten. See the [managed-operation design](../design/managed-network-operation.md).

## Maintenance and evidence

The holder initially stakes 100,000 STX in PoX-5 for 12 cycles. PoX-4 enrollment
also satisfies the observed network threshold. A six-cycle extension is offered
when at most six cycles remain and at least two blocks remain before prepare.
Changing the enrollment amount applies to a future enrollment, not the current
lock. The initial profile does not resize or prematurely unlock existing stakes.
Changing the horizon affects subsequent extension decisions.
At five-second Bitcoin cadence, a 12-cycle lock lasts roughly 20 minutes and
renewal becomes eligible with roughly 10 minutes remaining. Delayed execution
can still cross a protocol boundary; the margin is not an execution guarantee.

A managed account records its exact TxID and nonce before one submission attempt.
After restart, workers observe the same transaction without signing or sending
it again. Native indexed receipts cover Nakamoto transactions. Initial legacy
PoX-4 receipts use the node's execution callback, checked against signed bytes,
the admitted source Pod and canonical legacy headers. Lost callback evidence
can leave the account reserved; a balance change is not a replacement receipt.
The legacy receiver is a trusted, same-namespace actor-service profile, not a
receipt attestation mechanism for a compromised ingress.

The consumer durably retains the receipt before acknowledging its account.
Failed execution consumes the nonce but blocks the capability for inspection.
Matched native rejection envelopes retain only `FeeTooLow`, `BadNonce`, or
`Other`; no ambiguous outcome authorizes a resend. New operations pause while
existing receipts remain observable. When a recorded successful lock expires,
reenrollment can be selected from a current reward window and matching nonce.
Unknown pre-existing locks are not adopted.

Inspect native chain progress across reward cycles as well as capability status.
[Qualification](managed-operation-qualification.md) distinguishes automated tests,
live evidence and retained failed fixtures.

## Future bridge and pool support

Explicit registry initialization is separate from contract deployment and signer
participation. Real sBTC daemons may use the same principals, but require a
supported registry ownership/key handoff. Merely starting daemon Pods does not
establish that handoff; the explicit aggregate key is not a DKG result.
Delegated pools can use a separate administration capability while preserving
holder and consensus-signer boundaries. This delivery does not implement bridge
daemons or PoX-1 through PoX-3 administration.
