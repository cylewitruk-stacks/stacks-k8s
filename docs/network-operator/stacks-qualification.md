# Initial Stacks profile qualification

Qualification date: 2026-09-06. Scope: trusted disposable regtest on Docker
Desktop/macOS arm64, a dedicated one-node kind cluster running Kubernetes
1.36.1. This record does not qualify managed Kubernetes, multi-miner behavior,
PoX-5 transitions, persistent-data migration, or general image compatibility.

## Images and inputs

| Component | Qualified artifact |
| --- | --- |
| Bitcoin Core | `bitcoin/bitcoin:31.1@sha256:da25cedc66b1daefff9f412ee196c901a899c3fa68a33b20849c3e08b5c40d63` |
| Stacks node/signer | Local `stacks-core-attacknet:a12-normal`; image ID `sha256:5b0283ee442bc02dd73807cc9b70ec94b6fd339b82d3de0d863a76c118272b81` |
| Network operator | Local `stacks-network-operator:m2-final`; image ID `sha256:169cc0ce6d958c8f90d0465206edda71baa2bbb701a48727111232eabeab1f34` |
| Transfer worker | Local `stacks-transaction-worker:m2-final`; image ID `sha256:4be7953b38b5cb7f9b2b6afd95253df22e1bb18bb56545b9035d78322486c585` |
| SDK | Locked `@stacks/transactions`, `@stacks/network`, `@stacks/encryption`, `@stacks/stacking` 7.6.0 |

The image IDs above are local Docker IDs. Kind reported these resolved runtime
digests in Pod status:

| Runtime | Digest |
| --- | --- |
| Stacks node/signer | `sha256:83f472f947408d8bef86314d35b66ec10f677261f8d12e2f6bd8c1cdcd6be1fa` |
| Network/Bitcoin controller | `sha256:f3085ea644484cb975e04a7590772b338e981c14a9e05b493cf0ba1e1c4ad3ba` |
| Transfer worker | `sha256:17224a015c761307ac0302cc56a204e74ce9a724423448b2bc78cce0f2e27c08` |

The Stacks image labels identify source revision
`fe1580785298a0382900f8aff06fc7bb79965bd8`, normal features
`monitoring_prom,slog_json`, and a `release-lite` build. Its binary reports
`attacknet-dev` with a dirty-source marker. The image digest identifies the
artifact tested; the label alone is not a reproducible source attestation.
No instrumented adversarial feature is required. This is a local qualification,
not an official Stacks release support claim.

