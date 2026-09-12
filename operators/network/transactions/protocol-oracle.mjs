/** Test-only offline oracle for the portable Go protocol subset. */
import { writeFileSync, readFileSync } from "node:fs";
import { createHash } from "node:crypto";
import { Cl, serializeCV, makeSTXTokenTransfer, makeContractCall, makeContractDeploy, signStructuredData, encodeStructuredDataBytes, getAddressFromPrivateKey, privateKeyToPublic } from "@stacks/transactions";
import { StackingClient } from "@stacks/stacking";

for (const pkg of ["transactions", "stacking"]) {
  const version = JSON.parse(readFileSync(new URL(`node_modules/@stacks/${pkg}/package.json`, import.meta.url))).version;
  if (version !== "7.6.0") throw Error("oracle must use pinned Stacks.js 7.6.0");
}
globalThis.fetch = () => { throw Error("protocol oracle must remain offline"); };
/** Translate the independent fixture's typed JSON into SDK values. */
function cv(v) {
  switch(v.type) {
    case 0: return Cl.int(v.integer);
    case 1: return Cl.uint(v.integer);
    case 2: return Cl.bufferFromHex(v.bytes ?? "");
    case 3: return Cl.bool(true);
    case 4: return Cl.bool(false);
    case 5: case 6: return Cl.principal(v.text);
    case 7: return Cl.ok(cv(v.items[0]));
    case 8: return Cl.error(cv(v.items[0]));
    case 9: return Cl.none();
    case 10: return Cl.some(cv(v.items[0]));
    case 11: return Cl.list(v.items.map(cv));
    case 12: return Cl.tuple(Object.fromEntries(Object.entries(v.fields).map(([k,x])=>[k,cv(x)])));
    case 13: return Cl.stringAscii(v.text);
    case 14: return Cl.stringUtf8(v.text);
    default: throw Error("unsupported fixture type");
  }
}
const uint = integer => ({type:1,integer:String(integer)});
const address = "ST000000000000000000002AMW42H";
const values = [
 ...["0", "1", "18446744073709551615", "340282366920938463463374607431768211455"].map(uint),
 ...["-170141183460469231731687303715884105728","170141183460469231731687303715884105727","-1","0"].map(integer=>({type:0,integer})),
 {type:2,bytes:"00017fff"}, {type:2,bytes:""}, {type:3}, {type:4}, {type:5,text:address}, {type:6,text:address+".pox-4"},
 {type:7,items:[uint(1)]},{type:8,items:[uint(0)]},{type:9},{type:10,items:[{type:2,bytes:"ff"}]},
 {type:11,items:[uint(1),uint(2)]},{type:11,items:[]},
 {type:12,fields:{z:uint(1),"auth-id":uint(2),a:{type:10,items:[{type:3}]}}},
 {type:13,text:"hello\nworld"},{type:14,text:"Stacks 🦊 日本語"},
];
const result={oracle:"Stacks.js 7.6.0",clarity:values.map(input=>({input,bytes:serializeCV(cv(input))})),transactions:[],signatures:[],poxCalls:[]};
for (let i=1;i<=12;i++) {
 const scalar=BigInt(i).toString(16).padStart(64,"0");
 const privateKey=scalar+(i%2===0?"01":"");
 const nonce=i===12?"18446744073709551615":String(i-1), fee=i===12?"18446744073709551615":String(3000+i);
 const network=i%3===0?"mainnet":"testnet";
 const common={senderKey:privateKey,network,nonce:BigInt(nonce),fee:BigInt(fee),postConditionMode:i%2===0?1:2};
 for (const operation of ["transfer","call","deploy"]) {
  const input={operation,privateKey,nonce,fee,version:network==="mainnet"?0:128,chainID:network==="mainnet"?1:2147483648,postConditionMode:common.postConditionMode};
  let tx;
  if(operation==="transfer") { input.postConditionMode=2; Object.assign(input,{recipient:getAddressFromPrivateKey("0".repeat(63)+"101",network)+(i%4===0?".vault":""),amount:i===12?"18446744073709551615":String(i*100),memo:i%2?"stacks-k8s baseline":"🦊"}); tx=await makeSTXTokenTransfer({...common,recipient:input.recipient,amount:BigInt(input.amount),memo:input.memo}); }
  if(operation==="call") { Object.assign(input,{address,contract:"pox-4",function:"stack-stx",args:[uint(i),values[20],{type:9}]}); tx=await makeContractCall({...common,contractAddress:address,contractName:input.contract,functionName:input.function,functionArgs:input.args.map(cv),validateWithAbi:false}); }
  if(operation==="deploy") { Object.assign(input,{contract:"sample",source:"(define-public (hello) (ok u1))\n",clarityVersion:i%6+1}); tx=await makeContractDeploy({...common,contractName:input.contract,codeBody:input.source,clarityVersion:input.clarityVersion}); }
  result.transactions.push({input,bytes:tx.serialize(),txid:tx.txid()});
 }
 const domain=Cl.tuple({name:Cl.stringAscii("pox-5-signer"),version:Cl.stringAscii("1.0.0"),"chain-id":Cl.uint(2147483648)});
 const manager=address+".direct-manager", authID=String(i);
 const message=Cl.tuple({"signer-manager":Cl.principal(manager),topic:Cl.stringAscii("grant-authorization"),"auth-id":Cl.uint(authID)});
 result.signatures.push({kind:"grant",privateKey,manager,authID,signature:signStructuredData({privateKey,domain,message}),digest:createHash("sha256").update(encodeStructuredDataBytes({domain,message})).digest("hex")});
 const poxAddress="mrCDrCybB6J1vRfbwM5hemdJz73FwDBC8r", topic=i%2?"stack-stx":"stack-extend";
 const client=new StackingClient({address,network:"testnet"});
 result.signatures.push({kind:"pox",privateKey,authID,topic,rewardCycle:i,period:12,maxAmount:"340282366920938463463374607431768211455",poxVersion:0,poxHash:"751e76e8199196d454941c45d1b3a323f1433bd6",signature:client.signPoxSignature({topic,poxAddress,rewardCycle:i,period:12,signerPrivateKey:privateKey,authId:BigInt(authID),maxAmount:(1n<<128n)-1n})});
 const signature=result.signatures.at(-1).signature;
 const signerKey=privateKeyToPublic(scalar+"01");
 const poxCommon={contract:address+".pox-4",poxAddress,signerKey,signerSignature:i%3===0?undefined:signature,maxAmount:(1n<<128n)-1n,authId:BigInt(authID)};
 const operation=i%2?"enroll":"extend";
 const call=operation==="enroll"?client.getStackOptions({...poxCommon,amountMicroStx:100000000000n,cycles:12,burnBlockHeight:203}):client.getStackExtendOptions({...poxCommon,extendCycles:12});
 const tx=await makeContractCall({...call,senderKey:privateKey,nonce:BigInt(nonce),fee:BigInt(fee),validateWithAbi:false});
 result.poxCalls.push({operation,privateKey,nonce,fee,authID,signerKey,signature:poxCommon.signerSignature??"",args:call.functionArgs.map(serializeCV),bytes:tx.serialize(),txid:tx.txid()});
}
const fixtureURL = new URL("../../../contracts/stacks-protocol-v1/vectors.json",import.meta.url);
const output = JSON.stringify(result,null,2)+"\n";
if (process.argv.includes("--check")) {
  if (readFileSync(fixtureURL,"utf8") !== output) throw Error("portable Go protocol oracle fixture differs");
} else {
  writeFileSync(fixtureURL,output);
}
