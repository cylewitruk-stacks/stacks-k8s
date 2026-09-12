package clarity

import (
	"bytes"
	"encoding/hex"
	"math/big"
	"strings"
	"testing"
)

func TestRejectMalformedEncoding(t *testing.T) {
	cases := []string{"", "ff", "00", "0100", "0300", "0200000002ff", "0bffffffff", "0cffffffff", "0e00000001ff", "0d00000002c3a9", "0c00000002016103016104", "0c00000002016203016104", "0520" + strings.Repeat("00", 20), "061a" + strings.Repeat("00", 20) + "012f"}
	for _, s := range cases {
		raw, e := hex.DecodeString(s)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = Decode(raw); e == nil {
			t.Fatalf("accepted malformed value %s", s)
		}
	}
}
func TestResourceLimits(t *testing.T) {
	nested := Value{Type: True}
	for range 31 {
		nested = Value{Type: Some, Items: []Value{nested}}
	}
	raw, e := Encode(nested)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = Decode(raw); e != nil {
		t.Fatal(e)
	}
	nested = Value{Type: Some, Items: []Value{nested}}
	if _, e = Encode(nested); e == nil {
		t.Fatal("encoded excess depth")
	}
	if _, e = Decode(append([]byte{byte(Some)}, raw...)); e == nil {
		t.Fatal("decoded excess depth")
	}
	if _, e = EncodeWithLimits(Value{Type: Buffer, Bytes: make([]byte, 100)}, Limits{100, 32}); e == nil {
		t.Fatal("encoded excess bytes")
	}
	if _, e = DecodeWithLimits(raw, Limits{len(raw) - 1, 32}); e == nil {
		t.Fatal("decoded excess bytes")
	}
	cycle := Value{Type: Tuple, Fields: map[string]Value{}}
	cycle.Fields["cycle"] = cycle
	if _, e = Encode(cycle); e == nil {
		t.Fatal("accepted cyclic value")
	}
}
func TestIntegerAndValueBounds(t *testing.T) {
	for _, s := range []string{"-1", "340282366920938463463374607431768211456", ""} {
		if _, e := Uint128(s); e == nil {
			t.Fatalf("accepted uint128 %s", s)
		}
	}
	for _, v := range []Value{{Type: UInt}, {Type: UInt, Integer: big.NewInt(-1)}, {Type: Int, Integer: new(big.Int).Lsh(big.NewInt(1), 127)}, {Type: Int, Integer: new(big.Int).Sub(new(big.Int).Neg(new(big.Int).Lsh(big.NewInt(1), 127)), big.NewInt(1))}, {Type: Some}, {Type: ResponseOK, Items: []Value{Uint(1), Uint(2)}}, {Type: ASCII, Text: "🦊"}, {Type: UTF8, Text: string([]byte{0xff})}, {Type: Tuple, Fields: map[string]Value{"bad name": Uint(0)}}, {Type: StandardPrincipal, Text: "ST000000000000000000002AMW42X"}} {
		if _, e := Encode(v); e == nil {
			t.Fatalf("accepted invalid type %d", v.Type)
		}
	}
}
func FuzzDecode(f *testing.F) {
	for _, seed := range [][]byte{{3}, {9}, {0xb, 0, 0, 0, 0}, {0xc, 0, 0, 0, 1, 1, 'a', 3}, {0xe, 0, 0, 0, 2, 0xc3, 0xa9}} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, raw []byte) {
		v, e := DecodeWithLimits(raw, Limits{4096, 16})
		if e != nil {
			return
		}
		out, e := EncodeWithLimits(v, Limits{4096, 16})
		if e != nil {
			t.Fatal(e)
		}
		if !bytes.Equal(raw, out) {
			t.Fatal("accepted noncanonical encoding")
		}
	})
}
