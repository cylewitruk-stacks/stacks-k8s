# stacks-preflight

Check explicitly selected experiment prerequisites without changing Kubernetes
resources or backend data. The result is a point-in-time JSON report with
`Pass`, `Fail` or `Unknown` per check; any non-pass produces nonzero exit.
It does not initialize a network, grant permissions, inject faults or decide when
to run an experiment.

```bash
GOWORK=off go -C tools/stacks-preflight run . < request.json > preflight.json
```

```json
{
  "kubeconfig": "/path/to/kubeconfig",
  "context": "kind-stacks-k8s",
  "namespace": "lab",
  "network": "network",
  "networkUID": "12345678-1234-1234-1234-123456789abc",
  "telemetry": "capture",
  "operatorNamespace": "stacks-observation-system",
  "operatorDeployment": "telemetry-stacks-observability-operator",
  "chaos": true,
  "maxAgeSeconds": 60,
  "requiredSources": ["stacksnetworks.network.stacks.org/v1alpha2"],
  "backend": {
    "endpoint": "http://127.0.0.1:14000",
    "authorization": "Basic supply-securely",
    "tables": [{"name": "recording_objects", "timeColumn": "timestamp"}]
  }
}
```

Select actual deployment and table names from your installation. Supply backend
credentials through stdin rather than keeping them with exported evidence. Use a
trusted kubeconfig with static certificate/token credentials and a read-only backend
principal. Exec and auth-provider plugins are rejected because their execution
cannot be bounded by the API request deadline. No credential values,
container environment, Secret bodies or raw server errors appear in the report.

## Checks

- The named root has exactly the requested UID and is not deleting. Existence
  does not assert initialization, readiness, protocol progress or experiment health.
- The named recording targets that root, is admitted, and has current-generation
  `WorkloadsReady=True`. Its current recorder heartbeat must be fresh and backend-ready.
- Each explicitly requested recorder source is available with a fresh timestamp.
  Source availability does not prove every intermediate object change was captured.
- The named observer Deployment template includes the namespace in its explicit
  `manager` container’s effective `--watch-namespace` list. This checks desired
  scope; it does not prove every operator Pod has rolled to that template.
  Non-watch flags must use the chart’s
  `--key=value` form; ambiguous separate-value or bare flags fail closed.
- Separately, `observer-rollout` requires a positive desired replica count, a
  current observed generation, and all desired replicas updated, ready and
  available with no extra or unavailable replicas. This is the Deployment
  controller's report, not independent per-Pod image identity verification.
- With `chaos=true`, the namespace must have
  `chaos-mesh.org/inject=enabled`. This checks the namespace's opt-in to the
  pinned Chaos Mesh installation, not daemon health, permissions, injection or
  fault effect. Check the chosen native resource and observe its actual effects
  and recovery separately.
- Every requested backend table has at least one row for the network UID in the
  last `maxAgeSeconds` (15–600). Select one to eight tables with `timestamp` or
  `greptime_timestamp`. Sparse event tables can legitimately have no recent rows;
  select sources appropriate to the phase, such as continuously emitted logs or
  metrics. A passing table check does not assert per-actor coverage or completeness.

Heartbeat, source and backend checks allow timestamps up to five seconds ahead
of the tool's clock. The maximum age remains `maxAgeSeconds`; clock skew does
not extend the past window.

The command uses uncached Kubernetes GETs and bounded generated SQL SELECTs. It
requires `get` on the selected StacksNetwork, NetworkTelemetry, Deployment and,
when requested, Namespace. It never installs Roles or reads Secrets. Calls have
10-second deadlines within a 60-second invocation budget, without application
retries. Re-run explicitly when you need a new observation; checks are not an
atomic snapshot or a reservation against later changes.
