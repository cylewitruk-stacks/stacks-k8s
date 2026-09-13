# Bitcoin production

Select a `BitcoinBlockProduction` participant in `StacksNetwork.spec.participants`.
Its reusable definition references a `BitcoinBlockSchedule`, selected Bitcoin-node
participants and a payout wallet. The [example](../design/public-api/examples/30-actors.yaml)
shows the complete wiring and initial miner-wallet funding requirements.

Timing and target selection are separate. Fixed or uniform cadence creates one
opportunity at a time; weighted targets determine where it is offered. Capacity,
reservations, expiration and unavailable targets may skip opportunities. There is
no catch-up burst or replay of skipped slots.

The scoped Bitcoin control workload holds mutation credentials and writes durable
per-target `BitcoinExecution` records. It independently enforces the aggregate's
initialization ceilings. Actor RPC credentials are method restricted. A durable Armed
request without a known receipt closes its target; the
worker does not infer server-side cancellation from an HTTP timeout. Retained receipts
are accounted independently of later opportunity or producer selection; new sends
wait for that accounting.

A `BitcoinBlockScheduleOverride` temporarily replaces timing for an exact network
and production instance. Expiry/cancellation resumes the latest admitted baseline,
not a saved old policy. Finite actions use the same target reservation and executor.
See [schedules and actions](../design/public-api/operations.md).

Production observes frozen initialization ceilings and native release evidence.
Crossing a height is not permission to skip enrollment or contract confirmation.
After initialization, it maintains the declared ongoing schedule.

Use participant/root cooperative controls to pause new production. Receipt
collection and native observations continue. Follow [network cleanup](operations.md#removal-and-cleanup)
before deleting a namespace; genesis and execution records stay protected until
participant consumers finish disposal.
