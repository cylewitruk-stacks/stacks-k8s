/** Offline STX-transfer signing. Kubernetes and node RPC remain owned by the Go controller. */
import { readFileSync } from "node:fs";
import {
  makeSTXTokenTransfer,
  getAddressFromPrivateKey,
} from "@stacks/transactions";
import { fileURLToPath } from "node:url";

/** Sign an explicit testnet transfer without SDK fee/nonce discovery or network access. */
export async function signTransfer(account, input) {
  if (
    getAddressFromPrivateKey(account.privateKey, "testnet") !==
      account.sender ||
    input.sender !== account.sender
  ) {
    throw new Error("signing profile does not match the requested sender");
  }
  for (const field of ["nonce", "amountMicroSTX", "feeMicroSTX"]) {
    if (!/^\d+$/.test(String(input[field])))
      throw new Error(`invalid ${field}`);
  }
  const transaction = await makeSTXTokenTransfer({
    senderKey: account.privateKey,
    network: "testnet",
    recipient: input.recipient,
    amount: BigInt(input.amountMicroSTX),
    fee: BigInt(input.feeMicroSTX),
    nonce: BigInt(input.nonce),
    memo: "stacks-k8s baseline",
  });
  return { txid: transaction.txid(), bytes: transaction.serialize() };
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  try {
    const account = JSON.parse(readFileSync(process.argv[2], "utf8"));
    const input = JSON.parse(readFileSync(0, "utf8"));
    process.stdout.write(JSON.stringify(await signTransfer(account, input)));
  } catch {
    process.stderr.write("STX transfer signing failed\n");
    process.exitCode = 1;
  }
}
