package naming

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
)

// RuntimeName isolates instance workloads by full network and participant identity.
func RuntimeName(networkUID, participantUID, kind, name, purpose string) string {
	token := sha256.Sum256([]byte(networkUID))
	prefix := "n-" + hex.EncodeToString(token[:])[:12] + "-" + strings.ToLower(kind) + "-" + name + "-" + purpose
	if len(prefix) > 39 {
		prefix = prefix[:39]
	}
	prefix = strings.TrimRight(prefix, "-")
	encoded, _ := json.Marshal([]string{networkUID, participantUID, kind, name, purpose})
	digest := sha256.Sum256(encoded)
	return prefix + "-" + hex.EncodeToString(digest[:])[:12]
}
