package identity

import (
	"strings"
	"testing"
)

func TestAddressCodecAndCompressedKeys(t *testing.T) {
	for _, s := range []string{"ST000000000000000000002AMW42H", "SP000000000000000000002Q6VF78"} {
		version, hash, e := DecodeAddress(s)
		if e != nil {
			t.Fatal(e)
		}
		round, e := EncodeAddress(version, hash)
		if e != nil || round != s {
			t.Fatal("address round trip differs")
		}
	}
	for _, s := range []string{
		"",
		"ST000000000000000000002AMW42X",
		"st000000000000000000002amw42h",
		"ST0000000000000000000002AMW42H",
		strings.Repeat("S", 100),
	} {
		if _, _, e := DecodeAddress(s); e == nil {
			t.Fatalf("accepted malformed address %s", s)
		}
	}
	scalar := strings.Repeat("0", 63) + "1"
	for _, input := range []string{scalar, scalar + "01"} {
		key, e := CompressedPrivateKey(input)
		if e != nil || key != scalar+"01" {
			t.Fatal("compressed key differs")
		}
		before, _ := FromPrivate(input)
		after, _ := FromPrivate(key)
		if before != after {
			t.Fatal("normalization changed identity")
		}
	}
	if _, e := CompressedPrivateKey(strings.Repeat("0", 64)); e == nil {
		t.Fatal("accepted zero scalar")
	}
}
