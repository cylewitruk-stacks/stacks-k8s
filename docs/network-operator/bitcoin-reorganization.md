# Bitcoin reorganization

`BitcoinReorganization` replaces a bounded suffix on one Bitcoin-node participant. It captures
identity, invalidates the selected boundary, generates depth+1 replacements and removes its own
invalidity marker. Cleanup does not roll back the resulting best chain. Ambiguous execution or
unconfirmed cleanup remains visible and retains exclusion.

Install the [action operator](../../charts/stacks-action-operator/README.md) and enable
the corresponding network executor permission. See the
[public action contract](../design/public-api/operations.md) and
[examples](../../examples/actions/). Request specs are immutable; receipts and cleanup
acknowledgement precede reservation release. Readiness, mechanism completion and
protocol recovery are separate observations.
