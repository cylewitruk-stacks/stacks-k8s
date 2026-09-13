// Package identity provides the public encodings used by Stacks regtest identities.
package identity

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"math/big"
	"strings"

	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	//nolint:gosec,staticcheck // HASH160 is required by the Stacks and Bitcoin consensus address format.
	"golang.org/x/crypto/ripemd160"
)

// Public contains public Stacks and Bitcoin representations of one key.
type Public struct {
	// Address is the testnet P2PKH Stacks address.
	Address string `json:"address"`
	// PublicKey is the compressed consensus/P2P key.
	PublicKey string `json:"publicKey"`
	// MiningPublicKey is the uncompressed Stacks miner encoding.
	MiningPublicKey string `json:"miningPublicKey"`
	// BitcoinAddress uses the compressed key.
	BitcoinAddress string `json:"bitcoinAddress"`
	// MiningAddress matches the uncompressed Stacks miner key.
	MiningAddress string `json:"miningAddress"`
}

// Generate returns a cryptographically generated private scalar in hex.
func Generate() (string, error) {
	k, err := secp256k1.GeneratePrivateKey()
	if err != nil {
		return "", errors.New("generate secp256k1 identity")
	}
	defer k.Zero()
	return hex.EncodeToString(k.Serialize()), nil
}

// FromPrivate validates a scalar and returns only its public encodings.
// A trailing 01 compression marker is accepted for existing Stacks key files.
func FromPrivate(value string) (Public, error) {
	if len(value) == 66 && strings.HasSuffix(value, "01") {
		value = value[:64]
	}
	raw, err := hex.DecodeString(value)
	if err != nil || len(raw) != 32 {
		return Public{}, errors.New("private key must encode a 32-byte scalar")
	}
	defer clear(raw)
	var scalar secp256k1.ModNScalar
	defer scalar.Zero()
	if scalar.SetByteSlice(raw) || scalar.IsZero() {
		return Public{}, errors.New("private scalar is out of range")
	}
	k := secp256k1.NewPrivateKey(&scalar)
	defer k.Zero()
	return public(k.PubKey()), nil
}

// FromPublic validates a compressed public key without private material.
func FromPublic(value string) (Public, error) {
	raw, err := hex.DecodeString(value)
	if err != nil || len(raw) != 33 {
		return Public{}, errors.New("public key must be compressed secp256k1")
	}
	k, err := secp256k1.ParsePubKey(raw)
	if err != nil {
		return Public{}, errors.New("invalid secp256k1 public key")
	}
	return public(k), nil
}

func public(k *secp256k1.PublicKey) Public {
	compressed, uncompressed := k.SerializeCompressed(), k.SerializeUncompressed()
	hash := hash160(compressed)
	// Stacks single-signature testnet version is 26 (T in the c32 alphabet).
	payload := append(append([]byte{}, hash...), checksum(append([]byte{26}, hash...))...)
	return Public{
		Address:         "ST" + encode(payload, "0123456789ABCDEFGHJKMNPQRSTVWXYZ"),
		PublicKey:       hex.EncodeToString(compressed),
		MiningPublicKey: hex.EncodeToString(uncompressed),
		BitcoinAddress:  bitcoin(compressed),
		MiningAddress:   bitcoin(uncompressed),
	}
}

func hash160(b []byte) []byte {
	s := sha256.Sum256(b)
	//nolint:gosec // HASH160 is required by the Stacks and Bitcoin consensus address format.
	h := ripemd160.New() // HASH160 is required by the Stacks/Bitcoin address format.
	_, _ = h.Write(s[:])
	return h.Sum(nil)
}
func checksum(b []byte) []byte { a := sha256.Sum256(b); c := sha256.Sum256(a[:]); return c[:4] }
func bitcoin(key []byte) string {
	b := append([]byte{111}, hash160(key)...)
	return encode(append(b, checksum(b)...), "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz")
}

func encode(b []byte, alphabet string) string {
	n := new(big.Int).SetBytes(b)
	radix := big.NewInt(int64(len(alphabet)))
	mod := new(big.Int)
	out := []byte{}
	for n.Sign() > 0 {
		n.QuoRem(n, radix, mod)
		out = append(out, alphabet[mod.Int64()])
	}
	for _, v := range b {
		if v != 0 {
			break
		}
		out = append(out, alphabet[0])
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return string(out)
}

// FromDescriptor resolves the supported raw public pkh descriptor form.
// Core checksum-bearing, ranged and private descriptors require a later profile.
func FromDescriptor(descriptor string) (Public, string, error) {
	if !strings.HasPrefix(descriptor, "pkh(") || !strings.HasSuffix(descriptor, ")") {
		return Public{}, "", errors.New("descriptor must be raw public pkh(key)")
	}
	raw, err := hex.DecodeString(descriptor[4 : len(descriptor)-1])
	valid := len(raw) == 33 && (raw[0] == 2 || raw[0] == 3) || len(raw) == 65 && raw[0] == 4
	if err != nil || !valid {
		return Public{}, "", errors.New("invalid descriptor public key")
	}
	key, err := secp256k1.ParsePubKey(raw)
	if err != nil {
		return Public{}, "", errors.New("invalid descriptor curve point")
	}
	return public(key), bitcoin(raw), nil
}
