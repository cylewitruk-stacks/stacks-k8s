/** Offline PoX-4 signing adapter. All chain observations and submission belong to Go. */
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { StackingClient, Pox4SignatureTopic } from "@stacks/stacking";
import { getAddressFromPrivateKey, makeContractCall } from "@stacks/transactions";

/** Sign one explicitly specified enrollment or extension without network access. */
export async function signPoX(input) {
  if (input.contract !== "ST000000000000000000002AMW42H.pox-4")
    throw Error("unsupported PoX contract");
  if (getAddressFromPrivateKey(input.account.privateKey, "testnet") !== input.account.address)
    throw Error("account identity mismatch");
  if (!["enroll", "extend"].includes(input.operation)) throw Error("invalid operation");
  for (const field of ["fee", "nonce", "burnHeight", "rewardCycle", "amount", "authID", "cycles"])
    if (!/^\d+$/.test(String(input[field]))) throw Error("invalid integer input");
  const client = new StackingClient({ address: input.account.address, network: "testnet" });
  const maxAmount = (1n << 128n) - 1n;
  const common = {
    contract: input.contract,
    poxAddress: input.account.poxAddress,
    signerKey: input.account.publicKey,
    authId: BigInt(input.authID),
    maxAmount,
  };
  const signerSignature = client.signPoxSignature({
    topic: input.operation === "enroll" ? Pox4SignatureTopic.StackStx : Pox4SignatureTopic.StackExtend,
    rewardCycle: Number(input.rewardCycle),
    poxAddress: common.poxAddress,
    period: Number(input.cycles),
    signerPrivateKey: input.account.privateKey,
    authId: common.authId,
    maxAmount,
  });
  const options = input.operation === "enroll"
    ? client.getStackOptions({ ...common, signerSignature, amountMicroStx: BigInt(input.amount), cycles: Number(input.cycles), burnBlockHeight: Number(input.burnHeight) })
    : client.getStackExtendOptions({ ...common, signerSignature, extendCycles: Number(input.cycles) });
  const transaction = await makeContractCall({
    ...options,
    validateWithAbi: false,
    senderKey: input.account.privateKey,
    fee: BigInt(input.fee),
    nonce: BigInt(input.nonce),
  });
  return { txid: transaction.txid(), bytes: transaction.serialize() };
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  try {
    process.stdout.write(JSON.stringify(await signPoX(JSON.parse(readFileSync(0, "utf8")))) + "\n");
  } catch {
    process.stderr.write("PoX signing failed\n");
    process.exitCode = 1;
  }
}
