# Stacks experiment tool

`stacks-experiment` provides bounded external-agent primitives without moving scenario
planning into an operator. Build it from the repository root:

```bash
GOWORK=off go -C tools/stacks-experiment build -o stacks-experiment .
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

`event` renders one network-UID-bound Kubernetes Event. Apply it explicitly:

```bash
stacks-experiment event < phase.json | kubectl create -f -
```

```json
{
  "namespace": "experiment",
  "networkName": "network",
  "networkUID": "00000000-0000-0000-0000-000000000000",
  "phase": "load-start",
  "values": {"batch": 10}
}
```

`export` sends one read-only Greptime SQL query with mandatory network and absolute time
bounds. Participant filtering is optional; windows are limited to 24 hours and results to
10,000 rows. The request supplies a `timestamp` or `greptime_timestamp` column, table name,
backend endpoint, and reader authorization. Redirects and responses over 64 MiB are refused.

```json
{
  "endpoint": "http://127.0.0.1:14000",
  "authorization": "Basic <reader-credential>",
  "table": "stacks_<recording>_objects",
  "timeColumn": "timestamp",
  "networkUID": "00000000-0000-0000-0000-000000000000",
  "from": "2026-09-15T00:00:00Z",
  "to": "2026-09-15T01:00:00Z",
  "limit": 1000
}
```

The [contract-load fixtures](../../examples/experiments/contract-load/README.md) describe
the two-list argument shape used by `submit`. These commands record facts; an agent remains
responsible for sequencing, interpreting results, and retaining the output.
