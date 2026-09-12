# Bitcoin RPC ambiguity probes

These manual probes support the [execution contract](../../../docs/design/target-admission-and-rpc-execution.md).
They are not controller, concurrency or recovery qualification.

```bash
GOWORK=off go test tools/spikes/r1-r2/exclusion_test.go
sh tools/spikes/r1-r2/bitcoin-rpc-check.sh
```

The Docker probe queues generation behind a read-only wait, lets the generation
client time out, then checks the resulting height. It uses an installed
`bitcoin/bitcoin:31.1` image by default, creates no published ports or host mounts,
and removes its temporary container on exit. `BITCOIN_PROBE_IMAGE` selects another
installed image. Success demonstrates that a client timeout need not stop queued
server work; it is not a Bitcoin defect or a fence against future execution.

The serial model covers selected reservation, dispatch and receipt transitions.
It does not model Kubernetes durability, concurrent controllers, reset recovery,
credential fencing, proven-unsent withdrawal or qualified epoch retirement.
