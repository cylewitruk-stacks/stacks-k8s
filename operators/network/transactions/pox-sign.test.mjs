import { test } from "node:test";
import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { fileURLToPath } from "node:url";
import { deserializeTransaction } from "@stacks/transactions";
import { signPoX } from "./pox-sign.mjs";

// Block all SDK network discovery: signing must use only explicitly supplied inputs.
globalThis.fetch = () => { throw Error("unexpected network access"); };

test("PoX enrollment and extension sign offline with explicit account and nonce", async () => {
  const seed = "1".repeat(64);
  const [key] = JSON.parse(execFileSync(process.execPath, [fileURLToPath(new URL("./keys.mjs", import.meta.url))], { input: JSON.stringify([seed]), encoding: "utf8" }));
  const input = {
    account: { privateKey: seed + "01", address: key.address, publicKey: key.publicKey, poxAddress: key.bitcoinAddress },
    contract: "ST000000000000000000002AMW42H.pox-4", fee: "4321", nonce: "0", burnHeight: "210", rewardCycle: "10", amount: "20000000000000", authID: "1", cycles: "12",
  };
  for (const operation of ["enroll", "extend"]) {
    const signed = await signPoX({ ...input, operation });
    const tx = deserializeTransaction(signed.bytes);
    assert.equal(tx.txid(), signed.txid);
    assert.equal(tx.auth.spendingCondition.nonce, 0n);
    assert.equal(tx.auth.spendingCondition.fee, 4321n);
    assert.equal(tx.payload.functionName.content, operation === "enroll" ? "stack-stx" : "stack-extend");
  }
  await assert.rejects(signPoX({ ...input, operation: "enroll", contract: "ST000000000000000000002AMW42H.pox-5" }));
  await assert.rejects(signPoX({ ...input, operation: "enroll", nonce: "-1" }));
  for (const fee of [undefined, "-1", "1.5", "bad"])
    await assert.rejects(signPoX({ ...input, operation: "enroll", fee }));
});
