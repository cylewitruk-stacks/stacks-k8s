# Operator development

The Go module contains aggregate and actor controllers, capability workload
controllers, and independently scoped execution workers. One installation serves
network declarations across namespaces. See the chart's
[architecture document](../../docs/network-operator/architecture.md).
Development requires Go 1.27.1 and Node.js 24 or newer with npm. Network API
types and the matching isolated generator toolchain live under `../../apis/network`.

The [Bitcoin baseline guide](../../docs/network-operator/bitcoin-production.md)
covers static credential provisioning and real Core 31.1 acceptance tests.
The [Stacks transfer guide](../../docs/network-operator/stacks-production.md)
covers capability-owned SDK workers and managed direct PoX-4/PoX-5 participation; its
[qualification record](../../docs/network-operator/stacks-qualification.md)
includes commands for the opt-in live transfer tests.

Run development commands from the repository root:

```bash
make generate
make verify-network
```

After installing the chart in an isolated namespace, exercise real Kubernetes
workload controllers and garbage collection with a non-production test image:

```bash
docker build --tag stacks-network-live-actor:local \
  operators/network/internal/integration/testdata/actor
kind load docker-image stacks-network-live-actor:local --name YOUR_KIND_CLUSTER
```

Then run:

```bash
STACKS_NETWORK_LIVE_NAMESPACE=stacks-network-qualification \
STACKS_NETWORK_LIVE_ACTOR_IMAGE=stacks-network-live-actor:local \
STACKS_NETWORK_LIVE_KUBECONFIG=/absolute/path/to/kubeconfig \
STACKS_NETWORK_LIVE_CONTEXT=kind-YOUR_KIND_CLUSTER \
STACKS_NETWORK_LIVE_RESULT=/absolute/path/to/live-summary.json \
make -C operators/network test-live
```

The live suite creates a uniquely named disposable topology and verifies
manager restart, actor addition and removal, configuration rollout,
suspend/resume, Pod replacement, inventory rebinding, and clean teardown.
Run it only in a namespace dedicated to this chart.
The test image supplies the `/bin/bash` configuration-rendering dependency and
`nc` listener used to isolate Kubernetes lifecycle behavior from actor protocol
behavior; it is not a supported network actor image.
The explicit kubeconfig is required; the context may be omitted when that file
already selects the qualification cluster.

Generated deepcopy code and CRDs are committed. Do not hand-edit them.

## Optional finite Bitcoin generation

First install the action chart in the same namespace. Then enable
`bitcoinGeneration.enabled` together with `bitcoinProduction.enabled` on the
network release to use bounded `BitcoinBlockGeneration` actions. The network
chart adds read-only action Role rules; the action chart owns the 64-object
namespace quota and lifecycle permissions.
See the [operating profile](../../docs/network-operator/bitcoin-generation.md)
for immutable fields, admission, cancellation, attribution, and recovery.

## Optional local reorganization

`bitcoinReorganization.enabled` adds bounded local suffix replacement to the
shared executor and requires `bitcoinProduction.enabled`. Provision the helper's
explicit `--reorganization` RPC profile in a fresh environment first.
See the [operating guide](../../docs/network-operator/bitcoin-reorganization.md)
for depth/boundary limits, retained cleanup, cancellation, and teardown.

The Go `stacks-environment` command renders a managed declaration. Separate
capability workers own initialization, contract deployment and stacking renewal.
Enable `stacksOperation.enabled` alongside Bitcoin and transfer production.
Shared TOML templates
and immutable network genesis are described in [configuration](../../docs/network-operator/configuration.md).
