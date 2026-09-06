/** External, bounded PoX-4 bootstrap. Never run against a reused environment. */
import { readFileSync, writeFileSync } from "node:fs";
import { execFileSync, spawn } from "node:child_process";
import { setTimeout as sleep } from "node:timers/promises";
import { StackingClient, Pox4SignatureTopic } from "@stacks/stacking";
import { localNetwork } from "./network.mjs";

const args = Object.fromEntries(
  process.argv.slice(2).map((value) => {
    const i = value.indexOf("=");
    if (i < 0) throw Error("use --option=value");
    return [value.slice(2, i), value.slice(i + 1)];
  }),
);
if (!args.manifest || !args.kubeconfig || !args.context)
  throw Error("--manifest, --kubeconfig and --context are required");
const doc = JSON.parse(readFileSync(args.manifest, "utf8")),
  setup = doc.bootstrap;
const prefix = [
  "--kubeconfig",
  args.kubeconfig,
  "--context",
  args.context,
  "-n",
  setup.namespace,
];
/** Execute a bounded command against the explicitly selected namespace. */
const kube = (...argv) =>
  execFileSync("kubectl", [...prefix, ...argv], {
    encoding: "utf8",
    timeout: 30000,
    maxBuffer: 2 << 20,
    stdio: ["pipe", "pipe", "pipe"],
  });
/** Read one network-scoped Kubernetes resource. */
const get = (resource) =>
  JSON.parse(kube("get", resource, setup.networkName, "-o", "json"));
/** Change only the external bootstrap's declared parent fields. */
const patch = (spec) =>
  kube(
    "patch",
    "stacksnetwork",
    setup.networkName,
    "--type=merge",
    "-p",
    JSON.stringify({ spec }),
  );
const forwarders = [];
const ports = {
  bitcoin: Number(args["bitcoin-port"] ?? 19443),
  stacks: Number(args["stacks-port"] ?? 20443),
};
const bitcoinURL = `http://127.0.0.1:${ports.bitcoin}`,
  stacksURL = `http://127.0.0.1:${ports.stacks}`;
