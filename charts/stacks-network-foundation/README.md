# Stacks network foundation

This preview installs the composable `v1alpha2` APIs and a Go controller that resolves
participants, generates or imports account identities through scoped Jobs, and captures
immutable genesis. **It does not start Bitcoin, Stacks, consensus signers, or protocol
maintenance workers.** A captured network remains `Initializing`, with `Running=False`
and `Operational=False`.

See the [foundation guide](../../docs/network-operator/public-api-foundation.md) for
supported behavior and verification boundaries.

## Installation boundary

The foundation and existing `v1alpha1` network charts contain incompatible schemas for
some of the same cluster-scoped CRDs. Install this preview in a cluster without the old
network CRDs. Namespaces do not isolate CRD versions. There is no conversion or data
migration, and this chart never removes old resources automatically. A template guard
checks every bundled CRD name and rejects incompatible versions when Helm queries the API
server, including partial old installations. Offline `helm template` cannot inspect existing
CRDs.

Kubernetes 1.37 is the foundation chart's tested minimum. The existing operator chart's
support policy is unchanged. Helm does not upgrade CRDs from `crds/`; review and apply
updated foundation CRDs explicitly when testing a new schema revision.

```bash
docker build -f operators/network/Dockerfile.foundation -t stacks-network-foundation:dev .
kind load docker-image stacks-network-foundation:dev --name "$KIND_CLUSTER"
helm upgrade --install foundation charts/stacks-network-foundation \
  --kube-context "$STACKS_CONTEXT" --namespace foundation-system --create-namespace
kubectl --context "$STACKS_CONTEXT" apply -f docs/design/public-api/examples/30-actors.yaml
kubectl --context "$STACKS_CONTEXT" -n lab-30 get stacksnetwork network -o yaml
```

The example starts with `operation: Paused`: account resolution and participant
allocation proceed, but genesis is not created. Set `operation: Running` to capture it.

## Permissions and ownership

The controller watches public declarations and writes compiled participants, genesis,
status, resolver Jobs, and their scoped ServiceAccounts/Roles. It creates empty generated
Secrets; only the resolver Job writes and reads key bytes. Controller Secret reads and
watches request metadata only. Kubernetes RBAC cannot distinguish a metadata request
from a data request, so this process boundary is enforced by code and tests, not an
independent RBAC denial on the controller.

Each resolver receives only `get` on its exact Secret and `get/patch` on its exact report
ConfigMap. Generated-key resolvers additionally receive `patch` on that Secret. Inputs
must be immutable before inspection completes. Reports contain validated public values
and input UIDs. Jobs are bounded to 120 seconds with three retries; they can safely repeat
inspection of the same immutable key. This is distinct from future transaction workers'
crash contract. A terminal Job failure reports `Resolved=False / ResolverFailed` on its
identity declaration; the controller does not replace or poll the failed Job. Inspect the
Job and replace the identity declaration to retry. A valid public report takes precedence
over Job failure. Resolver objects remain visible until their owning identity is deleted.

Job and report ConfigMap informers select
`app.kubernetes.io/managed-by=stacks-network-foundation`. Ownership still requires exact owner
UIDs. User declarations
and metadata-only Secret notifications are not filtered by that label. ServiceAccounts use
direct reads and need no list/watch grants. Events create/patch permissions support manager
leader election.

Reusable declarations and their default accounts have independent owners. Inline default
accounts belong to their generated participant. Deleting a root deletes its participants;
Kubernetes garbage collection removes their descendants and root-owned genesis. No actor
PVCs exist in this delivery. No finalizer blocks reusable-definition deletion on behalf
of a network.

## Values

| Value | Default | Meaning |
| --- | --- | --- |
| `image.repository` | `stacks-network-foundation` | Controller and resolver image |
| `image.tag` | `dev` | Explicit locally built revision |
| `image.pullPolicy` | `IfNotPresent` | Controller image pull policy |
| `resources` | 100m CPU / 128Mi request, 512Mi memory limit | Controller resource budget |

Resolver Jobs request 10m CPU and 32Mi memory, with a 128Mi memory limit. They use
non-root containers, a read-only filesystem and no Linux capabilities. No Node runtime
is included in the foundation image.

```bash
kubectl --context "$STACKS_CONTEXT" -n lab-30 delete stacksnetwork network --wait=true
kubectl --context "$STACKS_CONTEXT" delete namespace lab-30 --wait=true
helm uninstall foundation --kube-context "$STACKS_CONTEXT" -n foundation-system
```

Helm retains CRDs. Remove them only when deliberately discarding all foundation data in
the selected cluster. Namespace cleanup, not Helm uninstall alone, removes account keys.
