# Mutable topology design

## Purpose

`StacksNetwork` remains the supported aggregate API for declaring a reusable
Stacks regtest topology. It compiles to the smallest actor resources with
materially different executables and lifecycle contracts.

This design extends safe live mutation without turning topology into an action
or upgrade scheduler.

## Non-goals

- Building OCI images from Git.
- Producing Bitcoin blocks or forcing Bitcoin state transitions.
- Registering signers or submitting protocol transactions.
- Sequencing actor upgrades.
- Applying network or process faults.
- Determining whether a protocol outcome is correct.

## Current and recommended resources

| Resource | Status | Owner |
| --- | --- | --- |
| `StacksNetwork` | Implemented; extend | Aggregate topology controller. |
| `BitcoinNode` | Implemented; extend | Bitcoin leaf controller. |
| `StacksNode` | Implemented; extend | Stacks node leaf controller. |
| `StacksSigner` | Implemented; extend | Signer leaf controller. |
| `SbtcSigner` | Open | Add only when its executable and lifecycle differ materially from `StacksSigner`. |

Role variation using the same binary and workload contract remains a field,
not another CRD. New leaf kinds require a different executable, configuration
contract, ports, dependencies, readiness, or persistent-state lifecycle.

## Target topology example

```yaml
apiVersion: network.stacks.org/v1alpha1
kind: StacksNetwork
metadata:
  name: mixed-network
spec:
  defaults:
    bitcoinImage: registry.example/bitcoin@sha256:1111111111111111111111111111111111111111111111111111111111111111
    stacksNodeImage: registry.example/stacks-node@sha256:2222222222222222222222222222222222222222222222222222222222222222
    stacksSignerImage: registry.example/stacks-signer@sha256:3333333333333333333333333333333333333333333333333333333333333333
    workload:
      storage:
        enabled: true
        size: 20Gi
        retainOnDelete: true
  bitcoinNodes:
    - name: bitcoin-a
      role: miner
      config:
        generated:
          profile: bitcoin-regtest/v1
      peerRefs: [bitcoin-b, bitcoin-c]
    - name: bitcoin-b
      role: miner
      image: registry.example/bitcoin@sha256:4444444444444444444444444444444444444444444444444444444444444444
      config:
        generated:
          profile: bitcoin-regtest/v1
      peerRefs: [bitcoin-a, bitcoin-c]
    - name: bitcoin-c
      role: follower
      config:
        generated:
          profile: bitcoin-regtest/v1
      peerRefs: [bitcoin-a, bitcoin-b]
  stacksNodes:
    - name: miner-a
      role: miner
      bitcoinNodeRef: bitcoin-a
      config:
        secretRef:
          name: miner-a-v1
          key: config.toml
    - name: miner-b
      role: miner
      bitcoinNodeRef: bitcoin-b
      image: registry.example/stacks-node@sha256:5555555555555555555555555555555555555555555555555555555555555555
      config:
        secretRef:
          name: miner-b-v2
          key: config.toml
    - name: follower-a
      role: follower
      bitcoinNodeRef: bitcoin-c
      config:
        generated:
          profile: nakamoto-regtest-node/v1
  signers: []
```

Git repository and revision do not appear in this API. The agent prepares or
selects the image externally and patches only its immutable OCI reference and,
when needed, a compatible configuration reference.

## Desired live-update semantics

| Change | Reconciliation |
| --- | --- |
| Add actor | Create leaf, Service, workload, and storage; withhold complete inventory until Ready. |
| Remove actor | Foreground-delete the leaf and owned resources; preserve explicitly retained storage. |
| Image/config/process change | Roll only the affected actor and consumers whose compiled direct dependency changed. |
| Peer/dependency change | Recompile affected leaves; roll only actors whose effective configuration changes. |
| Resource/placement change | Patch the StatefulSet Pod template and wait for its new revision. |
| Suspend actor/network | Scale selected workload to zero and withdraw its admitted identity. |
| Immutable workload-shape change | Report `Degraded`; require a new logical actor name. |

An update is complete only when `status.observedGeneration` matches metadata,
all desired leaves report their current generation, and the aggregate publishes
a complete new inventory. A partial rollout never retains the previous digest
as if it described current state.

## Independent upgrades

The agent performs an independent upgrade by patching one actor template at a
time and observing convergence. The operator provides no `UpgradeCampaign`.

Recommended actor update fields are:

- immutable image reference;
- complete configuration source;
- command/argument/environment escape hatch;
- resources and placement; and
- storage retention policy.

Configuration compatibility varies across historical versions. The API must
continue to accept a complete ConfigMap or Secret-backed file. Semantic patch
fields may be added only for stable cross-version settings; they must never
make the raw configuration escape hatch unavailable.

Generated and inline configuration is content-bound automatically. Today a
ConfigMap without `expectedDigest` is not watched for content-triggered
rollout. Changing that behavior requires a leaf-spec/inventory contract version
and migration note because it changes when actors restart. The recommended new
contract watches referenced ConfigMaps, computes their content digest, and
rolls only affected actors. For Secrets, prefer immutable/versioned Secret
names and patch the reference. An optional declared content digest may enforce
strict byte identity, but it is not required for normal operation.

### Bitcoin RPC credential input

