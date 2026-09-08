package topology

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"testing"

	observationv1alpha1 "github.com/cylewitruk-stacks/stacks-k8s/operators/observability/api/v1alpha1"
)

const inventoryContractVersion = "network.stacks.org/inventory/v1"

type inventoryContractFixture struct {
	Contract string          `json:"contract"`
	Digest   string          `json:"digest"`
	Payload  json.RawMessage `json:"payload"`
}

type observedInventoryPayload struct {
	Actors             []observationv1alpha1.ObservedActorIdentity `json:"actors"`
	ObservedGeneration int64                                       `json:"observedGeneration"`
	SchemaVersion      string                                      `json:"schemaVersion"`
}

func TestInventoryDigestMatchesNetworkOperatorCompatibilityVector(t *testing.T) {
	fixture := loadNetworkInventoryContractFixture(t)
	if fixture.Contract != inventoryContractVersion {
		t.Fatalf("contract = %q", fixture.Contract)
	}
	var payload observedInventoryPayload
	if err := json.Unmarshal(fixture.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.SchemaVersion != inventoryContractVersion {
		t.Fatalf("schemaVersion = %q", payload.SchemaVersion)
	}
	digest, err := inventoryDigest(payload.ObservedGeneration, payload.Actors)
	if err != nil {
		t.Fatal(err)
	}
	if digest != fixture.Digest {
		t.Fatalf("inventory digest = %s, want %s", digest, fixture.Digest)
	}
	assertDigestActorContractFields(t, fixture.Payload)
}

func loadNetworkInventoryContractFixture(t *testing.T) inventoryContractFixture {
	t.Helper()
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate inventory contract test")
	}
	path := filepath.Clean(filepath.Join(filepath.Dir(source), "..", "..", "..", "..", "contracts", "inventory-v1.json"))
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var fixture inventoryContractFixture
	if err := json.Unmarshal(content, &fixture); err != nil {
		t.Fatal(err)
	}
	return fixture
}

func assertDigestActorContractFields(t *testing.T, payload json.RawMessage) {
	t.Helper()
	var raw struct {
		Actors []map[string]json.RawMessage `json:"actors"`
	}
	if err := json.Unmarshal(payload, &raw); err != nil {
		t.Fatal(err)
	}
	if len(raw.Actors) != 2 {
		t.Fatalf("compatibility actor count = %d, want 2", len(raw.Actors))
	}
	typeOfActor := reflect.TypeOf(digestActor{})
	for _, actor := range raw.Actors {
		want := make([]string, 0, typeOfActor.NumField())
		for index := 0; index < typeOfActor.NumField(); index++ {
			tag := typeOfActor.Field(index).Tag.Get("json")
			name, options, _ := strings.Cut(tag, ",")
			if name == "" || name == "-" {
				t.Fatalf("field %s has no inventory JSON name", typeOfActor.Field(index).Name)
			}
			if strings.Contains(options, "omitempty") != (name == "role") {
				t.Fatalf("unexpected optionality for inventory field %s", name)
			}
			if name != "role" || string(actor["kind"]) != `"BitcoinNode"` {
				want = append(want, name)
			}
		}
		got := make([]string, 0, len(actor))
		for name := range actor {
			got = append(got, name)
		}
		sort.Strings(got)
		sort.Strings(want)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("inventory actor fields = %v, want %v", got, want)
		}
	}
}
