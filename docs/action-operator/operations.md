# Bounded action operations

Install the [action chart](../../charts/stacks-action-operator/README.md) in the
network namespace and enable matching network executor capabilities. Lifecycle
controllers write action status and cleanup finalizers; they do not access Secret
values or send Bitcoin RPCs.

Requests use `actions.stacks.org/v1alpha2`, exact `spec.networkUID` and a logical
Bitcoin-node participant reference. Specs are immutable from creation; pending time
counts against the bounded timeout. Fill these fields in the
[examples](../../examples/actions/) from the selected running network.

Observe `phase` together with admission, receipt and cleanup conditions. Generation
completion reports acknowledged blocks; reorganization completion reports its local
mechanism. Neither is a global consensus/recovery verdict. Cancellation/deletion
cannot retract an already armed RPC or erase an uncertain execution record.

Keep lifecycle controllers and worker read permissions installed until requests
settle or the network is disposed. Disabling an installation flag is not cancellation.
Follow [network cleanup](../network-operator/operations.md#removal-and-cleanup) before
removing the namespace or action release.
