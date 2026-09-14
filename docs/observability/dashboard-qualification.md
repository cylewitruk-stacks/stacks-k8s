# Persistent dashboard qualification — 2026-09-14

The disposable `stacks-k8s` cluster was destroyed and recreated through the repository
helpers, with Kubernetes 1.37.0 on three arm64 nodes. The control-plane container
publishes `127.0.0.1:14000` to NodePort `30400`. The local observability chart profile
uses GreptimeDB 1.2.0, upstream chart 0.4.14, and observation image
`stacks-observability-operator:dashboard-20260914`.

The dependency is fetched from its canonical Helm repository using `Chart.lock`.
Downloaded archives are ignored by Git. No chart binary is committed.

Local evidence: `/tmp/stacks-dashboard/`. Credentials are excluded from committed
artifacts and were supplied through the administrator-created `greptime-auth` Secret.

| Check | Observed result |
| --- | --- |
| Fresh installation | One Helm release installed the operator and bundled backend; the post-install Job configured the `public` database TTL to `1day`. |
| Host access | `/dashboard/` returned HTML with HTTP 200 through loopback, without a `kubectl port-forward` process. The NodePort Service exposes only HTTP; the backend's other protocol ports remain ClusterIP-only. |
| Authentication | Unauthenticated SQL returned 401. The reader queried data successfully and could not insert a row. The UI requires the same reader connection settings; opening its public page does not authenticate queries. |
| Exposure opt-out | `dashboard.enabled=false` removed the NodePort Service and host access. Re-enabling restored access and the existing row remained readable. |
| Stop/start | The cluster helpers restarted all three nodes; Docker retained its port mapping and the stored row survived. An immediate HTTP probe timed out before application recovery; the subsequent probe succeeded. Node readiness is not backend readiness. |
| Uninstall/reinstall | The backend PVC retained the same UID after Helm uninstall. Reinstall reused it and the reader recovered the original row. Successful initialization hook Jobs were removed. |
| OTLP contract | The existing live append/redaction test passed through the persistent endpoint under race detection, retaining two identical timestamp/tag records with zero credential-canary leaks. |
| Chart packaging | A packaged chart rendered successfully with the downloaded dependency. Default, internal-backend and local-dashboard modes pass structural tests, including custom name/port and initializer opt-out. |

`http-before.log`, `http-reenabled.log`, `http-after-start-retry.log`,
`http-after-reinstall.log`, `ingestion-live.log` and the PVC JSON files record these
checks. Early SQL probe attempts used reserved column names and were corrected to
quoted identifiers; no product change was needed.

Snapshot `888838972dc3aa8bea923c3b34933d81d219c80d` passed full isolated
`make verify` with `EXIT_CODE=0` and `DRIFT_EXIT_CODE=0` in `verify.log`.
`make vuln`, `make docker-check`, focused chart tests under race detection,
Markdown lint and relative-link checks also passed. Runtime hashes match that
snapshot for the initial delivery. Subsequent dependency-helper and chart-test
checks are recorded in `/tmp/stacks-dashboard-followup/` and
`/tmp/stacks-dashboard-helm/`; no cluster rollout was needed. The temporary verification
repositories and local credential file were removed. Both qualification tables
were dropped after the retention checks; the cluster credential Secret remains.

This qualification covers installation, HTTP access, authentication and storage
lifecycle. It does not qualify Perses panel editing, dashboard-definition persistence,
Ingress, other host platforms, HA storage or protocol recovery. Browser automation was
unavailable; HTTP serving and authenticated query behavior were tested directly.

The recreated cluster is left running with Headlamp, Metrics Server, Chaos Mesh,
the network operator and the bundled observability installation. No network fixture
is retained. Previous cluster data was removed by the authorized recreation. Future
cluster destruction also deletes retained PVC data; Helm uninstall alone does not.
