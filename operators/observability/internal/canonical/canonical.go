// Package canonical provides deterministic JSON hashing for cross-operator contracts.
package canonical

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

const maximumJSONSafeInteger int64 = 9_007_199_254_740_991

// Digest returns the SHA-256 digest of recursively key-sorted JSON.
func Digest(value any) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("marshal canonical input: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	var normalized any
	if err := decoder.Decode(&normalized); err != nil {
		return "", fmt.Errorf("decode canonical input: %w", err)
	}
	if err := validate(normalized, "$"); err != nil {
		return "", err
	}
	encoded, err = json.Marshal(normalized)
	if err != nil {
		return "", fmt.Errorf("marshal canonical JSON: %w", err)
	}
	sum := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func validate(value any, path string) error {
	switch typed := value.(type) {
	case nil, bool, string:
		return nil
	case json.Number:
		integer, err := typed.Int64()
		if err != nil {
			return fmt.Errorf("%s must contain integers only: %w", path, err)
		}
		if integer < -maximumJSONSafeInteger || integer > maximumJSONSafeInteger {
			return fmt.Errorf("%s exceeds the JSON safe-integer range", path)
		}
		return nil
	case []any:
		for index, item := range typed {
			if err := validate(item, fmt.Sprintf("%s[%d]", path, index)); err != nil {
				return err
			}
		}
		return nil
	case map[string]any:
		for key, item := range typed {
			if err := validate(item, path+"."+key); err != nil {
				return err
			}
		}
		return nil
	default:
		return fmt.Errorf("%s contains unsupported canonical JSON type %T", path, value)
	}
}
