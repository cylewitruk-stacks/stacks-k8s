# Development

## Prerequisites

- Go 1.27.1
- Helm 3
- Docker for container validation and local images
- Network access when envtest assets or vulnerability data are not cached

No committed `go.work` is required. The modules are intentionally verified in
isolation so local workspace state cannot hide a missing dependency or combine
the independently versioned API, runtime, and generator dependency graphs.

## Go lint and formatting

Install the exact `golangci-lint` release recorded in
[`.golangci-lint-version`](../.golangci-lint-version). Local verification and CI
use the same [configuration](../.golangci.yml); a missing or mismatched executable
fails with an installation/version message. Set `GOLANGCI_LINT` to an absolute
executable path when it is not on `PATH`.

| Command | Behavior |
| --- | --- |
| `make lint` | Check every loadable Go module, including tests and `integration,live` code; never execute tests or rewrite source. |
| `make fmt` / `make fmt-check` | Check formatting across all modules and print diffs without changing files. |
| `make fmt-fix` | Apply `gofumpt`, `goimports` and `golines` formatting. Review the resulting diff. |
| `make verify` | Require lint and formatting checks alongside the existing verification gates. |

Each module runs with `GOWORK=off` and the explicit root configuration. The
`apis/network/tools` and `operators/observability/tools` modules contain only
build-tagged generator imports; they receive formatting and module-integrity
checks rather than package analysis. Generated sources are excluded using their
standard generated-code marker; generator drift remains a separate check.
Kubebuilder markers are exempt from line-length checks because each marker must
stay on one line. Test helpers may take `testing.T` before context.

Lint includes the standard analyzers plus the configured correctness, security
and style checks. The line-length limit is 120; `golines` wraps supported Go
syntax, while `lll` can also flag long comments or literals requiring manual
review. A suppression must name its linter and explain the reason. CI and local
checks report existing findings as failures; there is no baseline suppression or
new-code-only filter. A clean lint run does not replace tests or vulnerability
checks.

For eligible automatic lint fixes, run `golangci-lint run --fix` from the module
directory with the same root configuration, then review the diff and rerun lint.

## Domain vocabulary

Keep machine-readable vocabulary with its owner. Public kinds, REST resource
names, condition types and cross-controller reasons live in their API packages.
Producer-specific reasons stay in the producer package. Worker modes, signing-key
roles, metadata selectors and RPC methods belong to the component defining that
process or protocol boundary. Consumers reuse those definitions.

Use named string types for domain fields and helper signatures that benefit from
them. Kubernetes condition types/reasons and generic object bindings remain
extensible strings with named constants. A `Running` condition, desired operation,
phase and reason are distinct symbols even when their wire values match. Constants
do not validate unknown input or make Go switches exhaustive.

### Object identity and supported kinds

When a concrete object is available, use its Go type instead of passing a second
Kind argument. The network runtime's `internal/objectref` constructors accept
specific resource types and capture name/UID without reading contents; fingerprints
remain explicit. Metadata-only Secret references require the expected GVK.

Register resource-focused controllers with typed prototypes. Use the manager's
scheme for generic GVK lookup and fresh object allocation, preserving the selected
version and excluding previously fetched fields. Never silently choose between
ambiguous registrations. Scheme registration does not authorize a capability:
keep supported-kind checks at declaration, request and persisted-reference
boundaries. Expected owner kinds, dynamic references and SSA TypeMeta still need
explicit Kubernetes identity.

### Literal boundaries

The following literals remain intentional; they are declarations or independently
specified data rather than undocumented controller vocabulary.

| Boundary | Motivation |
| --- | --- |
| Constant definitions | The owning declaration must define the actual wire value once. |
| JSON/TOML tags, codec keys, URL/path grammar and fixed configuration presets | Keeping the format at its encoder/decoder or preset makes the wire layout reviewable. Extract a shared identifier when another runtime component depends on the same contract. |
| RBAC validation fixtures and native option allow/deny tables | These express an independently checked permission/configuration policy. Keep expected values independent of the producer and avoid obscuring the policy behind shared production constants. |
| Kubernetes security-context literals such as dropping `ALL` capabilities | A self-contained native policy declaration is directly auditable; use upstream constants where provided. |
| CLI flag declarations, help, diagnostics and log messages | Their declaration site explains their meaning. Worker mode values transported between Pods and entrypoints use constants. |
| Tests, schema enums, YAML and contract fixtures | Independent literal expectations catch accidental wire changes; coupling them to production constants would weaken that check. |
| Empty strings and syntax delimiters | These express absence or grammar, not a product state. |

A one-use status reason is still machine-readable vocabulary. New literals used
for status publication, branching, resource lookup or producer/consumer identity
must use an existing domain symbol or receive a named owner. Do not create a
repository-wide miscellaneous constants package or generate code merely to ban
all string literals.

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

`make docker-check` validates the operator and worker Dockerfiles and all supported
collector source configurations against the pinned OTel binary. Run it after changing
collector templates or configuration rendering, as well as dependency/container changes.
`make docker-build`
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
