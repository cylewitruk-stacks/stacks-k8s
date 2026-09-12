# Finite Bitcoin generation

`BitcoinBlockGeneration` requests 1–100 acknowledged single-block RPCs against a logical
Bitcoin-node participant and exact network UID. Immediate, fixed, uniform and explicit cadence are
bounded by the immutable timeout. Each action shares the existing production executor and target
reservation; unknown dispatch is never replayed.

Install the [action operator](../../charts/stacks-action-operator/README.md) and enable
the corresponding network executor permission. See the
[public action contract](../design/public-api/operations.md) and
[examples](../../examples/actions/). Request specs are immutable; receipts and cleanup
acknowledgement precede reservation release. Readiness, mechanism completion and
protocol recovery are separate observations.
