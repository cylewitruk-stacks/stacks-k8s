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

Initial qualification should cover:

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
    stacks.org/network: mixed-network
    stacks.org/action-id: follower-to-bitcoin-delay
  annotations:
    stacks.org/requested-by: external-agent
spec:
  action: delay
  mode: all
  selector:
    namespaces: [stacks-regtest]
    labelSelectors:
      network.stacks.org/network: mixed-network
      network.stacks.org/actor: follower-a
  direction: both
  target:
    mode: all
    selector:
      namespaces: [stacks-regtest]
      labelSelectors:
        network.stacks.org/network: mixed-network
        network.stacks.org/actor: bitcoin-c
  delay:
    latency: 750ms
    jitter: 100ms
    correlation: "25"
  duration: 30s
```

The exact correlation-label prefix is open. Actor selection uses the network
operator's existing labels; agents should not depend on Pod names.

## Targeting contract

- Select one namespace and one `StacksNetwork` label.
- Prefer logical actor labels over role selectors.
- Record an agent-chosen action ID that is unique within the observation
  retention window.
- Use Chaos Mesh `target` selection for the remote side of network faults.
- Do not use raw external IP targets in the supported profile initially.
- Observe the exact selected Pods and UIDs from Chaos Mesh status and
  Kubernetes audit/watch data.

Chaos Mesh resources may select several Pods because one native fault is still
one independently observable action. Cross-kind or ordered behavior requires
separate resources created by the agent.

## Lifecycle and mutability

The upstream resource controller owns injection and recovery. In the supported
profile agents may create, inspect, and delete a resource. Admission makes its
spec immutable even where upstream permits updates; create a new named object
for a materially different fault.

`duration` is required in the supported profile when the upstream kind offers
it. An admission policy caps duration and target scope. Deletion remains the
manual cancellation path.

The observability operator records requested spec, API admission result,
resolved Pods when available, conditions, injection/recovery timestamps, and
capture gaps. It does not decide that the intended network effect occurred
solely because Chaos Mesh reports completion.

## Safety and RBAC

Use upstream namespace filtering and Kubernetes RBAC:

- install Chaos Mesh with experiments restricted to explicit namespaces;
- grant the agent only the qualified Chaos Mesh kinds;
- deny `Schedule` and `Workflow` creation in the supported agent Role;
- deny unrestricted namespaces and unenrolled actor selectors;
- cap duration, modes, percentages, and dangerous parameters with
  ValidatingAdmissionPolicy where CEL can express the contract;
- use a small fail-closed validating webhook only for enrolled-target checks
  that static admission cannot express; and
- keep the Chaos Daemon's privileged permissions isolated from stacks-k8s
  operator ServiceAccounts.

Native Chaos selectors bind logical actor labels, not immutable Pod UIDs. At
resource admission, the webhook resolves the selector against a complete
admitted `StacksNetwork` inventory and validates enrollment of those logical
actors. Kubernetes/Chaos may retarget a replacement Pod with the same labels
while a fault is active; this limitation cannot be eliminated without wrapping
the upstream mechanism.

Therefore the exact-identity guarantee is deliberately narrower for native
faults: their spec and admitted logical target set are pinned, while passive
observation records selected Pod UIDs and any replacement as
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
- Negative-test cross-namespace and unenrolled selectors.
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
- Admission/RBAC prevent unsupported orchestration APIs and out-of-scope
  targets.
- Observability correlates request, logical target, selected Pod identities,
  divergence, lifecycle, telemetry, and cleanup without mutating the fault.
- The compatibility matrix is backed by real-cluster evidence.
- Removing Chaos Mesh leaves both existing operators functional.

## Open decisions

1. Stable correlation label/annotation keys.
2. Availability/failure policy for the required enrolled-target webhook.
3. Initial platform matrix and qualified Chaos Mesh version.
4. Which native kinds are safe on arm64 and each supported runtime/CNI.

## References

- [Chaos Mesh controller architecture](https://github.com/chaos-mesh/chaos-mesh/blob/master/controllers/README.md)
- [Chaos Mesh fault types](https://chaos-mesh.org/docs/next/basic-features/)
- [Kubernetes custom resources](https://kubernetes.io/docs/concepts/extend-kubernetes/api-extension/custom-resources/)
