// Package naming owns stable Kubernetes names for compiled topology objects.
package naming

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

const maximumNameLength = 63

// Child returns a stable DNS-label name for one aggregate actor.
func Child(network, actor string) string {
	value := network + "-" + actor
	if len(value) <= maximumNameLength {
		return value
	}
	digest := sha256.Sum256([]byte(value))
	suffix := "-" + hex.EncodeToString(digest[:4])
	prefix := strings.TrimRight(value[:maximumNameLength-len(suffix)], "-")
	return prefix + suffix
}

// Resource appends a stable suffix while preserving the DNS-label limit.
func Resource(owner, suffix string) string {
	return Child(owner, suffix)
}
