# Bitcoin control runtime

The v1alpha2 runtime uses one network-owned `BitcoinExecution` per Core node and
one retained `BitcoinInitialization`. The aggregate persists their actual UIDs
before workers activate. Missing bound records fail closed.

A scoped Go Deployment reads only its own mounted control RPC credentials.
Wallet create/load/import/unload and block generation use the same persisted
Armed slot, initialization reservation, exact actor process identity, and fresh
admission checks. A process sends only an arm it successfully wrote itself.
Unknown delivery retains Armed; client deadlines and worker termination do not
prove Core quiescence. Successful responses remain in memory across API outages
and receive up to 25 seconds of shutdown drain. Detached wallet intent remains
persisted until unloading is acknowledged or native absence is observed.

The scheduler owns cadence and target selection. It initializes watch-only
public descriptor wallets, funds the complete captured miner cohort, checks
coinbase maturity and all captured Core views, and initially holds at the frozen
PrepareBitcoin ceiling (203). Its 120-second observation deadline survives
pause. Later advancement requires the aggregate’s exact frozen gate index and
ceiling, with all prior completions retained. After 203, captured Stacks actors
must be currently verified and ready. Once PoX-4 activates, each legacy
confirmation opportunity requires a fresh bound stacker worker with pending
authorized work. The worker repeats those checks before sending. Initial funding
is never replayed. Private Core wallet spending is not implemented.
Compatible replacement production participants inherit retained progress through
`status.production`; captured bootstrap provenance remains immutable. After every
frozen gate completes, Fixed/Uniform baseline timing uses one durable weighted
selection per opportunity. Unavailable or busy targets consume their opportunity
without redraw or catch-up. Per-execution receipt cursors preserve out-of-order
acknowledgements across target and producer removal. The producer receives only
the bounded `status.scheduling` projection; the initialization record remains
the sole selection authority. Rejected candidates retain the last complete
admission while its captured mandatory inputs remain available.

Pause acknowledgement means that the current worker stopped sending and has no
Armed request. Its original acknowledgement time is retained separately from a
five-second process heartbeat; the root rejects stale heartbeats. Native chain
and wallet observations continue during pause without executing mutation plans.
Terminal drain acknowledges local closure, including retained
uncertainty. Neither is process termination: participant `status.bitcoinControl`
separately retains exact Deployment, ReplicaSet, and Pod exit evidence. Pod
finalizers retain that evidence until its status publication succeeds.

Unit tests cover full first-gate progression, identity changes, unknown delivery,
lost CAS acknowledgements, API outage receipts, pause/deadline behavior, wallet
cleanup conflicts, production replacement, and workload drift. Integration tests
exercise API status ownership and process-evidence retention. Native Core and
Kubernetes deployment qualification are separate gates; these tests do not
qualify legacy-runtime safety claims for the replacement.
