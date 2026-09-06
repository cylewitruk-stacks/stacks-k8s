import { test } from "node:test";
import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { fileURLToPath } from "node:url";
import { createHash } from "node:crypto";

test("environment separates transfer credentials, binds config and enables native indexing", () => {
  const doc = JSON.parse(
    execFileSync(
      process.execPath,
      [
        fileURLToPath(new URL("./environment.mjs", import.meta.url)),
        "--namespace=isolated-test",
        "--stacks-image=stacks:test",
      ],
      { maxBuffer: 1 << 20 },
    ),
  );
  const account = JSON.parse(
    doc.items.find((x) => x.metadata.name === "stacks-transaction-account")
      .stringData["account.json"],
  );
  const parent = doc.items.find((x) => x.kind === "StacksNetwork");
  const ingress = doc.items.find(
    (x) => x.metadata.name === "stacks-ingress-config",
  ).stringData["config.toml"];
  assert.match(ingress, /txindex = true/);
  assert.equal(
    account.configDigest,
    "sha256:" + createHash("sha256").update(ingress).digest("hex"),
  );
  assert.equal(parent.spec.stacksTransactionProduction.sender, account.sender);
  assert.equal(parent.spec.stacksTransactionProduction.amountMicroSTX, 1);
  for (const item of doc.items) {
    if (item.metadata.name !== "stacks-transaction-account")
      assert.ok(!JSON.stringify(item).includes(account.privateKey));
  }
  assert.ok(!JSON.stringify(parent).includes(doc.bootstrap.signer.privateKey));
  assert.ok(
    parent.spec.genesis.balances.every((x) => Number.isSafeInteger(x.amount)),
  );
  assert.ok(
    doc.items.filter((x) => x.kind === "Secret").every((x) => x.immutable),
  );
  assert.equal(parent.spec.bitcoinBlockProduction.paused, true);
  assert.ok(parent.spec.stacksNodes.every((x) => x.suspended));
});
