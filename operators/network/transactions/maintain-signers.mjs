/** External, time-bounded PoX-4 renewal for the initial disposable profile. */
import { readFileSync, writeFileSync } from "node:fs";
import { setTimeout as sleep } from "node:timers/promises";
import { StackingClient, Pox4SignatureTopic } from "@stacks/stacking";
import { localNetwork } from "./network.mjs";

const args = Object.fromEntries(
  process.argv.slice(2).map((value) => {
    const at = value.indexOf("=");
    if (at < 0) throw Error("use --option=value");
    return [value.slice(2, at), value.slice(at + 1)];
  }),
);
if (!args.manifest || !args.url || !args["duration-seconds"])
  throw Error("--manifest, --url and --duration-seconds are required");
const duration = Number(args["duration-seconds"]);
if (!Number.isSafeInteger(duration) || duration < 1 || duration > 86400)
  throw Error("duration must be 1..86400 seconds");
const { bootstrap: setup } = JSON.parse(readFileSync(args.manifest, "utf8"));
const client = new StackingClient({
  address: setup.signer.address,
  network: localNetwork(args.url),
});
const deadline = Date.now() + duration * 1000,
  maxAmount = (1n << 128n) - 1n;
const evidence = {
  schemaVersion: 1,
  network: setup.networkName,
  namespace: setup.namespace,
  renewals: [],
};
let pending = null;
while (Date.now() < deadline) {
  const info = await client.getPoxInfo(),
    status = await client.getAccountStatus();
  if (!info.contract_id.endsWith(".pox-4"))
    throw Error("this helper is qualified only for PoX-4");
  if (BigInt(status.locked) === 0n)
    throw Error(
      "signer lock expired; inspect bootstrap rather than silently re-enrolling",
    );
  if (pending) {
    if (status.unlock_height > pending.previousUnlockHeight) {
      evidence.renewals.push({
        ...pending,
        observedUnlockHeight: status.unlock_height,
        observedAt: new Date().toISOString(),
      });
      writeFileSync(
        args.evidence ?? `${args.manifest}.renewal-evidence.json`,
        JSON.stringify(evidence, null, 2) + "\n",
        { mode: 0o600 },
      );
      console.log("SignerLockExtended", status.unlock_height);
      pending = null;
    } else if (Date.now() > pending.deadline) {
      throw Error(
        "renewal did not confirm; inspect exact transaction before any resubmission",
      );
    }
  }
  const remaining = Math.ceil(
    (status.unlock_height - info.current_burnchain_block_height) /
      info.reward_cycle_length,
  );
  if (
    !pending &&
    remaining <= 6 &&
    info.next_cycle.blocks_until_prepare_phase > 0
  ) {
    const extendCycles = 6,
      authId = BigInt(info.current_burnchain_block_height);
    const signerSignature = client.signPoxSignature({
      topic: Pox4SignatureTopic.StackExtend,
      rewardCycle: info.reward_cycle_id,
      poxAddress: setup.signer.poxAddress,
      period: extendCycles,
      signerPrivateKey: setup.signer.privateKey,
      authId,
      maxAmount,
    });
    // One submission attempt; an exception or unknown outcome exits without reusing the nonce.
    const result = await client.stackExtend({
      extendCycles,
      poxAddress: setup.signer.poxAddress,
      privateKey: setup.signer.privateKey,
      signerKey: setup.signer.publicKey,
      signerSignature,
      authId,
      maxAmount,
      fee: 3000,
      nonce: status.nonce,
    });
    if (!result.txid)
      throw Error(
        "renewal submission is ambiguous; inspect the signer account before further work",
      );
    pending = {
      txid: result.txid,
      previousUnlockHeight: status.unlock_height,
      deadline: Date.now() + 180000,
    };
    console.log("SignerRenewalSubmitted", result.txid);
  }
  await sleep(2000);
}
if (pending)
  throw Error(
    "maintenance duration ended with a pending renewal; inspect its transaction before restarting",
  );
