package signing

import (
	"encoding/hex"
	"errors"

	"github.com/cylewitruk-stacks/stacks-k8s/libs/stacks/clarity"
	"github.com/cylewitruk-stacks/stacks-k8s/libs/stacks/identity"
)

// SignerArguments holds the explicit PoX-4 signer authorization call suffix.
type SignerArguments struct {
	// Signature is an RSV signature, or nil for an existing on-chain authorization.
	Signature []byte
	// PublicKey is the compressed consensus signer key.
	PublicKey string
	// MaxAmount is the uint128 authorization ceiling.
	MaxAmount clarity.Value
	// AuthID is the uint128 authorization identifier.
	AuthID clarity.Value
}

// values validates and serializes the signer authorization argument suffix.
func (s SignerArguments) values() ([]clarity.Value, error) {
	if _, e := identity.FromPublic(s.PublicKey); e != nil {
		return nil, e
	}
	if s.MaxAmount.Type != clarity.UInt || s.AuthID.Type != clarity.UInt {
		return nil, errors.New("PoX signer amounts require uint128")
	}
	if _, e := clarity.Encode(s.MaxAmount); e != nil {
		return nil, e
	}
	if _, e := clarity.Encode(s.AuthID); e != nil {
		return nil, e
	}
	signature := clarity.Value{Type: clarity.None}
	if s.Signature != nil {
		if len(s.Signature) != 65 || s.Signature[64] > 3 {
			return nil, errors.New("invalid PoX RSV signature")
		}
		signature = clarity.Value{
			Type:  clarity.Some,
			Items: []clarity.Value{{Type: clarity.Buffer, Bytes: append([]byte{}, s.Signature...)}},
		}
	}
	public, _ := hex.DecodeString(s.PublicKey)
	return []clarity.Value{signature, {Type: clarity.Buffer, Bytes: public}, s.MaxAmount, s.AuthID}, nil
}

// StackSTXArguments constructs the PoX-4 stack-stx argument order.
func StackSTXArguments(
	amount clarity.Value,
	address PoXAddress,
	burnHeight, cycles uint64,
	signer SignerArguments,
) ([]clarity.Value, error) {
	if amount.Type != clarity.UInt {
		return nil, errors.New("stack amount requires uint128")
	}
	if _, e := clarity.Encode(amount); e != nil {
		return nil, e
	}
	pox, e := address.Value()
	if e != nil {
		return nil, e
	}
	suffix, e := signer.values()
	if e != nil {
		return nil, e
	}
	return append([]clarity.Value{amount, pox, clarity.Uint(burnHeight), clarity.Uint(cycles)}, suffix...), nil
}

// StackExtendArguments constructs the PoX-4 stack-extend argument order.
func StackExtendArguments(address PoXAddress, cycles uint64, signer SignerArguments) ([]clarity.Value, error) {
	pox, e := address.Value()
	if e != nil {
		return nil, e
	}
	suffix, e := signer.values()
	if e != nil {
		return nil, e
	}
	return append([]clarity.Value{clarity.Uint(cycles), pox}, suffix...), nil
}
