/** Verify frozen portable-protocol vectors against the pinned offline SDK. */
import { test } from "node:test";
import { execFileSync } from "node:child_process";
import { fileURLToPath } from "node:url";

test("portable protocol fixture matches Stacks.js 7.6.0", () => {
  execFileSync(process.execPath, [fileURLToPath(new URL("protocol-oracle.mjs", import.meta.url)), "--check"], { timeout: 30_000, stdio: "pipe" });
});
