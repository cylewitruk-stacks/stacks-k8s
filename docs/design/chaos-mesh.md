# Native Chaos Mesh integration

## Purpose

Agents use native Chaos Mesh CRDs directly for generic infrastructure faults.
`stacks-k8s` supplies targeting conventions, RBAC/policy, packaging guidance,
observation correlation, and examples. It does not wrap native faults in a
second scenario or campaign API.

Chaos Mesh's controller architecture is the implementation inspiration: small
level-based controllers, one logical writer per field, explicit shared
lifecycle ownership, and independently understandable mechanisms. Its
`Schedule` and `Workflow` orchestration APIs are intentionally outside this
project's supported agent workflow.

## Supported native resources

The [implemented initial profile](../chaos/operations.md) qualifies directed
`NetworkChaos` delay and bidirectional actor partitions. The following broader scope remains planned:

| Chaos Mesh kind | Use |
| --- | --- |
| `PodChaos` | Pod kill/failure and container kill. |
| `NetworkChaos` | Delay, loss, duplication, corruption, bandwidth, and partition. |
| `DNSChaos` | DNS error and response manipulation. |
| `IOChaos` | Filesystem latency, failure, and bounded attribute/mistake injection. |
| `TimeChaos` | Process clock offset where platform support is proven. |
| `StressChaos` | CPU and memory pressure. |

`HTTPChaos`, `KernelChaos`, and provider-specific faults remain unqualified
until a concrete Stacks use case and safe target contract exist.

## Direct resource example

```yaml
apiVersion: chaos-mesh.org/v1alpha1
kind: NetworkChaos
metadata:
  name: follower-to-bitcoin-delay
  namespace: stacks-regtest
  labels:
    network.stacks.org/network: mixed-network
    actions.stacks.org/correlation-id: investigation-42
spec:
  action: delay
  mode: one
  selector:
    namespaces: [stacks-regtest]
    labelSelectors:
      network.stacks.org/network: mixed-network
      network.stacks.org/actor: follower-a
  direction: to
  target:
    mode: one
    selector:
      namespaces: [stacks-regtest]
      labelSelectors:
        network.stacks.org/network: mixed-network
        network.stacks.org/actor: bitcoin-c
  delay:
    latency: 750ms
    jitter: 0ms
    correlation: "0"
  duration: 30s
```

The correlation label follows the normative
[atomic-action contract](actions.md). Actor selection uses the network
operator's existing labels; agents should not depend on Pod names.

## Targeting contract

- Select one namespace and one `StacksNetwork` label.
- Prefer logical actor labels over role selectors.
- Record an agent-chosen correlation value. It is a search hint; resource UID
  remains the action identity.
- Use Chaos Mesh `target` selection for the remote side of network faults.
- Do not use raw external IP targets in the supported profile initially.
- Observe the exact selected Pods and UIDs from Chaos Mesh status and
  Kubernetes audit/watch data.

Chaos Mesh resources may select several Pods because one native fault is still
one independently observable action. Cross-kind or ordered behavior requires
separate resources created by the agent.

## Protocol faults and production control

Qualification distinguishes Bitcoin P2P traffic, Stacks-to-Bitcoin RPC,
and producer-to-Bitcoin RPC. The profile delays or partitions actor-to-actor
traffic without port filtering. [Delay](../chaos/qualification.md) and
[partition evidence](../chaos/partition-qualification.md) qualify Bitcoin peer
separation and miner-to-Bitcoin disruption with independent producer progress,
plus separate administrator-only control-path loss. Native cleanup does not
promise Stacks progress: a repeated expiry case stalled at a reward-cycle
transition and remains an open fixture/protocol investigation. A partial
protocol partition should preserve the
central producer's management path when continued block production is part of
the declared experiment. Source/target selection and direction matter: Stacks
clients and the producer can use the same Bitcoin RPC port. A separate Service
name alone does not isolate traffic reaching the same Pod.

Qualification must verify both the intended disruption and continued control
access on the selected CNI/runtime. Pod-wide isolation, process failure, or
node loss may remove that path; the centralized design makes no continuity
promise then. Record control unavailability and ambiguous RPC effects rather
than inferring that the Bitcoin process stopped or no block was produced.

A disconnected real miner may continue working on locally available work,
depending on its implementation and dependencies. The centralized regtest
profile models protocol partitions with available management access; autonomous
node-local production is deferred and cannot be enabled as an implicit fallback.
See [Steady-state operation](steady-state-operation.md).

## Lifecycle and mutability

The upstream resource controller owns injection and recovery. In the supported
profile agents may create, inspect, and delete a resource. Admission makes its
complete spec immutable from creation, even where upstream permits updates;
create a new named object for a materially different fault.

`duration` is required in the supported profile when the upstream kind offers
it. An admission policy caps duration and target scope. Deletion remains the
manual cancellation path.

The planned observability integration records requested spec, API admission result,
resolved Pods when available, conditions, injection/recovery timestamps, and
capture gaps. It does not decide that the intended network effect occurred
solely because Chaos Mesh reports completion.

