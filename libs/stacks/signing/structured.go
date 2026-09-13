// Package signing implements SIP-018 and supported PoX signer authorizations.
package signing

import (
	"crypto/sha256"
	"errors"

	"github.com/cylewitruk-stacks/stacks-k8s/libs/stacks/clarity"
	"github.com/cylewitruk-stacks/stacks-k8s/libs/stacks/internal/keys"
)

// StructuredDigest hashes a validated SIP-018 domain and message.
func StructuredDigest(domain, message clarity.Value) ([32]byte, error) {
	if domain.Type != clarity.Tuple {
		return [32]byte{}, errors.New("SIP-018 domain must be a tuple")
	}
	for k, t := range map[string]clarity.Type{"name": clarity.ASCII, "version": clarity.ASCII, "chain-id": clarity.UInt} {
		v, ok := domain.Fields[k]
		if !ok || v.Type != t {
			return [32]byte{}, errors.New("invalid SIP-018 domain")
		}
	}
	d, e := clarity.Encode(domain)
	if e != nil {
		return [32]byte{}, e
	}
	m, e := clarity.Encode(message)
	if e != nil {
		return [32]byte{}, e
	}
	dh, mh := sha256.Sum256(d), sha256.Sum256(m)
	encoded := append([]byte("SIP018"), dh[:]...)
	encoded = append(encoded, mh[:]...)
	return sha256.Sum256(encoded), nil
}

// Structured returns a deterministic 65-byte RSV signature for Clarity verification.
func Structured(privateKey string, domain, message clarity.Value) ([65]byte, error) {
	digest, e := StructuredDigest(domain, message)
	if e != nil {
		return [65]byte{}, e
	}
	key, _, e := keys.Parse(privateKey)
	if e != nil {
		return [65]byte{}, e
	}
	defer key.Zero()
	vrs := keys.Sign(key, digest)
	var rsv [65]byte
	copy(rsv[:64], vrs[1:])
	rsv[64] = vrs[0]
	return rsv, nil
}

// Domain creates the standard SIP-018 domain fields using an explicit chain ID.
func Domain(name, version string, chainID uint32) clarity.Value {
	return clarity.Value{Type: clarity.Tuple, Fields: map[string]clarity.Value{"name": {Type: clarity.ASCII, Text: name}, "version": {Type: clarity.ASCII, Text: version}, "chain-id": clarity.Uint(uint64(chainID))}}
}

// PoXAddress is an already resolved PoX reward-address wire representation.
type PoXAddress struct {
	// Version is the PoX address mode (0..6), not the Bitcoin base58 version.
	Version byte
	// HashBytes contains 20 bytes for modes 0..4 or 32 bytes for modes 5..6.
	HashBytes []byte
}

// Value validates and encodes a PoX reward-address tuple.
func (a PoXAddress) Value() (clarity.Value, error) {
	length := 20
	if a.Version >= 5 {
		length = 32
	}
	if a.Version > 6 || len(a.HashBytes) != length {
		return clarity.Value{}, errors.New("unsupported PoX reward address")
	}
	return clarity.Value{Type: clarity.Tuple, Fields: map[string]clarity.Value{"version": {Type: clarity.Buffer, Bytes: []byte{a.Version}}, "hashbytes": {Type: clarity.Buffer, Bytes: append([]byte{}, a.HashBytes...)}}}, nil
}

// PoXAuthorization contains the explicit PoX-4 structured-message fields.
type PoXAuthorization struct {
	// Address is the resolved reward address.
	Address PoXAddress
	// RewardCycle is the authorization's reward cycle.
	RewardCycle uint64
	// Topic is the exact supported contract operation.
	Topic string
	// Period is the authorized lock period.
	Period uint64
	// MaxAmount is the uint128 maximum amount.
	MaxAmount clarity.Value
	// AuthID is the uint128 authorization identifier.
	AuthID clarity.Value
	// ChainID selects the structured signature domain's chain.
	ChainID uint32
}

// PoX signs an explicit PoX-4 authorization without enrollment policy or RPC.
func PoX(privateKey string, a PoXAuthorization) ([65]byte, error) {
	switch a.Topic {
	case TopicStackSTX, TopicStackExtend, TopicStackIncrease, TopicAggregateCommit, TopicAggregateIncrease:
	default:
		return [65]byte{}, errors.New("unsupported PoX topic")
	}
	if a.MaxAmount.Type != clarity.UInt || a.AuthID.Type != clarity.UInt {
		return [65]byte{}, errors.New("PoX amounts require uint128")
	}
	address, e := a.Address.Value()
	if e != nil {
		return [65]byte{}, e
	}
	message := clarity.Value{Type: clarity.Tuple, Fields: map[string]clarity.Value{"pox-addr": address, "reward-cycle": clarity.Uint(a.RewardCycle), "topic": {Type: clarity.ASCII, Text: a.Topic}, "period": clarity.Uint(a.Period), "max-amount": a.MaxAmount, "auth-id": a.AuthID}}
	return Structured(privateKey, Domain("pox-4-signer", "1.0.0", a.ChainID), message)
}

// SignerGrant signs the PoX-5 direct-manager grant-authorization message.
func SignerGrant(privateKey, manager string, authID clarity.Value, chainID uint32) ([65]byte, error) {
	principal, e := clarity.Principal(manager)
	if e != nil {
		return [65]byte{}, e
	}
	if authID.Type != clarity.UInt {
		return [65]byte{}, errors.New("grant auth ID requires uint128")
	}
	message := clarity.Value{Type: clarity.Tuple, Fields: map[string]clarity.Value{"signer-manager": principal, "topic": {Type: clarity.ASCII, Text: TopicGrantAuthorization}, "auth-id": authID}}
	return Structured(privateKey, Domain("pox-5-signer", "1.0.0", chainID), message)
}
