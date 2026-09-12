# Network operations

Install the [network chart](../../charts/stacks-network-operator/README.md) once.
Use a fresh namespace for each independent experiment, load the selected actor
images, and apply reusable inputs together with `StacksNetwork/network`.
The [30-actor example](../design/public-api/examples/30-actors.yaml) starts paused
so images, placement and resolved inputs can be inspected before genesis capture.

## Desired operation

```bash
kubectl -n lab-30 patch stacksnetwork network --type=merge \
  -p '{"spec":{"operation":"Running"}}'
kubectl -n lab-30 get stacksnetwork network -o yaml
kubectl -n lab-30 get stacksnetworkparticipants
```

`Paused` cooperatively stops managed sends while preserving worker processes.
It does not stop actor consensus or networking. Set `Running` to resume.
`Stopped` is terminal for that network: workers drain and actors scale to zero
with confirmed termination. A stopped network cannot be resumed; create a new
root after deletion. Participant-level controls are workload-specific; see
[lifecycle controls](../design/public-api/lifecycle.md).

Reapplying a manifest also reapplies its `operation`; a manifest containing
`Paused` pauses an already running network. Edit the desired declaration rather
than modifying generated Pods, configurations or admitted status directly.

## Readiness

`WorkloadReady` describes Kubernetes workloads. `Initialized` records completion
of frozen protocol gates. `Operational` requires fresh native chain/transaction
progress and required signer participation. A Ready Pod is not proof of consensus
progress. Examine root conditions, participant admission/workload/execution,
Bitcoin records and worker logs when initialization stops advancing.

Stacks management-worker loss fails the experiment. Preserve relevant evidence,
then recreate the network. No replacement worker guesses a nonce or resends an
uncertain transaction. Bitcoin ambiguity closes the affected target; it does not
claim that an interrupted RPC stopped executing.

## Removal and cleanup

Removing a participant entry destroys its runtime; its name cannot be reused
within that root. Collect logs and ephemeral evidence before removal. PVC survival
depends on admitted storage retention; observability is optional.

Remove native faults and wait for their controller cleanup before destroying the
network. Then stop and delete the root before deleting the namespace:

```bash
kubectl -n lab-30 patch stacksnetwork network --type=merge \
  -p '{"spec":{"operation":"Stopped"}}'
kubectl -n lab-30 wait stacksnetwork/network \
  --for=jsonpath='{.status.phase}'=Stopped --timeout=5m
kubectl -n lab-30 delete stacksnetwork network --wait=true --timeout=5m
kubectl -n lab-30 get stacksnetworkparticipants,pods,persistentvolumeclaims
```

Review retained PVCs before deleting the namespace. Namespace deletion also
removes reusable accounts and definitions. Keep the local kind cluster running;
uninstall per-experiment action/observation/profile releases after their resources
have finished cleanup. Do not remove finalizers to manufacture successful shutdown.
