package clarity

import (
	"encoding/binary"
	"errors"
	"math/big"
	"unicode/utf8"

	"github.com/cylewitruk-stacks/stacks-k8s/libs/stacks/identity"
)

// Decode consumes exactly one canonical value using default resource limits.
func Decode(raw []byte) (Value, error) { return DecodeWithLimits(raw, DefaultLimits()) }

// DecodeWithLimits rejects truncation, trailing bytes and noncanonical tuples.
func DecodeWithLimits(raw []byte, l Limits) (Value, error) {
	if l.MaxBytes < 1 || l.MaxDepth < 1 || l.MaxDepth > 256 || len(raw) > l.MaxBytes {
		return Value{}, errors.New("exceeded Clarity resource limit")
	}
	d := decoder{raw: raw, limits: l}
	v, err := d.value(1)
	if err != nil {
		return Value{}, err
	}
	if len(d.raw) != 0 {
		return Value{}, errors.New("trailing Clarity bytes")
	}
	return v, nil
}

// decoder owns a bounded unread slice.
type decoder struct {
	// raw contains the unread consensus bytes.
	raw []byte
	// limits bounds recursion and total input.
	limits Limits
}

// take consumes n bytes without allocating.
func (d *decoder) take(n int) ([]byte, error) {
	if n < 0 || n > len(d.raw) {
		return nil, errors.New("truncated Clarity value")
	}
	b := d.raw[:n]
	d.raw = d.raw[n:]
	return b, nil
}

// count reads a bounded uint32 length.
func (d *decoder) count() (int, error) {
	b, e := d.take(4)
	if e != nil {
		return 0, e
	}
	n := uint64(binary.BigEndian.Uint32(b))
	if n > uint64(len(d.raw)) {
		return 0, errors.New("invalid Clarity count")
	}
	// #nosec G115 -- count rejects values larger than len(d.raw), which is representable as int.
	return int(n), nil
}

// name consumes a valid one-byte-length identifier.
func (d *decoder) name() (string, error) {
	b, e := d.take(1)
	if e != nil {
		return "", e
	}
	b, e = d.take(int(b[0]))
	if e != nil {
		return "", e
	}
	if !ValidName(string(b)) {
		return "", errors.New("invalid Clarity name")
	}
	return string(b), nil
}

// value decodes one nested value within the depth limit.
func (d *decoder) value(depth int) (Value, error) {
	if depth > d.limits.MaxDepth {
		return Value{}, errors.New("exceeded Clarity depth limit")
	}
	tag, e := d.take(1)
	if e != nil {
		return Value{}, e
	}
	v := Value{Type: Type(tag[0])}
	switch v.Type {
	case Int, UInt:
		b, e := d.take(16)
		if e != nil {
			return Value{}, e
		}
		v.Integer = new(big.Int).SetBytes(b)
		if v.Type == Int && b[0]&0x80 != 0 {
			v.Integer.Sub(v.Integer, new(big.Int).Lsh(big.NewInt(1), 128))
		}
	case Buffer, ASCII, UTF8:
		n, e := d.count()
		if e != nil {
			return Value{}, e
		}
		b, _ := d.take(n)
		if v.Type == Buffer {
			v.Bytes = append([]byte{}, b...)
		} else {
			if !utf8.Valid(b) {
				return Value{}, errors.New("invalid UTF-8")
			}
			if v.Type == ASCII {
				for _, c := range b {
					if c > 127 {
						return Value{}, errors.New("invalid ASCII")
					}
				}
			}
			v.Text = string(b)
		}
	case True, False, None:
	case StandardPrincipal, ContractPrincipal:
		b, e := d.take(21)
		if e != nil {
			return Value{}, e
		}
		var hash [20]byte
		copy(hash[:], b[1:])
		v.Text, e = identity.EncodeAddress(b[0], hash)
		if e != nil {
			return Value{}, e
		}
		if v.Type == ContractPrincipal {
			name, e := d.name()
			if e != nil {
				return Value{}, e
			}
			if !ValidContractName(name) {
				return Value{}, errors.New("invalid contract name")
			}
			v.Text += "." + name
		}
	case Some, ResponseOK, ResponseErr:
		child, e := d.value(depth + 1)
		if e != nil {
			return Value{}, e
		}
		v.Items = []Value{child}
	case List:
		n, e := d.count()
		if e != nil {
			return Value{}, e
		}
		v.Items = make([]Value, 0, min(n, 32))
		for range n {
			child, e := d.value(depth + 1)
			if e != nil {
				return Value{}, e
			}
			v.Items = append(v.Items, child)
		}
	case Tuple:
		n, e := d.count()
		if e != nil || n > len(d.raw)/3 {
			return Value{}, errors.New("invalid tuple count")
		}
		v.Fields = make(map[string]Value, min(n, 32))
		previous := ""
		for range n {
			name, e := d.name()
			if e != nil {
				return Value{}, e
			}
			if name <= previous {
				return Value{}, errors.New("noncanonical tuple keys")
			}
			previous = name
			child, e := d.value(depth + 1)
			if e != nil {
				return Value{}, e
			}
			v.Fields[name] = child
		}
	default:
		return Value{}, errors.New("unsupported Clarity type")
	}
	return v, nil
}
