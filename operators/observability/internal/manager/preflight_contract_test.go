package manager

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"testing"

	api "github.com/cylewitruk-stacks/stacks-k8s/operators/observability/api/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/observability/internal/recorder"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// Verify independent consumer bytes against the served API type and real flag parser.
func TestInvestigationPreflightContract(t *testing.T) {
	data, err := os.ReadFile("../../../../contracts/investigation-preflight-v1.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		TelemetryResource schema.GroupVersionResource           `json:"telemetryResource"`
		Telemetry         json.RawMessage                       `json:"telemetry"`
		Observer          struct{ ScopeFlag, Namespace string } `json:"observer"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	var v api.NetworkTelemetry
	decoder := json.NewDecoder(bytes.NewReader(fixture.Telemetry))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&v); err != nil {
		t.Fatal("fixture contains fields absent from the served telemetry type:", err)
	}
	if fixture.TelemetryResource != api.GroupVersion.WithResource(api.ResourceNetworkTelemetries) ||
		v.APIVersion != api.GroupVersion.String() ||
		v.Kind != api.KindNetworkTelemetry ||
		v.Spec.NetworkName != "network" ||
		string(v.Spec.NetworkUID) != "12345678-1234-1234-1234-123456789abc" ||
		len(v.Status.Conditions) != 1 ||
		v.Status.Conditions[0].Type != api.ConditionWorkloadsReady ||
		!v.Status.Admitted ||
		v.Status.Recording == nil ||
		!v.Status.Recording.BackendReady ||
		v.Status.Recording.ObservedGeneration != v.Generation ||
		v.Status.Recording.HeartbeatAt.IsZero() ||
		len(v.Status.Recording.Sources) != 1 ||
		v.Status.Recording.Sources[0].Name != recorder.SourceContainerResource ||
		!v.Status.Recording.Sources[0].Available ||
		v.Status.Recording.Sources[0].ObservedAt.IsZero() {
		t.Fatal("served telemetry contract drift")
	}
	opts := Options{}
	flags := flag.NewFlagSet("contract", flag.ContinueOnError)
	opts.Bind(flags)
	if err := flags.Parse([]string{"--" + fixture.Observer.ScopeFlag + "=" + fixture.Observer.Namespace}); err != nil {
		t.Fatal(err)
	}
	namespaces, err := opts.enrolledNamespaces()
	if err != nil || len(namespaces) != 1 {
		t.Fatal(namespaces, err)
	}
	if _, ok := namespaces[fixture.Observer.Namespace]; !ok {
		t.Fatal("watch boundary drift")
	}
}
