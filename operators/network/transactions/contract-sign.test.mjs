import { test } from "node:test";
import assert from "node:assert/strict";
import { getAddressFromPrivateKey, deserializeTransaction, PostConditionMode, deserializeCV, cvToString } from "@stacks/transactions";
import { signContract, encodeArguments } from "./contract-sign.mjs";

globalThis.fetch = () => { throw Error("unexpected network access"); };
const privateKey = "1".repeat(64) + "01";
const account = { privateKey, address: getAddressFromPrivateKey(privateKey, "testnet") };
const common = { account, fee: "12345", nonce: "7" };

test("read-only arguments are encoded offline without signing authority", () => {
  const values = encodeArguments([{ type: "principal", value: account.address + ".manager" }, { type: "uint", value: "42" }]);
  assert.equal(cvToString(deserializeCV(values[0])), account.address + ".manager");
  assert.equal(cvToString(deserializeCV(values[1])), "u42");
  assert.throws(() => encodeArguments([{ type: "signer-grant" }]));
});

test("contract publications preserve Go-selected bytes and Clarity version offline", async () => {
  for (const clarityVersion of [3, 6]) {
    const result = await signContract({ ...common, operation: "publish", contractName: "example", clarityVersion, source: "(define-read-only (read) u1)" });
    const tx = deserializeTransaction(result.bytes);
    assert.equal(result.txid, tx.txid());
    assert.equal(tx.auth.spendingCondition.nonce, 7n);
    assert.equal(tx.auth.spendingCondition.fee, 12345n);
    assert.equal(tx.payload.clarityVersion, clarityVersion);
    assert.equal(tx.payload.codeBody.content, "(define-read-only (read) u1)");
    assert.equal(tx.postConditionMode, PostConditionMode.Deny);
  }
});

test("calls preserve explicit arguments and only opt into the requested STX lock", async () => {
  for (const allowSTXLock of [false, true]) {
    const result = await signContract({ ...common, operation: "call", contractAddress: "ST000000000000000000002AMW42H", contractName: "pox-5", functionName: "stake", allowSTXLock,
      arguments: [{ type: "principal", value: account.address + ".manager" }, { type: "uint", value: "100000000000" }, { type: "uint", value: "12" }, { type: "uint", value: "241" }, { type: "none" }] });
    const tx = deserializeTransaction(result.bytes);
    assert.equal(tx.payload.functionName.content, "stake");
    assert.equal(tx.payload.functionArgs[1].value, 100000000000n);
    assert.equal(tx.postConditionMode, allowSTXLock ? PostConditionMode.Allow : PostConditionMode.Deny);
    assert.equal(tx.auth.spendingCondition.fee, 12345n);
  }
});

test("invalid inputs cannot trigger SDK discovery", async () => {
  await assert.rejects(signContract({ ...common, operation: "publish", clarityVersion: 99, source: "", contractName: "bad" }));
  await assert.rejects(signContract({ ...common, nonce: "-1" }));
  await assert.rejects(signContract({ ...common, account: { ...account, address: "ST000000000000000000002AMW42H" } }));
});
