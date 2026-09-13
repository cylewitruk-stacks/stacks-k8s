# Stacks network operator

This chart installs the composable `v1alpha2` APIs and network operator. It resolves
participants, captures immutable genesis, provisions Bitcoin/Stacks/signer actors, and
manages scoped workers for production, stacking and contract initialization. Network
initialization requires native protocol evidence; ready Pods alone do not establish a
functioning network.

See the [runtime guide](../../docs/network-operator/public-api-foundation.md) for
supported behavior and verification boundaries.

## Installation boundary

The chart requires Kubernetes 1.37. It serves `v1alpha2` and rejects incompatible
existing CRD versions when Helm can query the API server. Offline rendering cannot
inspect existing schemas. Helm does not upgrade CRDs under `crds/`; review and apply
schema updates explicitly. See [installation compatibility](../../docs/network-operator/migration.md)
for the preview chart rename and incompatible old installations.

```bash
STACKS_KIND_CLUSTER=stacks-k8s
STACKS_CONTEXT=kind-stacks-k8s
export KUBECONFIG="$PWD/tools/local-cluster/kubeconfig"
docker build -f operators/network/Dockerfile -t stacks-network-operator:dev .
kind load docker-image stacks-network-operator:dev --name "$STACKS_KIND_CLUSTER"
helm upgrade --install network charts/stacks-network-operator \
  --kube-context "$STACKS_CONTEXT" --namespace stacks-network-system --create-namespace
kubectl --context "$STACKS_CONTEXT" apply -f docs/design/public-api/examples/30-actors.yaml
kubectl --context "$STACKS_CONTEXT" -n lab-30 get stacksnetwork network -o yaml
```

The example starts with `operation: Paused`: account resolution and participant
allocation proceed, but genesis is not created. Set `operation: Running` to capture it.
Select compatible actor images explicitly before starting; the example is a composition
template, not proof that arbitrary image revisions support its protocol profile.

## Permissions and ownership

The controller watches public declarations and writes compiled participants, genesis,
status, workloads, resolver Jobs, and their scoped ServiceAccounts/Roles. It creates empty generated
Secrets; only the resolver Job writes and reads key bytes. Controller Secret reads and
watches request metadata only. Kubernetes RBAC cannot distinguish a metadata request
from a data request, so this process boundary is enforced by code and tests, not an
independent RBAC denial on the controller.

Each resolver receives only `get` on its exact Secret and `get/patch` on its exact report
ConfigMap. Generated-key resolvers additionally receive `patch` on that Secret. Inputs
must be immutable before inspection completes. Reports contain validated public values
and input UIDs. Jobs are bounded to 120 seconds with three retries; they can safely repeat
inspection of the same immutable key. Transaction workers instead bind to one Pod/process;
their loss fails the experiment. A terminal resolver Job failure reports
`Resolved=False / ResolverFailed` on its
identity declaration; the controller does not replace or poll the failed Job. Inspect the
Job and replace the identity declaration to retry. A valid public report takes precedence
over Job failure. Resolver objects remain visible until their owning identity is deleted.

Job and report ConfigMap informers select
`app.kubernetes.io/managed-by=stacks-network-operator`. Ownership still requires exact owner
UIDs. User declarations and metadata-only Secret notifications are not filtered by that
label. ServiceAccount reads bypass the cache. Events create/patch permissions support
manager leader election.

Reusable declarations and their default accounts have independent owners. Inline default
accounts belong to their generated participant. Deleting a root deletes its participants;
Kubernetes garbage collection removes their descendants; actor PVC retention follows the
declared storage policy. Shared genesis and Bitcoin records remain finalized until their
participant consumers are gone. Primary-resource patch permissions on these three kinds
allow the controller to release their retention finalizers. No finalizer blocks
reusable-definition deletion on behalf of a network.

## Values

| Value | Default | Meaning |
| --- | --- | --- |
| `image.repository` | `stacks-network-operator` | Controller and resolver image |
| `image.tag` | `dev` | Explicit locally built revision |
| `image.pullPolicy` | `IfNotPresent` | Controller image pull policy |
| `resources` | 100m CPU / 128Mi request, 512Mi memory limit | Controller resource budget |
| `bitcoinActions.generationEnabled` | `false` | Enable finite generation consumption in existing Bitcoin control workers |
| `bitcoinActions.reorganizationEnabled` | `false` | Enable bounded local reorganization consumption in existing Bitcoin control workers |

Resolver Jobs request 10m CPU and 32Mi memory, with a 128Mi memory limit. They use
non-root containers, a read-only filesystem and no Linux capabilities. No Node runtime
is included in the network image.

```bash
kubectl --context "$STACKS_CONTEXT" -n lab-30 delete stacksnetwork network --wait=true
kubectl --context "$STACKS_CONTEXT" delete namespace lab-30 --wait=true
helm uninstall network --kube-context "$STACKS_CONTEXT" -n stacks-network-system
```

Helm retains CRDs. Remove them only when deliberately discarding all network data in
the selected cluster. Namespace cleanup, not Helm uninstall alone, removes account keys.

Finite action lifecycle controllers install separately through
[stacks-action-operator](../stacks-action-operator/README.md). The matching network
option grants only public action reads to each existing Bitcoin control worker;
requests share its retained execution reservation with baseline generation.
