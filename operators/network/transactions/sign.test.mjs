import assert from "node:assert/strict";
import { test } from "node:test";
import { randomBytes } from "node:crypto";
import {
  deserializeTransaction,
  getAddressFromPrivateKey,
} from "@stacks/transactions";
import { signTransfer } from "./sign.mjs";

test("signs exactly the requested testnet transfer without RPC discovery", async () => {
  const privateKey = randomBytes(32).toString("hex") + "01";
  const sender = getAddressFromPrivateKey(privateKey, "testnet");
  const input = {
    sender,
    recipient: getAddressFromPrivateKey(
      randomBytes(32).toString("hex") + "01",
      "testnet",
    ),
    nonce: "7",
    amountMicroSTX: "1",
    feeMicroSTX: "1000",
  };
  const originalFetch = globalThis.fetch;
  globalThis.fetch = () => {
    throw new Error("unexpected network access");
  };
  try {
    const signed = await signTransfer({ privateKey, sender }, input);
    const decoded = deserializeTransaction(signed.bytes);
    assert.equal(decoded.txid(), signed.txid);
    assert.equal(decoded.chainId, 0x80000000);
    assert.equal(decoded.auth.spendingCondition.nonce, 7n);
    assert.equal(decoded.auth.spendingCondition.fee, 1000n);
    assert.equal(decoded.payload.amount, 1n);
    await assert.rejects(
      signTransfer(
        { privateKey, sender },
        { ...input, sender: input.recipient },
      ),
    );
  } finally {
    globalThis.fetch = originalFetch;
  }
});
