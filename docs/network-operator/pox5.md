# PoX-4 and PoX-5 operation

The supported regtest profile enters epoch 4.0 from its epoch schedule. Its frozen
initial cohort enrolls direct stackers for PoX-4, deploys the pinned sBTC contracts
and registry initialization, then deploys direct PoX-5 managers and enrolls/renews
those same declared holders. Native inclusion, source and contract-state checks
control each release gate.

The consensus signer, stacker administration and sBTC initialization are separate
roles. A `stacks-signer` Pod signs consensus messages; it does not administrate its
holder's stacking transactions. Current sBTC initialization supplies explicit test
signer material without claiming a running sBTC signer network. That initialization
boundary can be replaced by a separately implemented real signer capability.

The [protocol timing contract](../design/public-api/protocol-timing.md) defines
which cycle, contract and signer-set facts release Bitcoin production. The
[runtime guide](public-api-foundation.md) describes observed conditions and worker
failure boundaries. Mainnet activation dates are not replayed by a regtest schedule.