const evidence = {
  schemaVersion: 1,
  namespace: setup.namespace,
  network: setup.networkName,
  events: [],
};
/** Persist credential-free bootstrap observations. */
const record = (event, details = {}) => {
  evidence.events.push({ event, at: new Date().toISOString(), ...details });
  writeFileSync(
    args.evidence ?? `${args.manifest}.bootstrap-evidence.json`,
    JSON.stringify(evidence, null, 2) + "\n",
    { mode: 0o600 },
  );
  console.log(event, JSON.stringify(details));
};
/** Poll a prerequisite within the given wall-clock bound. */
async function wait(label, fn, seconds = 180) {
  const deadline = Date.now() + seconds * 1000;
  while (Date.now() < deadline) {
    if (await fn()) return;
    await sleep(1000);
  }
  throw Error(`${label} did not complete`);
}
/** Open a loopback-only forwarding session owned by this helper. */
async function forward(service, local, remote) {
  const child = spawn(
    "kubectl",
    [
      ...prefix,
      "port-forward",
      "--address",
      "127.0.0.1",
      `service/${service}`,
      `${local}:${remote}`,
    ],
    { stdio: ["ignore", "pipe", "pipe"] },
  );
  forwarders.push(child);
  child.stderr.resume();
  await new Promise((resolve, reject) => {
    const timer = setTimeout(
      () => reject(Error("port-forward startup timed out")),
      20000,
    );
    child.stdout.on("data", (value) => {
      if (String(value).includes("Forwarding from")) {
        clearTimeout(timer);
        resolve();
      }
    });
    child.on("exit", () => {
      clearTimeout(timer);
      reject(Error("port-forward exited"));
    });
  });
}
/** Issue one Bitcoin request without retrying uncertain mutations. */
async function bitcoin(method, params = [], wallet = "") {
  const response = await fetch(
    bitcoinURL + (wallet ? `/wallet/${wallet}` : ""),
    {
      method: "POST",
      signal: AbortSignal.timeout(
        method === "generatetoaddress" ? 120000 : 30000,
      ),
      headers: {
        Authorization:
          "Basic " +
          Buffer.from(
            setup.bitcoin.username + ":" + setup.bitcoin.password,
          ).toString("base64"),
        "Content-Type": "application/json",
      },
      body: JSON.stringify({ jsonrpc: "2.0", id: "bootstrap", method, params }),
    },
  );
  const value = await response.json();
  if (!response.ok || value.error)
    throw Error(`Bitcoin ${method} failed; do not blindly rerun bootstrap`);
  return value.result;
}
/** Read a bounded native Stacks response. */
async function json(path) {
  const response = await fetch(stacksURL + path, {
    signal: AbortSignal.timeout(5000),
  });
  if (!response.ok) throw Error("Stacks read unavailable");
  return response.json();
}
try {
  await wait("Bitcoin readiness", () => {
    try {
      return (
        JSON.parse(
          kube(
            "get",
            "bitcoinnode",
            setup.networkName + "-bitcoin",
            "-o",
            "json",
          ),
        ).status?.ready === true
      );
    } catch {
      return false;
    }
  });
  await forward(setup.networkName + "-bitcoin", ports.bitcoin, 18443);
  const parent = get("stacksnetwork"),
    ledger = get("bitcoinblockproduction");
  if (
    parent.spec.bitcoinBlockProduction?.paused !== true ||
    ledger.status?.dispatchID ||
    (await bitcoin("getblockcount")) !== 0
  )
    throw Error(
      "bootstrap requires a fresh height-zero network and a never-used paused Bitcoin ledger",
    );
  const wallet = await bitcoin("createwallet", [
    "stacks-miner",
    true,
    true,
    "",
    false,
    true,
  ]);
  if (!wallet.name) throw Error("wallet creation did not confirm");
  const descriptor = await bitcoin("getdescriptorinfo", [
    `addr(${setup.bitcoin.address})`,
  ]);
  const imported = await bitcoin(
    "importdescriptors",
    [[{ desc: descriptor.descriptor, timestamp: 0 }]],
    "stacks-miner",
  );
  if (!imported[0]?.success) throw Error("descriptor import did not confirm");
  const hashes = await bitcoin("generatetoaddress", [
    201,
    setup.bitcoin.address,
    1000000,
  ]);
  if (hashes.length !== 201) throw Error("bootstrap generation is incomplete");
  record("BitcoinBootstrap", {
    blocks: hashes.length,
    lastBlockHash: hashes.at(-1),
  });
  patch({
    stacksNodes: parent.spec.stacksNodes.map((x) => ({
      ...x,
      suspended: false,
    })),
    signers: parent.spec.signers.map((x) => ({ ...x, suspended: false })),
  });
  await wait(
    "Stacks ingress readiness",
    () => {
      try {
        return (
          JSON.parse(
            kube(
              "get",
              "stacksnode",
              setup.networkName + "-signer-node",
              "-o",
              "json",
            ),
          ).status?.ready === true
        );
      } catch {
        return false;
      }
    },
    240,
  );
  await forward(setup.networkName + "-signer-node", ports.stacks, 20443);
  await wait(
    "Stacks burnchain catchup",
    async () => {
      try {
        return (await json("/v2/info")).burn_block_height >= 201;
      } catch {
        return false;
      }
    },
    180,
  );
  patch({ bitcoinBlockProduction: { paused: false } });
  const network = localNetwork(stacksURL);
  const client = new StackingClient({ address: setup.signer.address, network });
  await wait(
    "PoX-4 activation",
    async () => {
      try {
        return (await client.getPoxInfo()).contract_id.endsWith(".pox-4");
      } catch {
        return false;
      }
    },
    180,
  );
  const info = await client.getPoxInfo(),
    status = await client.getAccountStatus();
  const amount = BigInt(Math.floor(info.next_cycle.min_threshold_ustx * 1.5)),
    authId = 1n,
    maxAmount = (1n << 128n) - 1n,
    cycles = 12;
  if (BigInt(status.balance) < amount)
    throw Error("signer genesis balance is insufficient");
  const signature = client.signPoxSignature({
    topic: Pox4SignatureTopic.StackStx,
    rewardCycle: info.reward_cycle_id,
    poxAddress: setup.signer.poxAddress,
    period: cycles,
    signerPrivateKey: setup.signer.privateKey,
    authId,
    maxAmount,
  });
  const result = await client.stack({
    poxAddress: setup.signer.poxAddress,
    privateKey: setup.signer.privateKey,
    amountMicroStx: amount,
    burnBlockHeight: info.current_burnchain_block_height,
    cycles,
    fee: 3000,
    nonce: status.nonce,
    signerKey: setup.signer.publicKey,
    signerSignature: signature,
    authId,
    maxAmount,
  });
  if (!result.txid)
    throw Error(
      "enrollment submission did not acknowledge a transaction; inspect its nonce before any further action",
    );
  record("SignerEnrollmentSubmitted", { txid: result.txid });
  await wait(
    "signer lock confirmation",
    async () => BigInt((await client.getAccountStatus()).locked) > 0n,
    180,
  );
  record("SignerEnrollmentLocked", {
    unlockHeight: (await client.getAccountStatus()).unlock_height,
  });
  await wait(
    "Nakamoto activation",
    async () => {
      const value = await json("/v2/info");
      return value.burn_block_height >= 230 && value.stacks_tip_height > 0;
    },
    240,
  );
  patch({ stacksTransactionProduction: { paused: false } });
  await wait(
    "first exact transfer inclusion",
    () => get("stackstransactionproduction").status?.confirmed > 0,
    240,
  );
  const ready = await json("/v2/info");
  record("BootstrapComplete", {
    burnHeight: ready.burn_block_height,
    stacksHeight: ready.stacks_tip_height,
    serverVersion: ready.server_version,
    genesisHash: ready.genesis_chainstate_hash,
    confirmedTransfers: get("stackstransactionproduction").status.confirmed,
  });
} finally {
  for (const child of forwarders) child.kill("SIGTERM");
}
