package identity

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestScalarBoundsAndPublicValidation(t *testing.T) {
	for _, key := range []string{
		"",
		strings.Repeat("0", 64),
		strings.Repeat("f", 64),
		strings.Repeat("1", 62),
		strings.Repeat("1", 64) + "02",
	} {
		if _, err := FromPrivate(key); err == nil {
			t.Fatalf("accepted invalid scalar of length %d", len(key))
		}
	}
	one := strings.Repeat("0", 63) + "1"
	p, err := FromPrivate(one)
	if err != nil {
		t.Fatal(err)
	}
	if p.PublicKey != "0279be667ef9dcbbac55a06295ce870b07029bfcdb2dce28d959f2815b16f81798" {
		t.Fatal("incorrect generator encoding")
	}
	if p.BitcoinAddress != "mrCDrCybB6J1vRfbwM5hemdJz73FwDBC8r" {
		t.Fatal("incorrect Bitcoin encoding")
	}
	q, err := FromPublic(p.PublicKey)
	if err != nil || q != p {
		t.Fatal("public/private encodings disagree")
	}
	for _, key := range []string{"", p.MiningPublicKey, "02" + strings.Repeat("f", 64)} {
		if _, err := FromPublic(key); err == nil {
			t.Fatal("accepted invalid public key")
		}
	}
	generated, err := Generate()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := FromPrivate(generated); err != nil {
		t.Fatal(err)
	}
}

func TestStacksJSIdentityVectors(t *testing.T) {
	data, err := os.ReadFile("../../../contracts/stacks-identity/v1/vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	var vectors []struct {
		PrivateKey string `json:"privateKey"`
		Public
	}
	if err := json.Unmarshal(data, &vectors); err != nil {
		t.Fatal(err)
	}
	if len(vectors) != 32 {
		t.Fatal("incomplete oracle vectors")
	}
	for i, v := range vectors {
		got, err := FromPrivate(v.PrivateKey)
		if err != nil || got != v.Public {
			t.Fatalf("vector %d mismatch: %+v; %v", i, got, err)
		}
	}
}

func TestDescriptorRejectsUnsupportedFormats(t *testing.T) {
	key := "0279be667ef9dcbbac55a06295ce870b07029bfcdb2dce28d959f2815b16f81798"
	valid, address, err := FromDescriptor("pkh(" + key + ")")
	if err != nil || address != valid.BitcoinAddress {
		t.Fatalf("public descriptor: %v", err)
	}
	for _, descriptor := range []string{
		"wpkh(" + key + ")",
		"pkh(" + key + ")#checksum",
		"pkh(xprv123/*)",
		"pkh(00)",
		"pkh(" + key[:64] + ")",
	} {
		if _, _, err := FromDescriptor(descriptor); err == nil {
			t.Fatalf("unsupported descriptor accepted: %s", descriptor)
		}
	}
}
