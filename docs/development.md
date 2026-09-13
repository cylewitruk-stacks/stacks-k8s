# Development

## Prerequisites

- Go 1.27.1
- Helm 3
- Docker for container validation and local images
- Network access when envtest assets or vulnerability data are not cached

No committed `go.work` is required. The modules are intentionally verified in
isolation so local workspace state cannot hide a missing dependency or combine
the independently versioned API, runtime, and generator dependency graphs.

## Domain vocabulary

Public kind, lifecycle and cadence constants live with their API types in
`apis/network`; runtime-only metadata stays with its owning controller package.
Use typed values through domain helpers and convert to strings at generic
Kubernetes, CLI, naming or document boundaries. Use concrete object switches
for supported action dispatch and return errors for unknown or nil inputs.

Keep schema enums, YAML examples and wire-test expectations independent of
implementation constants. A constant refactor must preserve serialized values
and generated schemas; a named Go string type does not validate external input
or make switches exhaustive. Condition reasons remain extensible strings, with
shared constants for reasons that coordinate execution across controllers.

## Kubernetes compatibility

The development target is Kubernetes **1.37.0**. API and runtime modules pin
`k8s.io/*` release modules to **v0.37.0** and operators use
[controller-runtime v0.25.0](https://github.com/kubernetes-sigs/controller-runtime/releases/tag/v0.25.0).
Generators remain at controller-tools **v0.22.0**, which already uses the
Kubernetes 0.37 family. Keep these module families aligned when updating pins.
Go 1.27.1 and the existing container build toolchain satisfy their requirements.

All envtest suites download **1.37.0** API-server assets by default, including
the Chaos Mesh admission tests. Their explicit download configuration selects
the pinned assets. Envtest verifies API-server behavior, not kubelet, CNI,
storage, or native fault injection. Helm lint also targets 1.37.0 in every chart.

Use the selected standalone kind cluster for live testing, with fresh namespaces
and explicit kubeconfig/context selection. Cluster creation is separate from
repository verification. The Helm `kubeVersion` constraints are API minimums,
not a tested-version matrix. Earlier qualification records retain their original
Kubernetes versions. See the [1.37 live qualification](local-cluster-qualification.md)
for cross-node native fault results and the unresolved Stacks recovery limit.

## Local kind cluster

The lifecycle helpers use standalone **kind 0.33.0** (or a compatible newer
version), Docker, kubectl, Helm 3 and `shasum`. Helm and `shasum` are optional
when both default add-ons are disabled. Start the Docker engine first. They use your
current Docker connection; set `DOCKER_CONTEXT=desktop-linux` explicitly when
using Docker Desktop. Docker Desktop's built-in Kubernetes provisioner is not
used. The checked-in configuration pins Kubernetes **1.37.0** by image digest
with one control-plane node and two workers.

From the repository root:

```bash
make cluster-create  # Create stacks-k8s with Headlamp and Metrics Server.
make cluster-stop   # Stop its node containers, retaining their data.
make cluster-start  # Resume existing containers and wait for node readiness.
make cluster-destroy # Delete stacks-k8s and all data stored in its nodes.
```

Create and start write `tools/local-cluster/kubeconfig`, which is ignored by Git,
with context `kind-stacks-k8s`. Your global kubeconfig is unchanged. To use it:

```bash
export KUBECONFIG="$PWD/tools/local-cluster/kubeconfig"
kubectl --context kind-stacks-k8s get nodes
```

### Dashboard and resource metrics

Creation installs checksum-verified upstream charts: **Headlamp 0.45.0** and
**Metrics Server chart 3.14.0 / application 0.9.0**. An existing cluster is left
intact if creation fails. An add-on failure returns an error and leaves the new
cluster available for retry with the install commands below.

```bash
make cluster-create HEADLAMP=false # Metrics Server remains enabled.
make cluster-create HEADLAMP=false METRICS_SERVER=false # Bare cluster.
make cluster-headlamp-install # Add/update both on an existing cluster.
make cluster-metrics-install # Install/check only Metrics Server.
make cluster-headlamp # Foreground port-forward; Ctrl-C closes it.
```

Open <http://127.0.0.1:8080> and obtain a login token in another terminal:

```bash
make cluster-headlamp-token
```

The token requests a one-hour lifetime and grants **cluster-admin**, deliberately
matching this local development tool's purpose. It is printed only by the token
command and is not saved by the helpers. Headlamp runs in namespace `headlamp`
with token authentication; access uses a loopback-only port-forward, without
ingress. Set `HEADLAMP_PORT=8081` on `cluster-headlamp` to use another local port.

[Headlamp](https://headlamp.dev/docs/latest/installation/metrics-server/)
automatically reads the Kubernetes metrics API for CPU/memory usage; no plugin
is required. Metrics Server runs in `kube-system`, with `--kubelet-insecure-tls`
for kind's self-signed kubelet serving certificates. These values are local-kind
configuration, not a production installation profile. An existing externally
managed metrics API is reused and checked for availability without taking over
its release. Our own release is upgraded to the repository's pinned values.

`HEADLAMP` and `METRICS_SERVER` accept `true` or `false`. To install only Headlamp,
use `make cluster-headlamp-install METRICS_SERVER=false`. Explicit install
commands do not create or start the cluster. All use the repository kubeconfig
and `kind-stacks-k8s` context. Chart downloads require registry/repository access.

```bash
make cluster-headlamp-uninstall
```

Uninstall removes Headlamp's release, retaining its namespace and Metrics Server.
Stop/start preserves installed add-ons and does not reinstall removed ones;
destroy removes everything with the cluster. Headlamp is independent of the
operator charts and complements the observability operator's evidence work.

### Chaos Mesh

Optionally install the pinned **Chaos Mesh 2.8.4** chart with Helm 3 and `shasum`:

```bash
make cluster-chaos-install
```

This verifies the chart SHA-256, installs or reconciles release `chaos-mesh` in
namespace `chaos-mesh`, and waits for readiness. Repeated calls reapply the
repository's values, including containerd support, namespace filtering, and a
disabled dashboard/DNS service. It uses the local kubeconfig and never creates
or starts the cluster. Namespace enrollment, fault profiles, workloads, and
qualification remain separate; see [native faults](chaos/operations.md#install).
Successful installation does not qualify live fault injection on Kubernetes 1.37.

Stop/start preserves disk data, not uninterrupted workload execution.
`make verify-local-cluster` tests command behavior with fake CLIs and does not
touch a cluster.

## Common commands

```bash
make fmt
make generate
make test
make test-race
make test-integration
make helm-verify
make rbac-verify
make modules-verify
make verify
```

`make generate` runs the isolated generator under `apis/network/tools`, updates
generated deepcopy code, and writes network and action CRDs directly to their owning charts.
Generated code and CRDs must be committed with their API source changes; CI
rejects drift.

`make helm-verify` validates values schemas, renders supported configurations,
and structurally checks the manager Deployment's security posture. It also
executes negative renders for unsafe replica settings and malformed image-pull
secret or service-account names. The shared structural validator lives under
`tools/chart-policy` and is not part of any operator binary.

`make docker-check` validates the operator and worker Dockerfiles, while `make docker-build`
compiles all three operator images from the repository's default-deny root build
context. CI runs both so sibling-module changes cannot silently break image
builds.

The network runtime consumes the types-only `apis/network` module through a
repository-local replacement. Published releases tag the API module first so
that the runtime module's declared version remains resolvable without that
replacement.

Paired executor/lifecycle tests in the network module import the action
controller packages through a test-only local module requirement. Production
binaries do not import another operator’s runtime; the repository contract
check enforces that boundary. Each module retains its own dependency pins, but
Go’s minimal version selection includes the test dependency in the network
module’s build list. An action-side dependency upgrade can therefore change
network-selected versions; qualify the pair together. A separate paired-test
module is deferred until independent version divergence warrants it.

Changes to admitted inventory, leaf specifications, actor Service ports, or
kubelet image-ID parsing must update the relevant fixture under
[`contracts/`](../contracts/). Each operator verifies those fixtures through
its own production code; do not replace that boundary with a shared runtime
package.

Run the opt-in checks separately when their external services are available:

```bash
make vuln
make docker-check
```

Keep controller packages aligned with the Kubernetes resource they reconcile.
Small cohesive packages are valid boundaries; merge them only when measured
coupling or duplication demonstrates a clearer design.

## Portable protocol library and SDK oracle

`libs/stacks` implements runtime RPC, Clarity values, address encoding and transaction
signing in Go. Network verification also runs the pinned offline Stacks.js oracle
under `operators/network/transactions` using Node.js 24+ and npm. Runtime images
contain no Node.js or signing subprocesses.

Verification uses `npm ci --ignore-scripts`. Offline CI must prepopulate the exact
locked npm packages and set `npm_config_offline=true`; Go modules, toolchains and
envtest assets must also be cached. Current vulnerability auditing requires the
npm advisory registry and Go vulnerability database.

## Native-fault contract verification

The Verify workflow's chart-policy job and `make verify` include the optional
Chaos Mesh profile's rendered permissions
and real API-server CEL tests. Its isolated `tools/chart-policy` module uses the
same Kubernetes/controller-runtime family as the operators. The upstream
NetworkChaos 2.8.4 CRD is downloaded and SHA-256 verified into the temporary
directory. Set `STACKS_CHAOS_CRD_FILE` to that exact file for offline use; a
missing or mismatched file fails verification. Envtest still needs its cached
API-server binaries. No upstream CRD is vendored or regenerated by this project.

Live fault qualification remains separate and opt-in; see
[the disposable fixture guide](chaos/operations.md#repeat-qualification).

The chart-policy job installs Go and Helm, runs `make verify-chart-policy`
(including the admission envtest), and verifies the native profile chart. Security
workflow triggers include `tools/**` and `charts/**`; the tool module's dependency
checksum participates in caching. Live injection remains outside CI.
