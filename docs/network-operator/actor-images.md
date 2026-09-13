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

Network defaults are immutable once the root is created. Set the default images
before applying a new network, or use mutable per-participant overrides for actor
rollouts:

| Field | Scope |
| --- | --- |
| `spec.defaults.images.bitcoin` | Default Bitcoin image |
| `spec.defaults.images.stacksNode` | Default Stacks node image |
| `spec.defaults.images.stacksSigner` | Default signer image |
| `spec.participants[*].overrides.image` | One actor instance |
| Reusable actor definition `spec.image` | Selected instances using that definition |

The heterogeneous participant's `kind` selects the override schema. Preserve its
name, kind and definition/inline source when changing only the image. Edit
`StacksNetwork/network` in the selected namespace, not generated participant
admission, configuration or Pod fields.

The aggregate admits the new policy; the actor controller updates its StatefulSet.
A direct Pod image edit is not the supported rollout path and invalidates trusted
runtime identity. Inspect the new Pod's image ID, native RPC and protocol progress
before updating another actor. There is no network-wide upgrade sequencer.

## Mixed versions and storage

Load all selected tags into kind first. Give every distinct build a distinct tag;
`IfNotPresent` permits use of locally loaded images. A participant's PVC can preserve
chain data across Pod replacement. `ephemeral: true` uses `emptyDir` and loses that
data. Binary/database compatibility determines whether a revision can reopen it;
selecting an older image is not a database rollback.

Images must provide `stacks-node` or `stacks-signer` on PATH and support the admitted
configuration, epochs, peer protocol and native RPC endpoints. Arbitrary historical
or adversarial builds are not automatically compatible or qualified. A changed
image does not require new genesis; changed genesis inputs do.

Management-worker placement and process identity have stricter controls than actor
rollouts. Do not replace a bound stacker, contract or traffic worker as an image
upgrade experiment; its loss ends the network's managed experiment. See
[lifecycle](../design/public-api/lifecycle.md).
