// Package keys centralizes private scalar validation for signing primitives.
package keys

import (
	"encoding/hex"
	"errors"
	"strings"

	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/decred/dcrd/dcrec/secp256k1/v4/ecdsa"
)

// Parse validates a scalar and returns the SDK-compatible compression flag.
// Callers must zero the returned private key after use.
func Parse(s string) (*secp256k1.PrivateKey, bool, error) {
	compressed := len(s) == 66 && strings.HasSuffix(s, "01")
	if compressed {
		s = s[:64]
	}
	raw, e := hex.DecodeString(s)
	if e != nil || len(raw) != 32 {
		return nil, false, errors.New("private key must encode a 32-byte scalar")
	}
	defer clear(raw)
	var scalar secp256k1.ModNScalar
	defer scalar.Zero()
	if scalar.SetByteSlice(raw) || scalar.IsZero() {
		return nil, false, errors.New("private scalar out of range")
	}
	return secp256k1.NewPrivateKey(&scalar), compressed, nil
}

// Sign returns deterministic low-S recovery-byte-first Stacks signatures.
func Sign(key *secp256k1.PrivateKey, digest [32]byte) [65]byte {
	compact := ecdsa.SignCompact(key, digest[:], false)
	var out [65]byte
	copy(out[:], compact)
	out[0] -= 27
	return out
}
