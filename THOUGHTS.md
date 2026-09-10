# My Thoughts

Non-normative brainstorming notes. Some ideas below are superseded or inconsistent;
the [public API proposal](docs/design/public-api/README.md) is the current design reference.

## Network Operator

### CRD's

#### ✨ **`BitcoinWallet`:**

Stacks miners need Bitcoin wallets... so I guess this could be that?

**Spec:**

- Private Key (secret)
- Public Key

#### ✨ **`StacksAccount`:**

Represents a known Stacks account, including its keypair/credentials and address information. Used
by other network-operator resources to reference a specific Stacks account.

Can be provisioned after a `StacksNetwork` has been bootstrapped within a namespace and may be
combined with a `StacksFaucetRequest` to request funding, which enables adding new stacks
miners/signers after network bootstrapping.

- Spec:
  - Name
  - Initial Balance (micro STX)
  - [Optional] Private Key (generated if omitted)
- Resolves to:
  - Public key (property, derived from private key)
  - Private key (secret)

#### ✨ **`StacksFaucet`:** *(singleton)*

Can be provisioned within a namespace without any bootstrapped `StacksNetwork` and will ensure that
a new `StacksAccount` is funded during genesis bootstrap which is dedicated to being a faucet. The
faucet runs a controller which listens for `StacksFaucetRequest` resources and executes
`stx-transfer` transactions from itself to destination `StacksAccount`s.

**Spec:**

- Initial Balance (micro STX)

**Resolves to:**

- A new generated `StacksAccount`

#### ✨ **`StacksFaucetRequest`:**

**Spec:**

- Destination Account (either a `StacksAccount` reference or a concrete Stacks address string
  (`ST...`)).
- Amount (Micro STX)

**Resolves to:**

- The `StacksFaucet` in the namespace makes an RPC call to an available `StacksNode` in the
    namespace with an `stx-transfer` Stacks transaction transferring STX from the faucet to the
    destination account.

#### ✨ **`StacksEpochSchedule`:**

An epoch schedule for a Stacks node, which must be placed in each Stacks node's config file.

**Spec:**

Contains an array of `{ epoch: epoch-decimal, startHeight: bitcoin-start-height-u32 }`, where each
must be monotonically increasing (both fields). E.g.:

```yaml
epochs:
- epoch: 1.0
  start-height: 0
- epoch: 2.0
  start-height: 0
- epoch: 2.05
  start-height: 203
# ...
- epoch: 4.0
  start-height: 262
```

#### ✨ **`StacksNetwork`:**

This resource represents a Stacks network.

**Spec:**

- Genesis Accounts (enumerated list of `StacksAccount`s which exist in the same namespace at
  creation time and represents the `[[ustx_balance]]` config entries in `StacksNode` configuration
  files).
- Epoch Schedule: (either an inlined epoch schedule or a ref to an existing `StacksEpochSchedule` -
  if omitted and exactly one `StacksEpochSchedule` exists in the same namespace upon creation, then
  it automatically links to that schedule).
- Stacks Faucet (ref to a `StacksFaucet` resource. If exactly one automatically exists in the
  namespace then that one is automatically linked, else one is created automatically together with a
  new dedicated `StacksAccount` and default faucet balance).
- Stacks Transaction Production
  - Enabled? (mutable)
  - Cadence (seconds, mutable)
- Bitcoin Block Production
  - Enabled? (mutable)
  - Spec: (either an inlined bitcoin block schedule or a ref to an existing `BitcoinBlockSchedule`).
- Stacks Nodes (list of admitted `StacksNode`s in this namespace)
- Stacks Signers (list of admitted `StacksSigner`s in this namespace)
- Stacks Accounts (list of **all** admitted `StacksAccount`s in this namespace)

**Resolves to:**

Adds watchers for `StacksNode` and `StacksSigner` resources and updates itself with current network
topology.

#### ✨ **`BitcoinNode`:**

**Spec:**

- Wallets: List of `BitcoinWallet` refs (optional)

...

#### ✨ **`BitcoinBlockSchedule`:**

... like our `BitcoinBlockProduction` today, I think ...

#### ✨ **`BitcoinBlockScheduleOverride`:**

**Spec:**

- Expiration: (seconds or # of bitcoin blocks)
- Spec: (either an inline bitcoin block schedule or a ref to an existing `BitcoinBlockSchedule`)

...

#### ✨ **`StacksNode`:**

**Spec:**

- `BitcoinNode` ref
- Miner spec: (optional, required if mining)
  - Stacks account?
  - Bitcoin Wallet (for block commits, reference to `BitcoinWallet`)
  - Mining config (fields for sats/vbyte fee, etc.)
- Advanced config overrides (accept a list of toml keys and content that e.g.
  `yq` can insert or update in place,or we could use a proper toml parser for
  config reconciliation to make sure that we don't muck up the toml file)
- etc...

...

#### ✨ **`StacksSigner`:**

...

#### ✨ **`StacksStacker`:**

...
