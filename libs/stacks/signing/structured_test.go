package signing

import (
	"strings"
	"testing"

	"github.com/cylewitruk-stacks/stacks-k8s/libs/stacks/clarity"
	"github.com/cylewitruk-stacks/stacks-k8s/libs/stacks/internal/keys"
	"github.com/decred/dcrd/dcrec/secp256k1/v4/ecdsa"
)

func TestSignatureRecoveryAndDomainSeparation(t *testing.T) {
	private := strings.Repeat("0", 63) + "1"
	domain := Domain("example", "1.0.0", 1)
	message := clarity.Uint(123)
	signature, e := Structured(private, domain, message)
	if e != nil {
		t.Fatal(e)
	}
	digest, e := StructuredDigest(domain, message)
	if e != nil {
		t.Fatal(e)
	}
	compact := append([]byte{signature[64] + 27}, signature[:64]...)
	public, _, e := ecdsa.RecoverCompact(compact, digest[:])
	if e != nil {
		t.Fatal(e)
	}
	key, _, e := keys.Parse(private)
	if e != nil {
		t.Fatal(e)
	}
	defer key.Zero()
	if !public.IsEqual(key.PubKey()) {
		t.Fatal("signature recovers another key")
	}
	other, e := Structured(private, Domain("example", "1.0.0", 2), message)
	if e != nil || other == signature {
		t.Fatal("chain domain not separated")
	}
	if _, e := Structured(private, clarity.Uint(1), message); e == nil {
		t.Fatal("accepted invalid domain")
	}
	if _, e := Structured(strings.Repeat("0", 64), domain, message); e == nil {
		t.Fatal("accepted zero scalar")
	}
}
func TestPoXInvalidInputs(t *testing.T) {
	for _, a := range []PoXAddress{{Version: 7, HashBytes: make([]byte, 32)}, {Version: 0, HashBytes: make([]byte, 32)}, {Version: 5, HashBytes: make([]byte, 20)}} {
		if _, e := a.Value(); e == nil {
			t.Fatal("accepted invalid PoX address")
		}
	}
	a := PoXAuthorization{Topic: "unknown"}
	if _, e := PoX("", a); e == nil {
		t.Fatal("accepted unknown topic")
	}
	if _, e := SignerGrant("", "invalid", clarity.Uint(1), 1); e == nil {
		t.Fatal("accepted invalid manager")
	}
}
