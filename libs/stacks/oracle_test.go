package stacks_test

import (
	"encoding/hex"
	"encoding/json"
	"math/big"
	"os"
	"strconv"
	"testing"

	"github.com/cylewitruk-stacks/stacks-k8s/libs/stacks/clarity"
	"github.com/cylewitruk-stacks/stacks-k8s/libs/stacks/signing"
	"github.com/cylewitruk-stacks/stacks-k8s/libs/stacks/transaction"
)

// fixtureValue is the oracle's language-neutral input representation.
type fixtureValue struct {
	Type                 clarity.Type
	Integer, Bytes, Text string
	Items                []fixtureValue
	Fields               map[string]fixtureValue
}

// value reconstructs the production input independently of the expected bytes.
func (f fixtureValue) value(t *testing.T) clarity.Value {
	t.Helper()
	v := clarity.Value{Type: f.Type, Text: f.Text}
	if f.Integer != "" {
		var ok bool
		v.Integer, ok = new(big.Int).SetString(f.Integer, 10)
		if !ok {
			t.Fatal("invalid integer fixture")
		}
	}
	var e error
	v.Bytes, e = hex.DecodeString(f.Bytes)
	if e != nil {
		t.Fatal(e)
	}
	for _, item := range f.Items {
		v.Items = append(v.Items, item.value(t))
	}
	if f.Fields != nil {
		v.Fields = map[string]clarity.Value{}
		for k, x := range f.Fields {
			v.Fields[k] = x.value(t)
		}
	}
	return v
}

// uint64Value decodes explicit unsigned transaction fields without float conversion.
func uint64Value(t *testing.T, s string) uint64 {
	t.Helper()
	n, e := strconv.ParseUint(s, 10, 64)
	if e != nil {
		t.Fatal(e)
	}
	return n
}

func TestStacksJSOracle(t *testing.T) {
	raw, e := os.ReadFile("../../contracts/stacks-protocol-v1/vectors.json")
	if e != nil {
		t.Fatal(e)
	}
	var fixture struct {
		Oracle  string
		Clarity []struct {
			Input fixtureValue
			Bytes string
		}
		Transactions []struct {
			Input struct {
				Operation, PrivateKey, Nonce, Fee, Recipient, Amount, Memo, Address, Contract, Function, Source string
				Version, PostConditionMode, ClarityVersion                                                      byte
				ChainID                                                                                         uint32
				Args                                                                                            []fixtureValue
			}
			Bytes, TxID string
		}
		Signatures []struct {
			Kind, PrivateKey, Manager, AuthID, Signature, Digest, Topic, MaxAmount, PoxHash string
			RewardCycle, Period                                                             uint64
			PoxVersion                                                                      byte
		}
	}
	if e = json.Unmarshal(raw, &fixture); e != nil {
		t.Fatal(e)
	}
	if fixture.Oracle != "Stacks.js 7.6.0" || len(fixture.Clarity) != 23 || len(fixture.Transactions) != 36 ||
		len(fixture.Signatures) != 24 {
		t.Fatal("incomplete pinned oracle")
	}
	for i, v := range fixture.Clarity {
		t.Run("clarity/"+strconv.Itoa(i), func(t *testing.T) {
			encoded, e := clarity.Encode(v.Input.value(t))
			if e != nil {
				t.Fatal(e)
			}
			if hex.EncodeToString(encoded) != v.Bytes {
				t.Fatalf("Clarity differs: %x", encoded)
			}
			decoded, e := clarity.Decode(encoded)
			if e != nil {
				t.Fatal(e)
			}
			round, e := clarity.Encode(decoded)
			if e != nil || hex.EncodeToString(round) != v.Bytes {
				t.Fatal("decode round trip differs")
			}
		})
	}
	for i, v := range fixture.Transactions {
		t.Run("transaction/"+strconv.Itoa(i), func(t *testing.T) {
			in := v.Input
			o := transaction.Options{
				Version:           in.Version,
				ChainID:           in.ChainID,
				Nonce:             uint64Value(t, in.Nonce),
				Fee:               uint64Value(t, in.Fee),
				PostConditionMode: in.PostConditionMode,
				PrivateKey:        in.PrivateKey,
			}
			var tx transaction.Transaction
			var e error
			switch in.Operation {
			case "transfer":
				tx, e = transaction.Transfer(o, in.Recipient, uint64Value(t, in.Amount), in.Memo)
			case "call":
				args := []clarity.Value{}
				for _, a := range in.Args {
					args = append(args, a.value(t))
				}
				tx, e = transaction.Call(o, in.Address, in.Contract, in.Function, args)
			case "deploy":
				tx, e = transaction.Deploy(o, in.Contract, in.Source, in.ClarityVersion)
			default:
				t.Fatal("invalid fixture")
			}
			if e != nil {
				t.Fatal(e)
			}
			if hex.EncodeToString(tx.Bytes) != v.Bytes || tx.TxID != v.TxID {
				t.Fatalf("SDK transaction mismatch\ngot  %x\nwant %s\nTxID %s / %s", tx.Bytes, v.Bytes, tx.TxID, v.TxID)
			}
			if !transaction.Valid(tx) {
				t.Fatal("invalid signed identity")
			}
		})
	}
	for i, v := range fixture.Signatures {
		t.Run("signature/"+strconv.Itoa(i), func(t *testing.T) {
			auth, e := clarity.Uint128(v.AuthID)
			if e != nil {
				t.Fatal(e)
			}
			var sig [65]byte
			if v.Kind == "grant" {
				sig, e = signing.SignerGrant(v.PrivateKey, v.Manager, auth, 0x80000000)
				principal, pe := clarity.Principal(v.Manager)
				if pe != nil {
					t.Fatal(pe)
				}
				message := clarity.Value{
					Type: clarity.Tuple,
					Fields: map[string]clarity.Value{
						"signer-manager": principal,
						"topic":          {Type: clarity.ASCII, Text: "grant-authorization"},
						"auth-id":        auth,
					},
				}
				digest, de := signing.StructuredDigest(signing.Domain("pox-5-signer", "1.0.0", 0x80000000), message)
				if de != nil || hex.EncodeToString(digest[:]) != v.Digest {
					t.Fatal("SDK structured digest mismatch")
				}
			} else {
				amount, ae := clarity.Uint128(v.MaxAmount)
				if ae != nil {
					t.Fatal(ae)
				}
				hash, he := hex.DecodeString(v.PoxHash)
				if he != nil {
					t.Fatal(he)
				}
				sig, e = signing.PoX(
					v.PrivateKey,
					signing.PoXAuthorization{
						Address:     signing.PoXAddress{Version: v.PoxVersion, HashBytes: hash},
						RewardCycle: v.RewardCycle,
						Topic:       v.Topic,
						Period:      v.Period,
						MaxAmount:   amount,
						AuthID:      auth,
						ChainID:     0x80000000,
					},
				)
			}
			if e != nil {
				t.Fatal(e)
			}
			if hex.EncodeToString(sig[:]) != v.Signature {
				t.Fatalf("SDK structured signature mismatch\ngot  %x\nwant %s", sig, v.Signature)
			}
		})
	}
}

