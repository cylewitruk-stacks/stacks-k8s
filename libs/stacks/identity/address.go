package identity

import (
	"bytes"
	"errors"
	"math/big"
	"strings"
)

const c32 = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// DecodeAddress validates a canonical c32check address and returns its wire form.
func DecodeAddress(address string) (byte, [20]byte, error) {
	var hash [20]byte
	if len(address) < 3 || len(address) > 41 || address[0] != 'S' {
		return 0, hash, errors.New("invalid Stacks address")
	}
	version := strings.IndexByte(c32, address[1])
	if version < 0 {
		return 0, hash, errors.New("invalid address version")
	}
	n := new(big.Int)
	base := big.NewInt(32)
	for _, c := range []byte(address[2:]) {
		digit := strings.IndexByte(c32, c)
		if digit < 0 {
			return 0, hash, errors.New("invalid c32 digit")
		}
		n.Mul(n, base)
		n.Add(n, big.NewInt(int64(digit)))
	}
	raw := n.Bytes()
	if len(raw) > 24 {
		return 0, hash, errors.New("oversized address")
	}
	payload := make([]byte, 24)
	copy(payload[24-len(raw):], raw)
	copy(hash[:], payload[:20])
	if !bytes.Equal(payload[20:], checksum(append([]byte{byte(version)}, hash[:]...))) {
		return 0, hash, errors.New("invalid address checksum")
	}
	canonical, _ := EncodeAddress(byte(version), hash)
	if canonical != address {
		return 0, hash, errors.New("noncanonical address")
	}
	return byte(version), hash, nil
}

// EncodeAddress encodes an explicit five-bit version and 20-byte address hash.
func EncodeAddress(version byte, hash [20]byte) (string, error) {
	if version >= 32 {
		return "", errors.New("invalid address version")
	}
	payload := append([]byte{}, hash[:]...)
	payload = append(payload, checksum(append([]byte{version}, hash[:]...))...)
	return "S" + string(c32[version]) + encode(payload, c32), nil
}

// CompressedPrivateKey validates a private scalar and returns its explicit Stacks
// compressed-key encoding, matching the addresses returned by FromPrivate.
func CompressedPrivateKey(value string) (string, error) {
	if _, err := FromPrivate(value); err != nil {
		return "", err
	}
	return strings.ToLower(value[:64]) + "01", nil
}
