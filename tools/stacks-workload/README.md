# Stacks workload generator

`stacks-workload` submits bounded transaction workloads without moving scenario
planning into an operator. Build it from the repository root:

```bash
GOWORK=off go -C tools/stacks-workload build -o stacks-workload .
```

Commands accept exactly one JSON request on stdin. `submit` queries the sender's current
canonical nonce, constructs a finite contract deployment or load-call batch with
[`libs/stacks`](../../libs/stacks/README.md), submits each transaction once, and emits
JSON Lines for inputs, submission boundaries and exact canonical inclusion. An uncertain send is
never retried. `timeoutSeconds` bounds nonce discovery, submission and observation together.
Private keys are input-only and never emitted.

One load-call request is:

```json
{
  "endpoint": "http://127.0.0.1:20443",
  "privateKey": "<hex-private-key>",
  "sender": "ST...",
  "feeMicroSTX": 3000,
  "contract": "ST....load-driver",
  "function": "run16",
  "count": 10,
  "writes": 16,
  "reads": 128,
  "payloadBytes": 1024,
  "keyBase": 1000,
  "timeoutSeconds": 900
}
```

A deployment instead supplies `contract` as its name, `contractSource` and optional
`clarityVersion`; call-count, function and load-shape fields must be absent.

See the [contract-load fixtures](../../examples/experiments/contract-load/README.md)
for workload shapes. Use [stacks-evidence](../stacks-evidence/README.md) separately
for telemetry queries and [Kubernetes Events](../../examples/experiments/phase-event.yaml)
for phase markers. Network and fault orchestration remains external.

## Bounded pressure and variation

Calls may add these controls to the request above:

```json
{
  "intervalMilliseconds": 500,
  "maxOutstanding": 8,
  "observationConcurrency": 4,
  "durationSeconds": 120,
  "variation": {
    "seed": 42,
    "functions": ["run2", "run8", "run16"],
    "writes": {"min": 1, "max": 16},
    "reads": {"min": 4, "max": 128},
    "payloadBytes": {"min": 16, "max": 1024}
  }
}
```

The fragment supplements a complete submit request; it is not independently valid.
Use a funded sender that no other process is using. The tool reads its canonical
nonce once, then submits in nonce order through **one mutation lane**. It does not
reconstruct outstanding mempool state or coordinate other writers.

| Control | Default | Bound and meaning |
| --- | --- | --- |
| `count` | Required for calls | 1–10,000 planned calls; deployment is one transaction |
| `intervalMilliseconds` | 0 (unpaced) | 0–60,000; minimum spacing between submission starts |
| `maxOutstanding` | 100 | 1–100 submissions without observed canonical inclusion |
| `observationConcurrency` | 1 | 1–16 simultaneous read-only inclusion requests |
| `durationSeconds` | No separate cutoff | Positive and below `timeoutSeconds`; stops new sends, then drains observations |
| `timeoutSeconds` | Required | 1–3,600; includes nonce discovery, signing, submission and observation |

Zero for outstanding/concurrency selects the default. The duration clock starts
after nonce discovery; the total timeout starts before it. Count and duration are
independent ceilings. Backpressure can result in fewer submissions. After a slow
RPC or full outstanding window, the tool does not send a catch-up burst. Each
observation sweep starts at least three seconds after the previous sweep finishes;
it can run up to `observationConcurrency` reads at once. Submissions wait during a
sweep, so actual throughput may be below the requested upper rate.

Signing is lazy and pending records retain public identities, not transaction
bodies. Cancellation stops new sends and cancels active reads; a submission already
sent may still execute. An unacknowledged submission or failed observation stops
the invocation with nonzero exit and pending identities retained in JSONL. Neither
is automatically retried. Sweep errors report the number of unavailable observations
and preserve the first sanitized RPC error. The invocation's cancellation or total
deadline identity remains available even when the transport hides its underlying
error. This does not add RPC error categories or expose response bodies. A new invocation is not a recovery
mechanism: inspect account/transaction state before using that sender again.

`Started` records the starting nonce, effective controls and variation specification.
Its `inputs` object contains sender, contract, fee, total timeout and either `call`
(the fixed base function, writes, reads, payload length and key base) or `deployment`
(effective Clarity version and `sourceDigest`: `sha256:<hex>` of the exact source
bytes). Retain deployment source separately; its digest identifies but cannot
reconstruct it. Private keys, endpoint details and source text are not emitted.
`Started.inputs.call.keyBase` is the request base; `Authorized.shape.keyBase` is
the effective base for that transaction's ordinal.

Each `Authorized` event precedes submission and includes ordinal, nonce, TxID and actual
call shape; authorization alone does not prove a send occurred. `Submitted` means
an exact TxID acknowledgement. `Included` records an exact canonical observation
and execution success flag, without finality guarantees. A contract error still
consumes its nonce and releases outstanding capacity. `Rejected`,
`SubmissionUnconfirmed` and `ObservationUnavailable` identify interruption
boundaries. `Finished` reports attempted, included, pending and unsent counts;
`completed=true` means the bounded invocation drained successfully, not that every
planned call was sent or every contract call returned success. Hard kills or output
failures may prevent a final record. Zero counts may be omitted. `Started` has no
ordinal; `Finished` has neither nonce nor ordinal. Transaction events always retain
both fields, including zero values.

### Reproducibility boundary

`sha256-shapes-v1` maps each ordinal independently. For each varied field, hash the
UTF-8 string `sha256-shapes-v1:<seed>:<ordinal>:<field>` using SHA-256, take the first
eight bytes as an unsigned big-endian integer, reduce modulo the inclusive range
size, and add its minimum. Field names are `function`, `writes`, `reads` and
`payloadBytes`; functions use their ordered list indices. The modulo mapping is
not a claim of perfectly uniform sampling. The seed is an input-generation knob,
not a cryptographic secret or an actor RNG seed.

Omitted ranges preserve the fixed request fields. Writes range from 0–64, reads
from 0–1,024 and buffers from 0–4,096 bytes; supplied contracts may impose narrower
limits (the example driver accepts at most 512 reads). A selected function must
accept the two-list call shape. Up to 16 functions may be selected. Calls use
`keyBase + ordinal*64 + writeIndex`; payload byte `offset` is
`(offset + ordinal + writeIndex) mod 251`. Overflow is rejected before submission.

The same version, seed and input fields produce the same planned call inputs,
regardless of pauses caused by backpressure. Identical transaction bytes additionally
require identical signing identity, starting nonce, fee and transaction settings.
Rate and concurrency are bounds, not timing guarantees. Scheduling, inclusion,
peer selection, actor randomness and distributed outcomes are uncontrolled.
The user or external agent coordinates experiments and evaluates observations;
this tool does not schedule faults, classify failures or promise repeatable results.
