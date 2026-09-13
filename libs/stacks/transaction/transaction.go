// Package transaction constructs the supported standard single-signature subset.
package transaction

import (
	"crypto/sha256"
	"crypto/sha512"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"unicode/utf8"

	"github.com/cylewitruk-stacks/stacks-k8s/libs/stacks/clarity"
	"github.com/cylewitruk-stacks/stacks-k8s/libs/stacks/identity"
	"github.com/cylewitruk-stacks/stacks-k8s/libs/stacks/internal/keys"
	"golang.org/x/crypto/ripemd160"
)

// Explicit consensus transaction versions and post-condition modes.
const (
	Mainnet byte = 0
	Testnet byte = 0x80
	Allow   byte = 1
	Deny    byte = 2
)

// Options contains all authorization inputs; no fee or nonce discovery occurs.
type Options struct {
	// Version selects mainnet (0) or testnet (128) serialization.
	Version byte
	// ChainID is the explicit uint32 chain identifier.
	ChainID uint32
	// Nonce is the sender's explicitly owned nonce.
	Nonce uint64
	// Fee is the fee in micro-STX.
	Fee uint64
	// PostConditionMode selects Allow or Deny with an empty post-condition list.
	PostConditionMode byte
	// PrivateKey is a hex scalar; append 01 to authorize with a compressed key.
	PrivateKey string
}

// Transaction is immutable-by-convention serialized consensus data and its TxID.
type Transaction struct {
	// Bytes contains signed consensus bytes.
	Bytes []byte
	// TxID is lowercase SHA512/256 over Bytes, without a prefix.
	TxID string
}

// ID computes the consensus transaction identifier.
func ID(raw []byte) string { digest := sha512.Sum512_256(raw); return hex.EncodeToString(digest[:]) }

// Valid checks a nonempty bounded serialization against its claimed identifier.
func Valid(tx Transaction) bool {
	return len(tx.Bytes) > 0 && len(tx.Bytes) <= 1<<20 && ID(tx.Bytes) == tx.TxID
}

// Transfer signs an STX transfer with a principal recipient and UTF-8 memo of at most 34 bytes.
func Transfer(o Options, recipient string, amount uint64, memo string) (Transaction, error) {
	p, e := clarity.Principal(recipient)
	if e != nil {
		return Transaction{}, e
	}
	b, e := clarity.Encode(p)
	if e != nil {
		return Transaction{}, e
	}
	if len(memo) > 34 || !utf8.ValidString(memo) {
		return Transaction{}, errors.New("invalid transfer memo")
	}
	payload := append([]byte{0}, b...)
	payload = binary.BigEndian.AppendUint64(payload, amount)
	padded := make([]byte, 34)
	copy(padded, memo)
	payload = append(payload, padded...)
	return sign(o, payload)
}

// Call signs a contract invocation without fetching its ABI or performing RPC.
func Call(o Options, address, contract, function string, args []clarity.Value) (Transaction, error) {
	version, hash, e := identity.DecodeAddress(address)
	if e != nil {
		return Transaction{}, e
	}
	if !clarity.ValidContractName(contract) || !clarity.ValidName(function) || len(args) > 1024 {
		return Transaction{}, errors.New("invalid contract call")
	}
	payload := append([]byte{2, version}, hash[:]...)
	payload = appendName(payload, contract)
	payload = appendName(payload, function)
	payload = binary.BigEndian.AppendUint32(payload, uint32(len(args)))
	for _, arg := range args {
		raw, e := clarity.Encode(arg)
		if e != nil {
			return Transaction{}, e
		}
		if len(payload)+len(raw) > 1<<20 {
			return Transaction{}, errors.New("transaction too large")
		}
		payload = append(payload, raw...)
	}
	return sign(o, payload)
}

// Deploy signs a versioned Clarity publication; versions 1 through 6 are supported.
func Deploy(o Options, name, source string, version byte) (Transaction, error) {
	if !clarity.ValidContractName(name) || version < 1 || version > 6 || len(source) == 0 || len(source) > 131072 {
		return Transaction{}, errors.New("invalid contract publication")
	}
	for _, c := range []byte(source) {
		if c > 127 {
			return Transaction{}, errors.New("contract source must be ASCII")
		}
	}
	payload := appendName([]byte{6, version}, name)
	payload = binary.BigEndian.AppendUint32(payload, uint32(len(source)))
	payload = append(payload, source...)
	return sign(o, payload)
}

// appendName appends an already validated identifier.
func appendName(b []byte, s string) []byte { b = append(b, byte(len(s))); return append(b, s...) }

// sign serializes the initial authorization hash and replaces its empty signature.
func sign(o Options, payload []byte) (Transaction, error) {
	if o.Version != Mainnet && o.Version != Testnet || o.PostConditionMode != Allow && o.PostConditionMode != Deny {
		return Transaction{}, errors.New("unsupported transaction options")
	}
	key, compressed, e := keys.Parse(o.PrivateKey)
	if e != nil {
		return Transaction{}, e
	}
	defer key.Zero()
	pub := key.PubKey().SerializeUncompressed()
	encoding := byte(1)
	if compressed {
		pub = key.PubKey().SerializeCompressed()
		encoding = 0
	}
	sha := sha256.Sum256(pub)
	h := ripemd160.New()
	_, _ = h.Write(sha[:])
	b := []byte{o.Version}
	b = binary.BigEndian.AppendUint32(b, o.ChainID)
	b = append(b, 4, 0)
	b = append(b, h.Sum(nil)...)
	b = append(b, make([]byte, 16)...)
	b = append(b, encoding)
	signatureOffset := len(b)
	b = append(b, make([]byte, 65)...)
	b = append(b, 3, o.PostConditionMode, 0, 0, 0, 0)
	b = append(b, payload...)
	if len(b) > 1<<20 {
		return Transaction{}, errors.New("transaction too large")
	}
	initial := sha512.Sum512_256(b)
	pre := append(initial[:], 4)
	pre = binary.BigEndian.AppendUint64(pre, o.Fee)
	pre = binary.BigEndian.AppendUint64(pre, o.Nonce)
	digest := sha512.Sum512_256(pre)
	sig := keys.Sign(key, digest)
	copy(b[signatureOffset:], sig[:])
	binary.BigEndian.PutUint64(b[27:35], o.Nonce)
	binary.BigEndian.PutUint64(b[35:43], o.Fee)
	return Transaction{Bytes: b, TxID: ID(b)}, nil
}
