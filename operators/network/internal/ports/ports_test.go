package ports

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	networkv1alpha1 "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha1"
)

func TestProductionPortsMatchSharedContract(t *testing.T) {
	var fixture struct {
		Contract string `json:"contract"`
		Vectors  []struct {
			Kind    string                     `json:"kind"`
			RPCPort int32                      `json:"rpcPort"`
			P2PPort int32                      `json:"p2pPort"`
			Ports   []networkv1alpha1.PortSpec `json:"ports"`
		} `json:"vectors"`
	}
	loadContract(t, "actor-ports-v1.json", &fixture)
	if fixture.Contract != "network.stacks.org/actor-ports/v1" || len(fixture.Vectors) != 3 {
		t.Fatalf("actor-port contract = %q, vectors = %d", fixture.Contract, len(fixture.Vectors))
	}
	for _, vector := range fixture.Vectors {
		var actual []networkv1alpha1.PortSpec
		switch vector.Kind {
		case "BitcoinNode":
			actual = Bitcoin(vector.RPCPort, vector.P2PPort)
		case "StacksNode":
			actual = StacksNode()
		case "StacksSigner":
			actual = StacksSigner()
		default:
			t.Fatalf("unknown actor kind %q", vector.Kind)
		}
		if !reflect.DeepEqual(actual, vector.Ports) {
			t.Errorf("%s ports = %#v, want %#v", vector.Kind, actual, vector.Ports)
		}
	}
}

func loadContract(t *testing.T, name string, target any) {
	t.Helper()
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate actor-port contract test")
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