## Safety and RBAC

Use upstream namespace filtering and Kubernetes RBAC:

- install Chaos Mesh with experiments restricted to explicit namespaces;
- grant the agent only the qualified Chaos Mesh kinds;
- deny `Schedule` and `Workflow` creation in the supported agent Role;
- deny unrestricted namespaces and selectors missing the required network and
  logical-actor label shape;
- cap duration, modes, percentages, and dangerous parameters with
  ValidatingAdmissionPolicy where CEL can express the contract;
- reject `spec.remoteCluster` wherever an upstream kind exposes it;
- defer dynamic enrolled-target admission rather than requiring a webhook in
  v1; and
- keep the Chaos Daemon's privileged permissions isolated from stacks-k8s
  operator ServiceAccounts.

Native Chaos selectors bind required network and logical-actor labels, not
immutable Pod UIDs. Static admission constrains the namespace and selector
shape but does not claim to resolve the labels against a live
`StacksNetwork` inventory. A selector that currently matches nothing is a
valid no-op resource. Kubernetes/Chaos may retarget a replacement Pod with the
same labels while a fault is active; this limitation cannot be eliminated
without wrapping the upstream mechanism.

Therefore the exact-identity guarantee is deliberately narrower for native
faults: their immutable spec and requested logical labels are pinned, while
planned passive observation records selected Pod UIDs and any replacement as
`TargetIdentityDiverged`. Evidence after divergence is not attributed to the
original Pod. Agents requiring exact Pod identity must cancel on divergence or
use a purpose-built action whose mechanism supports UID pinning.

## Platform qualification

Support is a matrix, not a global claim. Record per kind:

- Kubernetes version and distribution;
- node OS, architecture, kernel, container runtime, and CNI;
- Chaos Mesh version;
- storage driver and filesystem for `IOChaos`;
- selected clock IDs for `TimeChaos`; and
- observed injection and recovery behavior.

Unsupported native IO/time/stress mechanisms fail admission in the supported
profile or are documented unavailable. Protocol-specific fallbacks require
their own action CRD and must not impersonate an upstream kind.

## Observability

Collection correlates:

- the complete submitted Chaos Mesh object and UID;
- requesting Kubernetes audit identity;
- selected actor labels and observed Pod UIDs;
- Chaos Mesh conditions and events;
- daemon/controller logs relevant to the action;
- query-time before/during/after views over continuously collected protocol
  and resource telemetry; and
- deletion and recovery observations.

The observer records facts and gaps. The external agent determines whether
the fault contributed to an issue.

## Packaging

Chaos Mesh remains an optional external chart dependency, version-pinned in a
tested local installation profile but not vendored or forked. The
`stacks-network-operator` chart must remain usable without it. A top-level
development bundle may install compatible versions of network, observability,
action, Chaos Mesh, and telemetry charts without making their releases
inseparable.

## Tests

- Render and validate every qualified native example against its installed
  CRD schema.
- Verify agent RBAC permits qualified faults and denies Workflow/Schedule.
- Negative-test cross-namespace, malformed logical selectors, and every
  supported kind's remote-cluster escape field.
- Verify each protocol partition's traffic direction and preserved management
  path, then separately test control-path loss and honest uncertainty.
- Live-test injection, cancellation, duration expiry, recovery, controller
  restart, and telemetry correlation per platform matrix.
- Prove observability records a failed admission and a capture gap.
- Run without Chaos Mesh and prove topology/identity observation still works.

## Alternatives

| Alternative | Disposition |
| --- | --- |
| Custom wrapper CRD for every native fault | Rejected; duplicates a functioning Kubernetes API. |
| Chaos Mesh Workflow or Schedule | Unsupported; external agent owns sequencing and timing. |
| Fork Chaos Mesh for Stacks actions | Deferred; separate protocol controllers avoid fork maintenance and privilege coupling. |
| Give the agent wildcard Chaos Mesh permissions | Rejected; only qualified kinds and namespaces are granted. |

## Definition of done

- Agents can submit each qualified native kind directly using logical actor
  selectors.
- Admission/RBAC prevent unsupported orchestration APIs, remote clusters, and
  out-of-scope selector shapes.
- Observability correlates request, logical target, selected Pod identities,
  divergence, lifecycle, telemetry, and cleanup without mutating the fault.
- The compatibility matrix is backed by real-cluster evidence.
- Removing Chaos Mesh leaves both existing operators functional.

## Open decisions

1. Availability/failure policy for dynamic enrolled-target admission.
2. Broader traffic and platform qualification beyond the initial delay/partition matrix.
3. Which additional native kinds are safe on arm64 and each supported runtime/CNI.

## References

- [Chaos Mesh controller architecture](https://github.com/chaos-mesh/chaos-mesh/blob/master/controllers/README.md)
- [Chaos Mesh fault types](https://chaos-mesh.org/docs/next/basic-features/)
- [Kubernetes custom resources](https://kubernetes.io/docs/concepts/extend-kubernetes/api-extension/custom-resources/)
