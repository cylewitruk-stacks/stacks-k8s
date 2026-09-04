package workload

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	networkv1alpha1 "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha1"
)

type leafSpecFixture struct {
	Contract string `json:"contract"`
	Vectors  []struct {
		ID     string          `json:"id"`
		Kind   string          `json:"kind"`
		Digest string          `json:"digest"`
		Spec   json.RawMessage `json:"spec"`
	} `json:"vectors"`
}

type imageIDFixture struct {
	Contract string `json:"contract"`
	Vectors  []struct {
		Input  string `json:"input"`
		Digest string `json:"digest"`
	} `json:"vectors"`
}

func TestLeafSpecificationDigestsMatchSharedContract(t *testing.T) {
	var fixture leafSpecFixture
	loadContract(t, "leaf-spec-v1.json", &fixture)
	if fixture.Contract != "network.stacks.org/leaf-spec/v1" || len(fixture.Vectors) != 7 {
		t.Fatalf("leaf-spec contract = %q, vectors = %d", fixture.Contract, len(fixture.Vectors))
	}
	for _, vector := range fixture.Vectors {
		t.Run(vector.ID, func(t *testing.T) {
			var spec any
			switch vector.Kind {
			case "BitcoinNode":
				spec = &networkv1alpha1.BitcoinNodeSpec{}
			case "StacksNode":
				spec = &networkv1alpha1.StacksNodeSpec{}
			case "StacksSigner":
				spec = &networkv1alpha1.StacksSignerSpec{}
			default:
				t.Fatalf("unknown leaf kind %q", vector.Kind)
			}
			if err := json.Unmarshal(vector.Spec, spec); err != nil {
				t.Fatal(err)
			}
			digest, err := SpecDigest(spec)
			if err != nil {
				t.Fatal(err)
			}
			if digest != vector.Digest {
				t.Fatalf("digest = %s, want %s", digest, vector.Digest)
			}
		})
	}
}

func TestRuntimeImageIDsMatchSharedContract(t *testing.T) {
	var fixture imageIDFixture
	loadContract(t, "image-id-v1.json", &fixture)
	if fixture.Contract != "network.stacks.org/image-id/v1" || len(fixture.Vectors) == 0 {
		t.Fatalf("image-id contract = %q, vectors = %d", fixture.Contract, len(fixture.Vectors))
	}
	for _, vector := range fixture.Vectors {
		if actual := immutableImageID(vector.Input); actual != vector.Digest {
			t.Errorf("immutableImageID(%q) = %q, want %q", vector.Input, actual, vector.Digest)
		}
	}
}

func loadContract(t *testing.T, name string, target any) {
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
