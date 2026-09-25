# Examples

Install the owning charts before applying resources. Examples are source-checkout
inputs; actor images must be built/loaded or available from a registry.

- [Network composition](network/README.md) uses the maintained public API example.
- [Actions](actions/) contain bounded Bitcoin requests; fill exact network identity
  and target/payout fields from your running network.
- [Native faults](chaos/) show exact actor selectors and network-attributed
  fault metadata; adapt their scope to the experiment.
- [Observation](observability/) describes read-only identity observation.

Use a fresh namespace for each independent experiment. Examples contain no private
fixture keys; account resolvers generate or import scoped credentials.
