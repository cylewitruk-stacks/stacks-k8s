package canonical

import "testing"

func TestDigestMatchesNetworkInventoryCompatibilityVector(t *testing.T) {
	payload := struct {
		SchemaVersion      string `json:"schemaVersion"`
		ObservedGeneration int64  `json:"observedGeneration"`
		Actors             []any  `json:"actors"`
	}{
		SchemaVersion:      "network.stacks.org/inventory/v1",
		ObservedGeneration: 2,
		Actors:             []any{map[string]any{"name": "follower", "kind": "StacksNode"}},
	}
	digest, err := Digest(payload)
	if err != nil {
		t.Fatal(err)
	}
	if digest != "sha256:5850e99a8ec3ed18cdd188674786b439cce268af3974100bd5d6f50696c312c5" {
		t.Fatalf("digest = %s", digest)
	}
}
