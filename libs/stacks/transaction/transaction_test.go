package transaction

import (
	"strings"
	"testing"

	"github.com/cylewitruk-stacks/stacks-k8s/libs/stacks/clarity"
)

func TestInvalidSigningInputs(t *testing.T) {
	o := Options{
		Version:           Testnet,
		ChainID:           0x80000000,
		PostConditionMode: Deny,
		PrivateKey:        strings.Repeat("0", 63) + "101",
	}
	address := "ST000000000000000000002AMW42H"
	for _, key := range []string{
		"",
		strings.Repeat("0", 64),
		strings.Repeat("f", 64),
		strings.Repeat("1", 64) + "02",
		"secret",
	} {
		bad := o
		bad.PrivateKey = key
		if _, e := Transfer(bad, address, 1, ""); e == nil {
			t.Fatal("accepted invalid key")
		}
	}
	for _, memo := range []string{strings.Repeat("a", 35), string([]byte{0xff})} {
		if _, e := Transfer(o, address, 1, memo); e == nil {
			t.Fatal("accepted invalid memo")
		}
	}
	for _, v := range []byte{0, 7, 255} {
		if _, e := Deploy(o, "sample", "(ok u1)", v); e == nil {
			t.Fatal("accepted unsupported version")
		}
	}
	if _, e := Call(o, address, "invalid/name", "call", nil); e == nil {
		t.Fatal("accepted invalid contract")
	}
	if _, e := Call(o, address, "sample", "call", []clarity.Value{{Type: 255}}); e == nil {
		t.Fatal("accepted invalid argument")
	}
	for _, field := range []string{"version", "mode"} {
		bad := o
		if field == "version" {
			bad.Version = 1
		} else {
			bad.PostConditionMode = 0
		}
		if _, e := Transfer(bad, address, 1, ""); e == nil {
			t.Fatal("accepted implicit option")
		}
	}
	tx, e := Transfer(o, address, 1, "")
	if e != nil {
		t.Fatal(e)
	}
	tx.Bytes[len(tx.Bytes)-1] ^= 1
	if Valid(tx) {
		t.Fatal("accepted tampered identity")
	}
}
