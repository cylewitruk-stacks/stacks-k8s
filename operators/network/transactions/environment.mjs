/** Emit a fresh Bitcoin/Stacks environment; output contains disposable credentials. */
import { createECDH, createHash, createHmac, randomBytes } from "node:crypto";
import { execFileSync } from "node:child_process";
import { fileURLToPath } from "node:url";
import { dirname, resolve } from "node:path";
import { getAddressFromPrivateKey } from "@stacks/transactions";
import { publicKeyToBtcAddress } from "@stacks/encryption";

const options = Object.fromEntries(
  process.argv.slice(2).map((arg) => {
    const at = arg.indexOf("=");
    if (at < 0) throw Error("use --option=value");
    return [arg.slice(2, at), arg.slice(at + 1)];
  }),
);
const namespace = options.namespace,
  name = options.name ?? "stacks",
  stacksImage = options["stacks-image"];
if (!namespace || !stacksImage)
  throw Error("--namespace and --stacks-image are required");
const root = resolve(dirname(fileURLToPath(import.meta.url)), "../../..");
const doc = JSON.parse(
  execFileSync(
    "go",
    [
      "-C",
      resolve(root, "operators/network"),
      "run",
      "./cmd/bitcoin-environment",
      "--namespace",
      namespace,
      "--name",
      name,
    ],
    { env: { ...process.env, GOWORK: "off" }, maxBuffer: 1 << 20 },
  ),
);
/** Generate fresh disposable key or credential material. */
const token = () => randomBytes(32).toString("hex");
/** Digest exact raw configuration bytes. */
const sha = (value) =>
  "sha256:" + createHash("sha256").update(value).digest("hex");
/** Provision immutable namespaced configuration outside public specs. */
const secret = (secretName, data) => ({
  apiVersion: "v1",
  kind: "Secret",
  metadata: { name: secretName, namespace },
  type: "Opaque",
  immutable: true,
  stringData: data,
});
/** Generate a distinct compressed testnet account. */
const key = () => {
  const privateKey = token() + "01";
  return {
    privateKey,
    address: getAddressFromPrivateKey(privateKey, "testnet"),
  };
};
const transfer = key(),
  recipient = key(),
  signer = key(),
  minerSeed = token(),
  peerSeed = token();
/** Derive the public encoding required by the selected protocol. */
const publicKey = (seed, format = "compressed") => {
  const ec = createECDH("secp256k1");
  ec.setPrivateKey(Buffer.from(seed.slice(0, 64), "hex"));
  return ec.getPublicKey("hex", format);
};
const minerPublic = publicKey(minerSeed, "uncompressed"),
  minerAddress = publicKeyToBtcAddress(minerPublic, 111);
const signerPublic = publicKey(signer.privateKey),
  actorPassword = token(),
  authToken = token(),
  bootstrapPassword = token();
/** Encode a static Bitcoin Core rpcauth verifier. */
const verifier = (user, password) => {
  const salt = token();
  return `rpcauth=${user}:${salt}$${createHmac("sha256", salt).update(password).digest("hex")}\n`;
};
const cm = doc.items.find((x) => x.kind === "ConfigMap");
cm.data["bitcoin.conf"] = cm.data["bitcoin.conf"].replace(
  "[regtest]",
  verifier("stacks", actorPassword) +
    verifier("bootstrap", bootstrapPassword) +
    "rpcwhitelist=stacks:getblockchaininfo,getblockcount,getblockhash,getblock,getrawtransaction,getnetworkinfo,listunspent,listwallets,listwalletdir,loadwallet,importdescriptors,sendrawtransaction,estimatesmartfee\n" +
    "rpcwhitelist=bootstrap:getblockchaininfo,getblockcount,createwallet,getdescriptorinfo,importdescriptors,generatetoaddress\n[regtest]",
);
const bitcoinDigest = sha(cm.data["bitcoin.conf"]);
const producer = doc.items.find(
  (x) =>
    x.kind === "Secret" && x.metadata.name === "stacks-bitcoin-production-rpc",
);
const bitcoinCredentials = JSON.parse(
  Buffer.from(producer.data["credentials.json"], "base64"),
);
bitcoinCredentials.configDigest = bitcoinDigest;
producer.data["credentials.json"] = Buffer.from(
  JSON.stringify(bitcoinCredentials),
).toString("base64");
const network = doc.items.find((x) => x.kind === "StacksNetwork");
network.spec.bitcoinNodes[0].config.configMapRef.expectedDigest = bitcoinDigest;
network.spec.bitcoinBlockProduction.targets[0].address = minerAddress;
network.spec.bitcoinBlockProduction.paused = true;
network.spec.bitcoinBlockProduction.intervalSeconds = 5;
const genesis = [
  { address: signer.address, amount: 1000000000000000 },
  { address: transfer.address, amount: 1000000000000 },
];
const epochs = [
  ["1.0", 0],
  ["2.0", 0],
  ["2.05", 203],
  ["2.1", 204],
  ["2.2", 206],
  ["2.3", 207],
  ["2.4", 208],
  ["2.5", 209],
  ["3.0", 223],
  ["3.1", 224],
  ["3.2", 225],
  ["3.3", 226],
  ["3.4", 227],
  ["4.0", 1000005],
]
  .map(
    ([epoch, height]) =>
      `[[burnchain.epochs]]\nepoch_name = "${epoch}"\nstart_height = ${height}\n`,
  )
  .join("\n");
