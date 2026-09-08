/** Offline SDK key encodings. Reads seeds from stdin; owns no resources or configuration. */
import { createECDH } from "node:crypto";
import { readFileSync } from "node:fs";
import { getAddressFromPrivateKey } from "@stacks/transactions";
import { publicKeyToBtcAddress } from "@stacks/encryption";
const seeds = JSON.parse(readFileSync(0, "utf8"));
const result = seeds.map((seed) => {
  if (!/^[0-9a-f]{64}$/.test(seed)) throw Error("invalid seed encoding");
  const key = createECDH("secp256k1");
  key.setPrivateKey(Buffer.from(seed, "hex"));
  const publicKey = key.getPublicKey("hex", "compressed");
  const miningPublicKey = key.getPublicKey("hex", "uncompressed");
  return {
    address: getAddressFromPrivateKey(seed + "01", "testnet"),
    publicKey,
    miningPublicKey,
    bitcoinAddress: publicKeyToBtcAddress(publicKey, 111),
    miningAddress: publicKeyToBtcAddress(miningPublicKey, 111),
  };
});
process.stdout.write(JSON.stringify(result) + "\n");
