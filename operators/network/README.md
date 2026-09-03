# Operator development

The Go module contains one aggregate controller, three leaf controllers, and a
shared workload collaborator. See the chart's
[architecture document](../../docs/network-operator/architecture.md).
Development requires Go 1.27.1; the matching generator toolchain is pinned in
`tools/go.mod`.

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
