# Configuration and genesis

A network selects reusable definitions or inline declarations through its
heterogeneous `spec.participants` list. Configuration precedence is network
defaults, definition values, then typed participant overrides; absent values are
filled from the supported profile. Lists replace inherited lists. See
[composition](../design/public-api/composition.md).

`StacksAccount` and `BitcoinWallet` resolve public identities independently of any
network. The network's genesis allocations reference accounts and amounts; unused
funded accounts are valid experiment inputs. Scoped Jobs generate or inspect keys.
Private material remains in immutable Secrets and is never read by the operator.

Creating the immutable `StacksGenesis` freezes resolved balances, epoch/PoX inputs,
contract sources and initial bootstrap requirements. All generated Stacks node
configuration uses this artifact. Stacker, contract and transaction workers consume
its bindings and their admitted participant policy. A new network requires a new
artifact; changing genesis inputs on a captured network is not supported.

## Actor configuration

Generated Stacks node and signer configuration uses a structured TOML document.
Typed managed values and validated overrides are rendered by scoped resolver Jobs.
Complete immutable configuration Secrets are supported via `config.secretRef`;
they must agree with managed identity, genesis and endpoint requirements unless
the declared profile explicitly permits Unverified operation.

See the [resource configuration contract](../design/public-api/resources.md) for
protected paths, Secret replacement and runtime eligibility. A verified syntax
check does not prove every Core option is supported by an arbitrary custom image.
The actor must start and expose the required native protocol endpoints.

Generated Bitcoin configuration supports scalar values, repeated option lists and
one network-section level, or a full immutable `bitcoin.conf` Secret. Bitcoin
nodes do not own mining cadence; a selected production participant does.

Images and workload resource requests can be overridden per actor. Rollouts replace
Pods while retaining the participant's identity and configured PVCs. Management
workers have a stricter single-process contract; their bound placement cannot be
silently repaired by creating a new worker. See [lifecycle](../design/public-api/lifecycle.md).