Configuration and bootstrap take inspiration from the historical
`feat/stacks-attacknet` checkout at `f9b022bff5`, principally `contrib/helm/hacknet`
and `contrib/attacknet`. Operators do not inherit its run orchestration.
The transfer worker uses the official SDK's
[STX-transfer builder](https://docs.stacks.co/reference/stacks.js/stacks-transactions/builders/makestxtokentransfer)
and the node's [native transaction API](https://docs.stacks.co/reference/node-operations/rpc-api/transactions).

## Automated live evidence

The dedicated cluster is `stacks-m2-20260906`, with explicit kubeconfig
`/tmp/stacks-m2-20260906.kubeconfig` and context `kind-stacks-m2-20260906`.
No existing user cluster or prior Bitcoin demo was modified. The final-profile
namespace is `stacks-m2-final`; earlier iterations used separate namespaces.
Private environment manifests remain outside Git with mode `0600`.

`TestLiveStacksTransfers` passed in the preceding `stacks-m2-live4` namespace:

- Native Core corroborated four exact successful transfer inclusions, matching
  ledger TxIDs and block IDs, while the Stacks tip advanced.
- Authorization-to-observation times were 6, 6, 4, and 2 seconds at a 10-second
  offered interval. These include polling/accounting delay and do not establish
  a universal five-second inclusion bound or a block-production guarantee.
- Pausing prevented new authorizations after the outstanding transfer resolved.
- A five-second cadence edit and an unrelated unhealthy actor did not stop
  transfers through the independently admitted ingress.
- Deleting the transaction worker while an acknowledged transaction was pending
  preserved that exact inclusion and resumed subsequent nonces.
- The suite restored the 10-second policy and all four actors to Ready.

`TestLiveStacksAbandonment` passed against the separate `stacks-m2-live3`
namespace, whose ledger retained an unresolved transaction. Parent deletion
released the transaction finalizer through the installed chart's actual RBAC.
This establishes administrative disposal, not cancellation of a signed transfer.

The external PoX-4 renewal helper submitted transaction
`2c5800acc0c521d0542f142e4b03d94858871987b2b2c5aa405c142126568961` and observed
the signer unlock height increase from 460 to 580. This is helper-observed
account evidence, not a separate independent canonical transaction assertion.
Its bounded duration and uncertain-submission behavior remain explicit.

## Final-image acceptance

Fresh bootstrap in `stacks-m2-final` completed with 201 startup Bitcoin blocks,
PoX-4 enrollment transaction
`1fd404451c9a71a972f5279c0a01923ad67e2d2e0d145414c4e91cd262ab3ef9`, a signer
lock until height 460, and the first confirmed transfer at Bitcoin height 230
and Stacks height 28. The genesis hash was
`74237aa39aa50a83de11a4f53e9d3bb7d43461d1de9873f402e5453ae60bc59b`.

The complete live transfer suite also passed against the final image pair in
86.906 seconds. Four corroborated inclusions took 4, 2, 2 and 2 seconds from
authorization to observation. Pause, cadence changes, unrelated-actor failure,
and pending-worker replacement all passed again. The final chart removes the
unused ConfigMap `get` permission; readiness needs only `list`.

Local supporting logs are `/tmp/stacks-m2-final-bootstrap.log`,
`/tmp/stacks-m2-final-live-tests.log`, `/tmp/stacks-m2-abandonment.log` and
`/tmp/stacks-m2-renewal.log`. These are local working evidence, not committed
portable artifacts. Bootstrap and renewal evidence files contain public facts;
the separate provisioning manifests contain credentials and are not review inputs.

After the final chart upgrade, the network reported all four actors Ready,
122 acknowledged baseline Bitcoin blocks, 45 confirmed STX transfers, Bitcoin
height 323 and Stacks height 164. Counts continue to change while it runs.
The four earlier task namespaces were deleted after their ledger cleanup.

The final namespace also renewed its signer lock from 460 to 580, with helper
transaction `d9dd660314f45c01fef2a14dd140167a1da78c794019cb8bfcd64cb0e3b1855f`.

A bounded one-hour renewal helper is left running for the final demo through
loopback port 20448. Its log is `/tmp/stacks-m2-final-renewal.log`. After it ends,
continued operation requires another explicitly supervised maintenance window
before the signer lock expires. Do not overlap signer-account owners; inspect
any unresolved renewal before restarting the helper.

## Compatibility corrections discovered live

The first bootstrap iteration exceeded the topology contract's JSON-safe
integer range; genesis allocations now stay below that range. One startup
block-generation response timed out; the external helper now drains both
port-forward pipes and allows 120 seconds for its single bounded generation
call, without retrying an ambiguous mutation.

Native transaction reads require `node.txindex = true`. Preflight now refuses
to arm without that endpoint. Native binary POSTs require a fixed content
length. Exact inclusion reports the textual result `(ok true)`, rather than a
serialized Clarity value. Regression tests cover these API details. Earlier
unresolved ledgers were not reset or replayed to obtain a passing result.

## Reproduce live checks

First provision and bootstrap a new environment using the
[operating guide](stacks-production.md). Forward its signer-node RPC explicitly,
then run the opt-in suite; it edits and restores the selected test network:

```bash
STACKS_NETWORK_LIVE_KUBECONFIG=/absolute/path/to/dedicated-kubeconfig \
STACKS_NETWORK_LIVE_CONTEXT=kind-YOUR_DEDICATED_CLUSTER \
STACKS_TX_LIVE_NAMESPACE=YOUR_DISPOSABLE_NAMESPACE \
STACKS_TX_LIVE_NETWORK=stacks \
STACKS_TX_LIVE_RPC_URL=http://127.0.0.1:20443 \
GOWORK=off go -C operators/network test -tags=live,stacksproduction \
  ./internal/integration -run '^TestLiveStacksTransfers$' -v -count=1 -timeout=10m
```

The abandonment test requires a separate disposable network with an outstanding
transaction and deletes its parent. It is intentionally not part of the normal
transfer test. Unit and envtest suites cover lost API acknowledgements, lost
submission responses, negative identity admission, failed execution, nonce drift,
CEL immutability, and ledger deletion without blocking actor updates.

## Post-review status refinements

Bounded rejection classification and the nonce/status clarifications were added
after the live runs above. Their follow-up qualification uses native-response
HTTP fixtures, controller/race tests, and API-server envtest, including failed
rejection writes and exact-inclusion recovery. The image IDs and live results
above describe the earlier tested artifacts; this follow-up did not rebuild or
upgrade the running demo or rerun the live mutation suites.
