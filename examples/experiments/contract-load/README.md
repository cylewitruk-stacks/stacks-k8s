# Nested contract-load fixture

These are the exact contract sources deployed in the
[defensive load/fault experiment](../../../docs/experiments/contract-load-and-faults.md).
They are external experiment inputs, not an operator workload or scenario controller.

Deploy `load-store`, `load-relay`, then `load-driver` under one funded testnet principal
using Clarity 4. The relative contract references require these names and deployment order.
Use unused funded accounts to submit `load-driver.run2`, `run8` or `run16`.
The repository [workload tool](../../../tools/stacks-workload/README.md) constructs,
submits once and observes this two-list call shape without owning scenario sequencing.

- `load-store`: maps with 2, 8 or 16 buffer fields; writes repeat the supplied buffer
  across all fields, and reads sum their lengths.
- `load-relay`: a cross-contract hop to each store read/write operation.
- `load-driver`: folds over writes and reads, calling the relay at each step.

Each call takes `writes`, a list of up to 64 `{key: uint, payload: (buff 4096)}` tuples,
and `reads`, a list of up to 512 uint keys. It returns the sum of the write payload
lengths and read field lengths. Use fresh keys when measuring storage growth; reusing
keys measures updates. The report records the tested sizes and batch counts.

Sign and submit through the portable Go library or another compatible external client.
Record the exact TxID before submission, use one sender per nonce sequence, and verify
canonical inclusion and execution result. These fixtures do not manage accounts,
nonces, fees, submission retries, fault timing or evidence collection.
