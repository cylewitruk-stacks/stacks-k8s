# Stacks workload generator

`stacks-workload` submits bounded transaction workloads without moving scenario
planning into an operator. Build it from the repository root:

```bash
GOWORK=off go -C tools/stacks-workload build -o stacks-workload .
```

Commands accept exactly one JSON request on stdin. `submit` queries the sender's current
canonical nonce, constructs a finite contract deployment or load-call batch with
[`libs/stacks`](../../libs/stacks/README.md), submits each transaction once, and emits
`Authorized`, `Submitted`, and exact canonical `Included` JSON Lines. An uncertain send is
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
