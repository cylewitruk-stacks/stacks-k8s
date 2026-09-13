# Participant composition

This contract defines selection and compilation;
[lifecycle](lifecycle.md) defines execution and destruction.

## One list, reference or inline

StacksNetwork.spec.participants is a Kubernetes map-list keyed by name. Each name
identifies one participant in this network, independently of its reusable definition.
Kinds are a closed union; arbitrary Kubernetes objects are not accepted.

```yaml
spec:
  participants:
    - name: miner-a
      kind: StacksNode
      definition:
        ref: {name: standard-miner}
      overrides:
        stacksNode:
          bitcoinNodeRef: {name: bitcoin-a}
          image: local/stacks:hypothesis-42
      control:
        suspended: false
    - name: bitcoin-a
      kind: BitcoinNode
      definition:
        inline:
          bitcoinNode:
            image: bitcoin/bitcoin:31.1
            peers: {discovery: Network}
```

Exactly one definition.ref or definition.inline is required. The outer kind determines
the referenced CR kind/group and the single permitted inline/override branch:

| kind | API group | Inline / override branch |
| --- | --- | --- |
| BitcoinNode | bitcoin.stacks.org | bitcoinNode |
| StacksNode | stacks.stacks.org | stacksNode |
| StacksSigner | stacks.stacks.org | stacksSigner |
| StacksStacker | stacks.stacks.org | stacksStacker |
| StacksFaucet | stacks.stacks.org | stacksFaucet |
| StacksContractSet | stacks.stacks.org | stacksContractSet |
| StacksTransactionProduction | stacks.stacks.org | stacksTransactionProduction |
| BitcoinBlockProduction | bitcoin.stacks.org | bitcoinBlockProduction |

All use the release's v1alpha2 schema. Inline branches contain the corresponding
reusable CR's spec, without apiVersion, metadata or status. Override branches contain
an optional subset of those same fields. Structural schemas declare every branch;
CEL requires the matching kind, exactly one source, and no other branch. Controls
are separate from definition specs. Preserve omission in reusable definitions, inline
branches and overrides: no API-server defaults on fields that network defaults can
supply, and no defaults on override fields. The defaults described in this proposal
are resolver/compiler fallbacks, not CRD default markers. After composing explicit
inputs, fill only still-absent values from the versioned profile, then validate the
complete effective configuration before creating protocol workloads.

There are at most 100 participants of each actor kind, 100 stackers, and one of each
faucet, traffic, contract-set and Bitcoin-production kind. The initial supported
profile requires all four maintenance kinds except the optional faucet. Counts apply
to entries, including inline entries, not to reusable CRs in the namespace.

## Reference meaning and overrides

| Reference | Resolves to |
| --- | --- |
| definition.ref | Same-namespace reusable CR of the entry's kind. |
| bitcoinNodeRef, nodeRef, signerRef, targetNodeRef, peer nodeRefs, serviceRefs | Participant name in this network, with the field's required kind. |
| Production targets and initialization target | BitcoinNode participant names. |
| accountRef and role-specific account refs | Same-namespace StacksAccount. |
| walletRefs / bitcoinWalletRef / payoutWalletRef | Same-namespace BitcoinWallet. |
| epochScheduleRef / scheduleRef | Same-namespace immutable schedule CR. |
| Faucet requests, Bitcoin actions, schedule overrides | Explicit network UID plus participant name; admission pins the generated participant UID. |

A definition can contain network-relative wiring names; it becomes usable when a
network supplies those names or overrides that wiring. Missing/wrong-kind participants
report UnselectedDependency; no dependency is instantiated implicitly. Unreferenced
definitions are parked and have no runtime. Account/wallet resolution is independent
of network membership. Sharing accounts or instantiating a definition more than once
is permitted; no key lease or role exclusivity is introduced.

