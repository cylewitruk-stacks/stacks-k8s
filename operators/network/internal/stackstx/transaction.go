// Package stackstx defines public Stacks transaction identity independently of orchestration.
package stackstx

import (
	"crypto/sha512"
	"encoding/hex"
)

// Transaction contains public signed bytes and their independently checked identity.
type Transaction struct {
	// TxID is the SHA512/256 digest of Bytes.
	TxID string `json:"txid"`
	// Bytes contains hex-encoded consensus serialization.
	Bytes string `json:"bytes"`
}

// Inclusion reports exact canonical transaction execution at observation time.
type Inclusion struct {
	// Source distinguishes the native index from a validated legacy execution event.
	Source string
	// Found reports exact canonical membership.
	Found bool
	// Success is the contract response outcome.
	Success bool
	// BlockID identifies the canonical inclusion block.
	BlockID string
}

// Valid checks bounded consensus bytes independently of the SDK.
func Valid(tx Transaction) bool {
	raw, err := hex.DecodeString(tx.Bytes)
	if err != nil || len(raw) < 100 || len(raw) > 256*1024 {
		return false
	}
	hash := sha512.Sum512_256(raw)
	return tx.TxID == hex.EncodeToString(hash[:])
}
