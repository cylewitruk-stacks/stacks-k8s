# Development

## Prerequisites

- Go 1.27.1
- Helm 3
- Docker for container validation and local images
- Network access when envtest assets or vulnerability data are not cached

No committed `go.work` is required. The modules are intentionally verified in
isolation so local workspace state cannot hide a missing dependency or combine
the independently versioned API, runtime, and generator dependency graphs.

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
version), Docker, and kubectl. Start the Docker engine first. They use your
current Docker connection; set `DOCKER_CONTEXT=desktop-linux` explicitly when
using Docker Desktop. Docker Desktop's built-in Kubernetes provisioner is not
used. The checked-in configuration pins Kubernetes **1.37.0** by image digest
with one control-plane node and two workers.

From the repository root:

```bash
make cluster-create  # Create stacks-k8s; an existing cluster is left intact.
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

## Stacks transfer profile

Network verification also runs the locked SDK tests with Node.js 24 or newer
and npm. Verification runs `npm ci --ignore-scripts`, so an existing
`node_modules` directory does not substitute for registry access. Offline CI
must prepopulate the npm cache with the exact locked packages and set
`npm_config_offline=true`; Go modules/toolchains and envtest assets must also
already be available. `make vuln` requires access to the npm advisory registry
and Go vulnerability database for a current audit. The separate worker Dockerfile
is `operators/network/transactions/Dockerfile`; it packages offline SDK adapters
and the Go manager for separate transfer and managed-operation Deployments.
Controllers own managed initialization and renewal. See the
[qualification record](network-operator/managed-operation-qualification.md).

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
