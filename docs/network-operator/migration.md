# Migration from Hacknet topology

The standalone operator is a new API, not an in-place conversion of
`testing.stacks.org/StacksNetwork`.

| Hacknet concern | Standalone topology |
| ---- | ---- |
| Aggregate actor declaration | `network.stacks.org/StacksNetwork` |
| Bitcoin actor | `BitcoinNode` leaf |
| Stacks node actor | `StacksNode` leaf |
| Stacks signer actor | `StacksSigner` leaf |
| Burnchain clock and mining | External client or testing control plane |
| Signer enrollment | External bootstrap client |
| Faults, upgrades, fuzzing, assertions | Attacknet control plane |
| Trusted runtime identity | Initial `stacks-observability-operator` `NetworkObservation` API |
| Protocol telemetry and evidence | Future observation and evidence layers |

Do not deploy old and new aggregates with the same network and actor names in
one namespace: their API groups differ, but their Services and StatefulSets do
not. Create a fresh namespace, translate the desired topology, verify the new
admitted inventory, and then retire the old network.

Attacknet continues to use its reviewed Hacknet topology until an explicit
adapter migration is qualified. The standalone chart does not claim behavioral
compatibility for Attacknet faults or runs merely because it can realize the
same actor graph.

Generated profiles intentionally cover only disposable Bitcoin regtest and
non-mining Stacks nodes. The Bitcoin profile's fixed development RPC
credentials are public configuration, not secrets. Move miner and signer full
configurations into Secrets and reference them with expected digests. Signer
enrollment is a bootstrap operation rather than a long-running actor and
remains outside this initial topology API.