M0.3 defines one narrower exception for generated Bitcoin and Stacks
configuration. Each enrolled namespace has a fixed immutable
`stacks-bitcoin-rpc` Secret with key `bitcoin-rpc.conf`. The topology
declaration adds optional `StacksNetwork.spec.bitcoinRPCAuth` using
`ConfigObjectRef`. Managed action RPC pins that field's name and key, requires
`expectedDigest`, and forbids `mountPath`.

The aggregate compiler copies the same input to
`BitcoinNode.spec.bitcoinRPCAuth` and `StacksNode.spec.bitcoinRPCAuth`.
`bitcoin-regtest/v2` and `nakamoto-regtest-node/v2` consume it through actor
init. Existing v1 profiles remain compatible topology options but cannot
report managed-RPC readiness. Custom configurations remain supported and must
opt into the reserved renderer inputs before becoming action-eligible.

This prerequisite changes the generated-profile enum, API schemas, aggregate
compiler, both workload renderers, and leaf `specDigest`. It adds vectors to
`leaf-spec-v1.json` and `inventory-v1.json` and lands before the first Bitcoin
action controller. Actor init containers verify the mounted bytes before use;
the network controller never reads Secret content.

The inventory's leaf `specDigest` records the reference, expected digest,
template digest, and renderer contract as configuration-input identity. It
does not attest Secret content or rendered TOML. Rotation replaces the
immutable Secret and changes the declared digest, intentionally withdrawing
admitted identity while affected actors roll. The exact contract and operator
trust exceptions are in [Bitcoin lifecycle design](bitcoin-lifecycle.md).

## Persistent state

Persistent storage is the default recommendation for meaningful upgrade and
recovery testing. The design must distinguish:

- process replacement with the same database;
- new actor with an empty database;
- retained database after actor deletion; and
- explicit clone, snapshot, or restore performed by storage tooling.

The operator must not infer database compatibility from an image tag. It rolls
the requested image and reports Kubernetes and process readiness facts. The
agent decides whether the resulting protocol behavior is acceptable.

Storage-class, access-mode, and claim-template changes remain immutable. The
operator must not delete data to satisfy such a patch.

## Direct leaf access

Aggregate-owned leaf resources are implementation state. Recommended Roles:

- `stacks-network-viewer`: read all network and leaf resources;
- `stacks-network-editor`: write `StacksNetwork`, read leaves; and
- operator ServiceAccount: write leaves and owned workloads.

This makes ordinary direct leaf edits fail authorization rather than briefly
apply and later revert. Cluster administrators intentionally retain override
power. A validating webhook is deferred unless accidental administrator edits
prove common enough to justify its availability and certificate cost.

Standalone leaves remain writable for advanced use but never appear in an
aggregate inventory unless owned by that `StacksNetwork` UID.

## Status and observability

The existing status contract remains authoritative:

- aggregate and leaf observed generations;
- desired and ready counts;
- `Progressing`, `Ready`, `Suspended`, or `Degraded` phase;
- complete admitted actor identities; and
- inventory digest only when every identity is current.

The observer should record every topology generation change when its audit
source provides continuity. When a source cannot prove loss absence, it
records the transition actually observed plus the source coverage class; it
must not assert a complete mutation history. The network operator does not
write that history.

## Reconciliation and ownership

- The aggregate controller is the only writer of aggregate-owned leaf specs.
- Each leaf controller is the only writer of its status and workload objects.
- Controllers use owner references, generation checks, and idempotent
  create/update/prune behavior.
- Direct identity reads remain uncached when stale state could admit the wrong
  Pod, image, Service, or StatefulSet.
- No controller waits for or invokes another controller in a fixed order.
  Readiness is expressed through resource status and watched dependencies.

## Tests

- Compile multi-Bitcoin and multi-Stacks-miner topologies.
- Add, remove, suspend, resume, and independently roll each actor kind.
- Verify only dependency-affected actors roll.
- Exercise ConfigMap content updates, versioned general configuration Secrets,
  and fixed-name Bitcoin credential rotation.
- Prove immutable storage changes preserve existing PVCs and become Degraded.
- Prove editor RBAC cannot patch aggregate-owned leaves.
- Run envtest generation/readiness and ownership transitions.
- Qualify persistent-data upgrades with selected real image pairs.

## Alternatives

| Alternative | Disposition |
| --- | --- |
| One StatefulSet per actor directly in `StacksNetwork` controller | Rejected; leaf CRDs provide composable ownership and status boundaries. |
| One CRD per actor role | Rejected unless executable or lifecycle differs materially. |
| In-cluster upgrade campaign | Rejected; the agent sequences independent topology patches. |
| Git source fields in actor specs | Rejected; source execution is outside controller trust. |
| Automatically delete/recreate immutable storage | Rejected as destructive. |

## Definition of done

- Multi-Bitcoin and multi-Stacks-miner examples converge with complete admitted
  identity.
- Independent image/configuration updates preserve requested persistent data.
- ConfigMap, versioned configuration Secret, and fixed-name Bitcoin credential
  changes have documented, tested rollout behavior.
- Aggregate-owned leaf edits are denied by the supported editor Role.
- No topology API performs mining, faults, replay, or upgrade sequencing.
- Real-image qualification states exactly which combinations were exercised.

## Open decisions

1. Whether `SbtcSigner` is required in the first actor-expansion milestone.
2. Initial supported persistent storage classes.
3. Whether automatic ConfigMap hashing belongs in the network operator or a
   reusable configuration controller.
4. Which protocol-ready probes, if any, belong in actor Pod readiness rather
   than passive observation.
