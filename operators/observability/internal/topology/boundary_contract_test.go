package topology

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

type imageIDFixture struct {
	Contract string `json:"contract"`
	Vectors  []struct {
		Input  string `json:"input"`
		Digest string `json:"digest"`
	} `json:"vectors"`
}

func TestRuntimeImageIDsMatchSharedContract(t *testing.T) {
	var fixture imageIDFixture
	loadBoundaryContract(t, "image-id-v1.json", &fixture)
	if fixture.Contract != "network.stacks.org/image-id/v1" || len(fixture.Vectors) == 0 {
		t.Fatalf("image-id contract = %q, vectors = %d", fixture.Contract, len(fixture.Vectors))
	}
	for _, vector := range fixture.Vectors {
		if actual := immutableImageID(vector.Input); actual != vector.Digest {
			t.Errorf("immutableImageID(%q) = %q, want %q", vector.Input, actual, vector.Digest)
		}
	}
}

func loadBoundaryContract(t *testing.T, name string, target any) {
	t.Helper()
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate boundary contract test")
	}
	path := filepath.Clean(filepath.Join(filepath.Dir(source), "..", "..", "..", "..", "contracts", name))
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(content, target); err != nil {
		t.Fatal(err)
	}
}
