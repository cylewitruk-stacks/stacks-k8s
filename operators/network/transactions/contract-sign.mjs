/** Offline Clarity transaction encoding. Go supplies all calls, versions, fees and nonces. */
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { createHash } from "node:crypto";
import { Cl, encodeStructuredDataBytes, getAddressFromPrivateKey, makeContractCall, makeContractDeploy, PostConditionMode, serializeCV, signWithKey } from "@stacks/transactions";

/** Encode a typed argument without fetching an ABI or chain state. */
function argument(value) {
  switch (value.type) {
    case "uint": return Cl.uint(value.value);
    case "principal": return Cl.principal(value.value);
    case "buffer": return Cl.bufferFromHex(value.value);
    case "none": return Cl.none();
    case "list": return Cl.list(value.items.map(argument));
    case "signer-grant": {
      const encoded = encodeStructuredDataBytes({
        message: Cl.tuple({ "signer-manager": Cl.principal(value.manager), topic: Cl.stringAscii("grant-authorization"), "auth-id": Cl.uint(value.authID) }),
        domain: Cl.tuple({ name: Cl.stringAscii("pox-5-signer"), version: Cl.stringAscii("1.0.0"), "chain-id": Cl.uint(0x80000000) }),
      });
      const digest = createHash("sha256").update(encoded).digest("hex");
      const signature = signWithKey(Uint8Array.from(Buffer.from(value.privateKey.slice(0, 64), "hex")), digest);
      return Cl.bufferFromHex(signature.slice(2) + signature.slice(0, 2));
    }
    default: throw Error("unsupported Clarity argument");
  }
}

/** Serialize public read-only arguments; Go owns the query and interpretation. */
export function encodeArguments(values) {
  if (!Array.isArray(values) || values.length > 16 || values.some(value => value.type === "signer-grant")) throw Error("invalid read arguments");
  return values.map(value => "0x" + serializeCV(argument(value)));
}

/** Sign one explicit testnet publication or call, without submission or protocol decisions. */
export async function signContract(input) {
  if (getAddressFromPrivateKey(input.account.privateKey, "testnet") !== input.account.address) throw Error("account mismatch");
  for (const field of ["fee", "nonce"]) if (!/^\d+$/.test(String(input[field]))) throw Error("invalid integer");
  const common = { senderKey: input.account.privateKey, network: "testnet", fee: BigInt(input.fee), nonce: BigInt(input.nonce), postConditionMode: input.allowSTXLock === true ? PostConditionMode.Allow : PostConditionMode.Deny };
  let tx;
  if (input.operation === "publish") {
    if (![3, 6].includes(input.clarityVersion) || typeof input.source !== "string" || input.source.length > 131072) throw Error("invalid publication");
    tx = await makeContractDeploy({ ...common, contractName: input.contractName, codeBody: input.source, clarityVersion: input.clarityVersion });
  } else if (input.operation === "call") {
    tx = await makeContractCall({ ...common, contractAddress: input.contractAddress, contractName: input.contractName, functionName: input.functionName, functionArgs: input.arguments.map(argument), validateWithAbi: false });
  } else throw Error("unsupported operation");
  return { txid: tx.txid(), bytes: tx.serialize() };
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  try {
    const input = JSON.parse(readFileSync(0, "utf8"));
    const result = input.operation === "encode-arguments" ? encodeArguments(input.arguments) : await signContract(input);
    process.stdout.write(JSON.stringify(result));
  }
  catch { process.stderr.write("offline contract signing failed\n"); process.exitCode = 1; }
}