const balances = genesis
  .map(
    (x) => `[[ustx_balance]]\naddress = "${x.address}"\namount = ${x.amount}\n`,
  )
  .join("\n");
/** Render the initial normal miner or signer-node profile. */
function nodeConfig(actor, seed, miner) {
  return (
    `[node]\nname = "${actor}"\nrpc_bind = "0.0.0.0:20443"\np2p_bind = "0.0.0.0:20444"\ndata_url = "http://__NODE_IP__:20443"\np2p_address = "__NODE_IP__:20444"\nprometheus_bind = "0.0.0.0:20446"\nworking_dir = "/data/node"\nseed = "${seed}"\nlocal_peer_seed = "${seed}"\nminer = ${miner}\nstacker = ${!miner}\nuse_test_genesis_chainstate = true\ntxindex = true\npox_sync_sample_secs = 0\nwait_time_for_blocks = 0\nwait_time_for_microblocks = 0\nmine_microblocks = false\nevent_dispatcher_blocking = false\nevent_dispatcher_queue_size = 1000\nbootstrap_node = "${publicKey(miner ? peerSeed : minerSeed)}@\${SERVICE:${miner ? "signer-node" : "miner"}}:20444"\n` +
    (miner
      ? `\n[miner]\nfirst_attempt_time_ms = 1000\nsubsequent_attempt_time_ms = 2000\nblock_commit_delay_ms = 1000\nactivated_vrf_key_path = "/data/node/activated-vrf-key.json"\n`
      : "") +
    `\n[connection_options]\npublic_ip_address = "__NODE_IP__:20444"\nprivate_neighbors = true\nwalk_interval = 5\ninv_sync_interval = 5\ndownload_interval = 1\nauth_token = "${authToken}"\n` +
    (!miner
      ? '\n[[events_observer]]\nendpoint = "${SERVICE:signer}:30000"\nevents_keys = ["stackerdb", "block_proposal", "burn_blocks"]\n'
      : "") +
    `\n[burnchain]\nchain = "bitcoin"\nmode = "nakamoto-neon"\npoll_time_secs = 1\nmagic_bytes = "T3"\npox_prepare_length = 5\npox_reward_length = 20\nburn_fee_cap = 20000\npeer_host = "\${SERVICE:bitcoin}"\npeer_port = 18444\nrpc_port = 18443\nrpc_ssl = false\nusername = "stacks"\npassword = "${actorPassword}"\ntimeout = 30\n` +
    (miner
      ? `wallet_name = "stacks-miner"\nlocal_mining_public_key = "${minerPublic}"\n`
      : "") +
    `\n${epochs}\n${balances}`
  );
}
const minerConfig = nodeConfig("miner", minerSeed, true),
  ingressConfig = nodeConfig("signer-node", peerSeed, false);
const signerConfig = `stacks_private_key = "${signer.privateKey.slice(0, 64)}"\nnode_host = "\${SERVICE:signer-node}:20443"\nendpoint = "0.0.0.0:30000"\nnetwork = "testnet"\nauth_password = "${authToken}"\ndb_path = "/data/signer.sqlite"\nmetrics_endpoint = "0.0.0.0:31000"\nevent_timeout_ms = 250\n`;
/** Bind a raw Secret reference to its exact content digest. */
const config = (secretName, text, keyName = "config.toml") => ({
  secretRef: { name: secretName, key: keyName, expectedDigest: sha(text) },
});
network.spec.defaults.stacksNodeImage = stacksImage;
network.spec.defaults.stacksSignerImage = stacksImage;
network.spec.genesis = { balances: genesis };
network.spec.stacksNodes = [
  {
    name: "miner",
    role: "miner",
    bitcoinNodeRef: "bitcoin",
    config: config("stacks-miner-config", minerConfig),
    serviceRefs: ["signer-node"],
    suspended: true,
  },
  {
    name: "signer-node",
    role: "signer-node",
    bitcoinNodeRef: "bitcoin",
    config: config("stacks-ingress-config", ingressConfig),
    serviceRefs: ["miner"],
    suspended: true,
  },
];
network.spec.signers = [
  {
    name: "signer",
    nodeRef: "signer-node",
    index: 0,
    weight: 1,
    publicKey: signerPublic,
    config: config("stacks-signer-config", signerConfig, "signer.toml"),
    suspended: true,
  },
];
network.spec.stacksTransactionProduction = {
  target: "signer-node",
  sender: transfer.address,
  recipient: recipient.address,
  amountMicroSTX: 1,
  feeMicroSTX: 1000,
  intervalSeconds: Number(options["interval-seconds"] ?? 10),
  paused: true,
};
doc.items.push(
  secret("stacks-miner-config", { "config.toml": minerConfig }),
  secret("stacks-ingress-config", { "config.toml": ingressConfig }),
  secret("stacks-signer-config", { "signer.toml": signerConfig }),
  secret("stacks-transaction-account", {
    "account.json": JSON.stringify({
      networkName: name,
      sender: transfer.address,
      privateKey: transfer.privateKey,
      configDigest: sha(ingressConfig),
    }),
  }),
);
// Bootstrap credentials remain outside actor and producer Pods; this local document is the helper input.
doc.bootstrap = {
  networkName: name,
  namespace,
  bitcoin: {
    username: "bootstrap",
    password: bootstrapPassword,
    address: minerAddress,
  },
  signer: {
    ...signer,
    publicKey: signerPublic,
    poxAddress: publicKeyToBtcAddress(signerPublic, 111),
  },
};
process.stdout.write(JSON.stringify(doc, null, 2) + "\n");