func TestPoXCallOracle(t *testing.T) {
	raw, e := os.ReadFile("../../contracts/stacks-protocol-v1/vectors.json")
	if e != nil {
		t.Fatal(e)
	}
	var fixture struct {
		PoXCalls []struct {
			Operation, PrivateKey, Nonce, Fee, AuthID, SignerKey, Signature, Bytes, TxID string
			Args                                                                         []string
		}
	}
	if e = json.Unmarshal(raw, &fixture); e != nil {
		t.Fatal(e)
	}
	if len(fixture.PoXCalls) != 12 {
		t.Fatal("incomplete PoX call oracle")
	}
	hash, _ := hex.DecodeString("751e76e8199196d454941c45d1b3a323f1433bd6")
	address := signing.PoXAddress{Version: 0, HashBytes: hash}
	amount, _ := clarity.Uint128("340282366920938463463374607431768211455")
	for i, v := range fixture.PoXCalls {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			auth, e := clarity.Uint128(v.AuthID)
			if e != nil {
				t.Fatal(e)
			}
			var signature []byte
			if v.Signature != "" {
				signature, e = hex.DecodeString(v.Signature)
				if e != nil {
					t.Fatal(e)
				}
			}
			signer := signing.SignerArguments{
				Signature: signature,
				PublicKey: v.SignerKey,
				MaxAmount: amount,
				AuthID:    auth,
			}
			var args []clarity.Value
			function := "stack-stx"
			if v.Operation == "enroll" {
				args, e = signing.StackSTXArguments(clarity.Uint(100000000000), address, 203, 12, signer)
			} else {
				function = "stack-extend"
				args, e = signing.StackExtendArguments(address, 12, signer)
			}
			if e != nil {
				t.Fatal(e)
			}
			if len(args) != len(v.Args) {
				t.Fatal("argument count differs")
			}
			for j, arg := range args {
				encoded, e := clarity.Encode(arg)
				if e != nil || hex.EncodeToString(encoded) != v.Args[j] {
					t.Fatalf("PoX argument %d differs: %v", j, e)
				}
			}
			tx, e := transaction.Call(
				transaction.Options{
					Version:           transaction.Testnet,
					ChainID:           0x80000000,
					PrivateKey:        v.PrivateKey,
					Nonce:             uint64Value(t, v.Nonce),
					Fee:               uint64Value(t, v.Fee),
					PostConditionMode: transaction.Deny,
				},
				"ST000000000000000000002AMW42H",
				"pox-4",
				function,
				args,
			)
			if e != nil {
				t.Fatal(e)
			}
			if hex.EncodeToString(tx.Bytes) != v.Bytes || tx.TxID != v.TxID {
				t.Fatal("PoX signed transaction differs")
			}
		})
	}
}
