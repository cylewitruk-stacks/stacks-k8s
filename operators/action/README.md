# Action operator

Independent Go runtime for bounded Bitcoin action lifecycles. The default
`BitcoinBlockGeneration` controller and optional `BitcoinReorganization`
controller project the network executor's durable ledger into action status
and cleanup finalizers. They do not issue RPCs or write the ledger.

- [Operating guide](../../docs/action-operator/operations.md)
- [Helm chart](../../charts/stacks-action-operator/README.md)
- [Atomic action contract](../../docs/design/actions.md)

```bash
make -C operators/action verify
```

Public controller packages permit paired executor/lifecycle regression tests
in the network module. Production binaries only share the versioned API module;
this runtime does not import another operator's implementation.
