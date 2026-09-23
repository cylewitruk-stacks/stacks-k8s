# stacks-inspect

Capture bounded native protocol facts as JSONL. This read-only command uses the
portable Stacks and Bitcoin clients. It does not discover workloads, forward
ports, collect Secrets, choose experiment steps or diagnose failures.

```bash
GOWORK=off go -C tools/stacks-inspect run . stacks < request.json > snapshot.jsonl
```

A Stacks request uses an explicitly forwarded or reachable node endpoint:

```json
{
  "endpoint": "http://127.0.0.1:20443",
  "attribution": "network-uid/participant-uid/pod-uid",
  "timeoutSeconds": 30,
  "cycles": [18, 19]
}
```

The initial `/v2/info` supplies the index block ID used for every PoX and reward-set
read. Omit `cycles` to inspect the current and next cycle from that pinned PoX view;
at most eight cycles are accepted. Native reward-set unavailability is a captured
fact, not a failed capture. Only the pinned missing-anchor message is classified as
`PoXAnchorBlockRequired`; other unavailable responses report `Unavailable` without
server text. This is a node report, not a root-cause conclusion.

Bitcoin requests select a bounded hash walk:

```json
{
  "endpoint": "http://127.0.0.1:18443",
  "credentials": {"username": "reader", "password": "supply-securely"},
  "attribution": "network-uid/participant-uid/pod-uid",
  "timeoutSeconds": 30,
  "blocks": 12
}
```

Use a principal permitting only `getblockchaininfo`, `getblockheader` and `getblock`.
The operator's observer credential has a different allowlist; this tool does not
expand it. Supply credentials via stdin; do not save secret-bearing requests in
an evidence bundle. Neither credentials nor endpoint URLs are emitted.

`tip` optionally selects an explicit block hash; otherwise the initial Core tip is
used. `compareTip` requests a second walk. Each walks at most `blocks` (1–128)
headers through `previousblockhash`, stopping at genesis or a validated header
already captured by the primary walk. Shared headers and transaction summaries
are emitted once; the comparison validates the height link at the intersection.
A failed transaction-summary read remains a capture failure and is not retried.
Ancestry is reported when both bounded header walks complete, even if a transaction
summary fails. An incomplete header walk suppresses ancestry. The record reports
same tip, either tip as ancestor, a shared ancestor, or `unknown-within-bound`. An unknown
result does not establish divergent chains. Transaction summaries contain count
and up to 128 transaction IDs per block, with explicit truncation. They do not
include transaction bodies or identify transaction types. Core RPC responses are
limited to 64 KiB; oversized blocks are explicit failed reads, never empty blocks.

## Stacks output

The tool owns its JSON shape independently of library structs. `stacks-tip.facts`
uses `networkID`, `burnHeight`, `stacksHeight`, `tip`, `consensusHash`,
`burnConsensusHash`, `indexBlockID` and `fullySynced`. Hashes and compressed
public keys are lowercase hexadecimal strings.

`pox.facts` contains `tip` and `pox`: `contract`, `burnHeight`, `rewardCycle`,
`cycleLength`, `minThresholdMicroSTX` and signed `blocksUntilPrepare`.
`reward-set.facts` contains `tip`, `cycle` and `available`, plus either
`unavailableReason` or `prepared`. A prepared set has `version`, `signers` and
`thresholdMicroSTX`; each signer has `publicKey`, `weight` and
`stackedAmountMicroSTX`. All µSTX amounts are unsigned decimal strings, preserving
uint128 precision. Available empty signer sets use `[]`.

## Evidence boundaries

- `callerAttribution` is copied from the caller. It does not prove Kubernetes UID
  binding or that a forwarded endpoint stayed attached to the same Pod. Capture
  workload identity separately, and use a Pod-specific forward when appropriate.
- Each record has request start and completion times. Independent requests are not
  an atomic distributed snapshot; native synchronization is not a liveness claim.
- `capture.facts.complete` means the requested reads succeeded, not exhaustive
  chain coverage. Cancellation after the final successful read does not undo that
  result; cancellation before remaining work makes the capture incomplete. Walk
  and transaction limits still apply. Errors preserve prior
  records and each failed read’s requested hash/tip/cycle, omit failed facts, and
  produce nonzero exit. Output errors or hard kills can prevent a final record.
  Timeout is one total 1–120 second budget, without retries.
- Start a new output file for every capture. Redirection and artifact retention
  belong to the caller. Use [stacks-evidence](../stacks-evidence/README.md) for
  retained telemetry export and verification.
