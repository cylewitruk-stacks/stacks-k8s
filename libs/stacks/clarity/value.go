// Package clarity implements bounded consensus encoding for Clarity values.
package clarity

import (
	"encoding/binary"
	"errors"
	"math/big"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/cylewitruk-stacks/stacks-k8s/libs/stacks/identity"
)

// Type identifies a Clarity consensus value tag.
type Type byte

// Consensus value tags supported by this codec.
const (
	Int Type = iota
	UInt
	Buffer
	True
	False
	StandardPrincipal
	ContractPrincipal
	ResponseOK
	ResponseErr
	None
	Some
	List
	Tuple
	ASCII
	UTF8
)

// Limits bounds bytes and recursive nesting for both encoding and decoding.
type Limits struct {
	// MaxBytes bounds one complete serialized value.
	MaxBytes int
	// MaxDepth includes the root value (depth one).
	MaxDepth int
}

// DefaultLimits bounds values to one MiB and 32 levels.
func DefaultLimits() Limits { return Limits{1 << 20, 32} }

// Value holds one consensus value. Only fields selected by Type are used.
type Value struct {
	// Type is the consensus discriminator.
	Type Type
	// Integer contains signed or unsigned 128-bit arithmetic values.
	Integer *big.Int
	// Bytes contains buffer bytes.
	Bytes []byte
	// Text contains a string or canonical principal.
	Text string
	// Items contains list elements, or exactly one response/optional child.
	Items []Value
	// Fields contains tuple entries, serialized in bytewise lexical order.
	Fields map[string]Value
}

// Uint constructs an unsigned integer from a uint64.
func Uint(n uint64) Value { return Value{Type: UInt, Integer: new(big.Int).SetUint64(n)} }

// Uint128 validates and constructs an unsigned 128-bit value from decimal text.
func Uint128(s string) (Value, error) {
	n, ok := new(big.Int).SetString(s, 10)
	if !ok || n.Sign() < 0 || n.BitLen() > 128 {
		return Value{}, errors.New("invalid uint128")
	}
	return Value{Type: UInt, Integer: n}, nil
}

// Principal constructs and validates an account or contract principal.
func Principal(s string) (Value, error) {
	v := Value{Type: StandardPrincipal, Text: s}
	if strings.Contains(s, ".") {
		v.Type = ContractPrincipal
	}
	_, err := Encode(v)
	return v, err
}

var namePattern = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_\-!?+<>=/*]*$|^[-+=/*]$|^[<>]=?$`)

// ValidName checks the supported Clarity identifier grammar and byte limit.
func ValidName(s string) bool { return len(s) > 0 && len(s) <= 128 && namePattern.MatchString(s) }

// ValidContractName checks contract identifiers (maximum 128 bytes).
func ValidContractName(s string) bool {
	if len(s) == 0 || len(s) > 128 || !((s[0] >= 'A' && s[0] <= 'Z') || (s[0] >= 'a' && s[0] <= 'z')) {
		return false
	}
	for _, c := range []byte(s) {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

// Encode serializes one value with default resource limits.
func Encode(v Value) ([]byte, error) { return EncodeWithLimits(v, DefaultLimits()) }

// EncodeWithLimits serializes a value and rejects invalid or oversized inputs.
func EncodeWithLimits(v Value, l Limits) ([]byte, error) {
	if l.MaxBytes < 1 || l.MaxDepth < 1 || l.MaxDepth > 256 {
		return nil, errors.New("invalid codec limits")
	}
	return encode(nil, v, l, 1)
}

// encode appends a validated value while enforcing the total output budget.
func encode(b []byte, v Value, l Limits, depth int) ([]byte, error) {
	if depth > l.MaxDepth || len(b) >= l.MaxBytes {
		return nil, errors.New("Clarity resource limit")
	}
	b = append(b, byte(v.Type))
	var err error
	appendData := func(data []byte) {
		if len(data) > l.MaxBytes-len(b)-4 {
			err = errors.New("Clarity byte limit")
			return
		}
		b = binary.BigEndian.AppendUint32(b, uint32(len(data)))
		b = append(b, data...)
	}
	switch v.Type {
	case Int, UInt:
		if v.Integer == nil {
			return nil, errors.New("missing Clarity integer")
		}
		n := new(big.Int).Set(v.Integer)
		if v.Type == UInt {
			if n.Sign() < 0 || n.BitLen() > 128 {
				return nil, errors.New("uint128 out of range")
			}
		} else {
			limit := new(big.Int).Lsh(big.NewInt(1), 127)
			if n.Cmp(new(big.Int).Neg(limit)) < 0 || n.Cmp(limit) >= 0 {
				return nil, errors.New("int128 out of range")
			}
			if n.Sign() < 0 {
				n.Add(n, new(big.Int).Lsh(big.NewInt(1), 128))
			}
		}
		raw := make([]byte, 16)
		n.FillBytes(raw)
		b = append(b, raw...)
	case Buffer:
		appendData(v.Bytes)
	case ASCII, UTF8:
		if !utf8.ValidString(v.Text) {
			return nil, errors.New("invalid UTF-8")
		}
		if v.Type == ASCII {
			for _, c := range []byte(v.Text) {
				if c > 127 {
					return nil, errors.New("invalid ASCII")
				}
			}
		}
		appendData([]byte(v.Text))
	case True, False, None:
	case StandardPrincipal, ContractPrincipal:
		parts := strings.Split(v.Text, ".")
		if len(parts) != 1 && len(parts) != 2 || v.Type == StandardPrincipal && len(parts) != 1 || v.Type == ContractPrincipal && len(parts) != 2 {
			return nil, errors.New("invalid principal")
		}
		version, hash, e := identity.DecodeAddress(parts[0])
		if e != nil {
			return nil, e
		}
		b = append(b, version)
		b = append(b, hash[:]...)
		if len(parts) == 2 {
			if !ValidContractName(parts[1]) {
				return nil, errors.New("invalid contract name")
			}
			b = append(b, byte(len(parts[1])))
			b = append(b, parts[1]...)
		}
	case Some, ResponseOK, ResponseErr:
		if len(v.Items) != 1 {
			return nil, errors.New("expected one wrapped value")
		}
		b, err = encode(b, v.Items[0], l, depth+1)
	case List:
		if len(v.Items) > l.MaxBytes {
			return nil, errors.New("list too large")
		}
		b = binary.BigEndian.AppendUint32(b, uint32(len(v.Items)))
		for _, child := range v.Items {
			b, err = encode(b, child, l, depth+1)
			if err != nil {
				return nil, err
			}
		}
	case Tuple:
		if len(v.Fields) > l.MaxBytes/3 {
			return nil, errors.New("tuple too large")
		}
		names := make([]string, 0, len(v.Fields))
		for k := range v.Fields {
			if !ValidName(k) {
				return nil, errors.New("invalid tuple key")
			}
			names = append(names, k)
		}
		sort.Strings(names)
		b = binary.BigEndian.AppendUint32(b, uint32(len(names)))
		for _, k := range names {
			b = append(b, byte(len(k)))
			b = append(b, k...)
			b, err = encode(b, v.Fields[k], l, depth+1)
			if err != nil {
				return nil, err
			}
		}
	default:
		return nil, errors.New("unsupported Clarity type")
	}
	if err != nil {
		return nil, err
	}
	if len(b) > l.MaxBytes {
		return nil, errors.New("Clarity byte limit")
	}
	return b, nil
}
