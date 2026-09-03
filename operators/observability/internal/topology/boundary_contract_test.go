package topology

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/cylewitruk-stacks/stacks-k8s/operators/observability/internal/canonical"
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

type actorPortsFixture struct {
	Contract string `json:"contract"`
	Vectors  []struct {
		Kind    string `json:"kind"`
		RPCPort int32  `json:"rpcPort,omitempty"`
		P2PPort int32  `json:"p2pPort,omitempty"`
		Ports   []struct {
			Name string `json:"name"`
			Port int32  `json:"port"`
		} `json:"ports"`
	} `json:"vectors"`
}

func TestLeafSpecificationDigestsMatchSharedContract(t *testing.T) {
	var fixture leafSpecFixture
	loadBoundaryContract(t, "leaf-spec-v1.json", &fixture)
	if fixture.Contract != "network.stacks.org/leaf-spec/v1" || len(fixture.Vectors) != 7 {
		t.Fatalf("leaf-spec contract = %q, vectors = %d", fixture.Contract, len(fixture.Vectors))
	}
	for _, vector := range fixture.Vectors {
		t.Run(vector.ID, func(t *testing.T) {
			decoder := json.NewDecoder(bytes.NewReader(vector.Spec))
			decoder.UseNumber()
			var spec map[string]any
			if err := decoder.Decode(&spec); err != nil {
				t.Fatal(err)
			}
			digest, err := canonical.Digest(spec)
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

func TestActorPortsMatchSharedContract(t *testing.T) {
	var fixture actorPortsFixture
	loadBoundaryContract(t, "actor-ports-v1.json", &fixture)
	if fixture.Contract != "network.stacks.org/actor-ports/v1" || len(fixture.Vectors) != 3 {
		t.Fatalf("actor-port contract = %q, vectors = %d", fixture.Contract, len(fixture.Vectors))
	}
	for _, vector := range fixture.Vectors {
		t.Run(vector.Kind, func(t *testing.T) {
			spec := map[string]any{}
			if vector.RPCPort != 0 {
				spec["rpcPort"] = int64(vector.RPCPort)
			}
			if vector.P2PPort != 0 {
				spec["p2pPort"] = int64(vector.P2PPort)
			}
			actual, ok := expectedActorPorts(vector.Kind, spec)
			if !ok {
				t.Fatal("production port resolver rejected contract vector")
			}
			want := make(map[string]int32, len(vector.Ports))
			for _, port := range vector.Ports {
				want[port.Name] = port.Port
			}
			if len(actual) != len(want) {
				t.Fatalf("ports = %v, want %v", actual, want)
			}
			for name, port := range want {
				if actual[name] != port {
					t.Fatalf("port %s = %d, want %d", name, actual[name], port)
				}
			}
		})
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