Precedence is **network defaults → definition → participant overrides**; profile
fallbacks fill only remaining holes. Merge
objects recursively, replace lists atomically, and preserve explicit false/zero/empty
values when valid; null is not a deletion operator. For mutually exclusive fields,
selecting one branch clears the inherited alternative before validation. For example,
specifying schedule replaces an inherited scheduleRef. Overrides never mutate a
reusable CR. Rendered TOML config.overrides has its own protected-path rules in
[resources](resources.md#configuration-escape-hatch); it is not a Kubernetes patch.

Both referenced definition updates and entry overrides are live candidate inputs.
Apply complete valid policies, never a mixture of accepted and deferred fields.
Edits conflicting with [unfinished bootstrap requirements](lifecycle.md#bootstrap-policy-updates)
wait without replacing the admitted policy.
An entry's kind is immutable while the entry exists; a different kind requires a new
participant name. Source identity is pinned at admission; changing it afterward also
requires a new participant. Image/amount edits follow the kind's supported update rules.
After binding, source UID and transitive credential/ingress identities are pinned.
An identical same-name replacement definition is still a different UID.

## Stable participant identity

The network creates one generated StacksNetworkParticipant per selected entry.
It owns the instance; the instance owns workload/config roots. Reusable definitions
remain independent. Runtime relationships and status use participant UID, so two
entries using one definition have distinct Pods, storage, admission and observations.
They may intentionally share its keys. No per-network runtime status is written to
the reusable definition.

See [StacksNetworkParticipant](resources.md#stacksnetworkparticipant) for generated
fields, controller ownership and the user-facing update boundary. Users edit the
network entry; direct edits to generated configuration are not another authority.

## Worked reuse and defaulting example

This excerpt uses the baseline's btc-01 participant. It is separate from the counted
30-actor example and is not a complete network manifest. The reusable node omits
storage, so it does not acquire a schema-inserted 2Gi value before composition.

```yaml
apiVersion: stacks.stacks.org/v1alpha2
kind: StacksAccount
metadata: {name: reuse-key-a, namespace: lab-30}
spec: {key: {generate: true}}
---
apiVersion: stacks.stacks.org/v1alpha2
kind: StacksAccount
metadata: {name: reuse-key-b, namespace: lab-30}
spec: {key: {generate: true}}
---
apiVersion: stacks.stacks.org/v1alpha2
kind: StacksNode
metadata: {name: shared-follower, namespace: lab-30}
spec:
  bitcoinNodeRef: {name: btc-01}
  identityAccountRef: {name: reuse-key-a}
  peers: {discovery: Network}
```

Compose the following into a complete root manifest **before creating that root**;
its defaults are immutable afterward. Preserve its other required participants.
Do not apply this fragment over a running network as a replacement list.

```yaml
spec:
  defaults:
    storage: {size: 5Gi}
  participants:
    - name: reused-a
      kind: StacksNode
      definition: {ref: {name: shared-follower}}
    - name: reused-b
      kind: StacksNode
      definition: {ref: {name: shared-follower}}
      overrides:
        stacksNode:
          identityAccountRef: {name: reuse-key-b}
          storage: {size: 8Gi, retainOnDelete: true}
```

| Resolved field | reused-a | reused-b |
| --- | --- | --- |
| Reusable source | shared-follower, same source UID | shared-follower, same source UID |
| Participant identity | Own generated UID | Different generated UID |
| Identity account | reuse-key-a from definition | reuse-key-b from override |
| Storage size | 5Gi from root, not the 2Gi profile fallback | 8Gi from override |
| retainOnDelete | false profile fallback | Explicit true override |
| Runtime / volume | Own workload and PVC | Separate workload and PVC |

Neither entry changes the reusable definition. Both need the same verified network
genesis, but have independent runtime status, controls and observations. An inline
stacksNode branch omitting storage inherits root 5Gi by the same rule. With root
storage also omitted, the remaining missing size resolves to profile fallback 2Gi.
