# Custom Stacks actor images in local kind

Users and agents build actor images outside the operators, then select them in
`StacksNetwork`. This supports instrumented builds and mixed-version networks.
The local-cluster helpers manage cluster lifecycle; they do not check out source,
build actor images or coordinate upgrades.

## Select the cluster

Reuse an explicitly selected cluster. From the stacks-k8s repository root,
these defaults select the existing cluster managed by the local helpers:

```bash
export STACKS_KIND_CLUSTER="${STACKS_KIND_CLUSTER:-stacks-k8s}"
export STACKS_KUBECONFIG="${STACKS_KUBECONFIG:-$PWD/tools/local-cluster/kubeconfig}"
export STACKS_CONTEXT="${STACKS_CONTEXT:-kind-$STACKS_KIND_CLUSTER}"
```

For another kind cluster, set all three variables to that cluster's name,
kubeconfig and context. Reuse these values in the provisioning guide. Each
independent experiment requires a fresh namespace and network identity.

## Build a selected revision

Use a separate worktree for each branch, revision or local instrumentation
change. These commands assume a local `stacks-core` repository and an unused
worktree path:

```bash
STACKS_SOURCE=/absolute/path/to/stacks-core
STACKS_REVISION=your-branch-or-commit
STACKS_BUILD=/tmp/stacks-core-build

git -C "$STACKS_SOURCE" worktree add --detach "$STACKS_BUILD" "$STACKS_REVISION"
```

Make any source changes in that worktree before building. For revisions with
the current root Dockerfile, which packages both `stacks-node` and
`stacks-signer`, build and load the image as follows. The platform must match the
kind nodes; the local qualification environment uses Linux ARM64.

```bash
STACKS_COMMIT=$(git -C "$STACKS_BUILD" rev-parse HEAD)
STACKS_BUILD_ID="${STACKS_COMMIT}-instrumented-01"
export STACKS_IMAGE="stacks-core:$STACKS_BUILD_ID"

docker build --platform linux/arm64 \
  --build-arg "STACKS_NODE_VERSION=local-$STACKS_BUILD_ID" \
  --build-arg "GIT_BRANCH=$STACKS_REVISION" \
  --build-arg "GIT_COMMIT=$STACKS_COMMIT" \
  --label "org.opencontainers.image.revision=$STACKS_COMMIT" \
  --tag "$STACKS_IMAGE" "$STACKS_BUILD"
kind load docker-image "$STACKS_IMAGE" --name "$STACKS_KIND_CLUSTER"
```

If kind reports missing content while importing an image from Docker's
multi-platform store, export just the node platform and load that archive:

```bash
docker image save --platform linux/arm64 -o /tmp/stacks-actor-image.tar "$STACKS_IMAGE"
kind load image-archive /tmp/stacks-actor-image.tar --name "$STACKS_KIND_CLUSTER"
```

The worktree selects the actual source revision. An image label or build argument
only records metadata; it does not perform a checkout. Older revisions may need
a different build recipe or toolchain. Keep the selected revision's build
requirements and the node architecture explicit. The build arguments populate
binary version metadata; `GIT_BRANCH` records the selected source ref for this
detached worktree.

The inspected root Dockerfile enables `monitoring_prom,slog_json` and uses
`--release`. This builds a new artifact; it does not reproduce the
[qualified `a12-normal` image](stacks-qualification.md#images-and-inputs), which
used `release-lite` and a modified checkout. Source changes, build inputs and
image identity must be qualified separately.

Use a new tag for each distinct build. Record the resolved commit, local patch
including any new source files, build options/toolchain, and image content ID.
A commit label alone does not identify a modified checkout. For repeatable builds,
pin builder and runtime base images rather than relying on moving tags.

```bash
docker image inspect "$STACKS_IMAGE" --format '{{.Id}}'
```

Loading imports the image into the selected kind cluster; no registry is
required. `IfNotPresent` uses the loaded image and is already the network default.
Avoid reusing tags or selecting `Always` for images available only locally. See
[kind's image-loading guide](https://kind.sigs.k8s.io/docs/user/quick-start/#loading-an-image-into-your-cluster).

## Select images in a network

For initial provisioning, pass `--stacks-image="$STACKS_IMAGE"` to the Go
`stacks-environment` command in the [production guide](stacks-production.md#provision-and-bootstrap).
The image supplies both the initial node and signer defaults.

The parent also supports these fields:

| Field | Scope |
| --- | --- |
| `spec.defaults.stacksNodeImage` | Default image for Stacks nodes. |
| `spec.defaults.stacksSignerImage` | Default image for signers. |
| `spec.stacksNodes[*].image` | Override for one named node. |
| `spec.signers[*].image` | Override for one named signer. |
| `spec.defaults.imagePullPolicy` | Shared actor pull policy; defaults to `IfNotPresent`. |

For a fresh mixed-version network, edit the complete generated document before
applying it, retaining each actor's other fields and the managed capability
references. Controllers compile and admit the declared topology.

For an existing network, edit the parent with the selected kubeconfig/context,
from the stacks-k8s repository root:

```bash
STACKS_NAMESPACE=my-network
STACKS_NETWORK=my-network
kubectl --kubeconfig "$STACKS_KUBECONFIG" \
  --context "$STACKS_CONTEXT" --namespace "$STACKS_NAMESPACE" \
  edit stacksnetwork "$STACKS_NETWORK"
```

Set the selected actor's `image` to the loaded tag. The aggregate controller
updates its owned actor leaf; that leaf's controller updates its StatefulSet.
Direct edits to an owned leaf are reconciled back to the parent's declaration.

A direct Pod image edit is not automatically reversed by these controllers.
When its image differs from the declared image, the leaf reports `Progressing`
without ready identity. Relevant production capability admission then fails
closed. Change the parent's image declaration to perform supported updates;
Pod edits are not a substitute for that path.

## Mixed-version upgrades and compatibility

Load all required images first. Update one actor image at a time, observe its
replacement Pod and protocol progress, then update the next actor. Changing a
shared default can update several actors concurrently; there is no network-wide
upgrade sequencer. The external user or agent decides ordering and acceptance
criteria. Kubernetes readiness alone does not establish protocol recovery.

Each actor uses its own StatefulSet. With PVC-backed storage, a Pod replacement
retains chain data; explicitly disabled storage uses `emptyDir` and loses it on
replacement. Binary/database compatibility still determines whether a revision
can reopen existing data. Selecting the old image is not a database rollback.

The image must provide the relevant executable on `PATH`: nodes run
`stacks-node start --config ...`, and signers run `stacks-signer run --config ...`.
It must support the selected configuration, genesis epochs, peer protocol and
any RPC endpoints required by enabled production capabilities. Arbitrary
historical or modified revisions are not automatically compatible or qualified.

An image change does not require changing genesis. A changed genesis requires a
fresh network and chain data. Managed initialization requires compatible epoch,
contract and receipt capabilities; see [configuration and genesis](configuration.md).
