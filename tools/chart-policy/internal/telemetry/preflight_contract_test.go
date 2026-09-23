package telemetry

import (
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// Check emitted chart fields against the independent preflight consumer contract.
func TestPreflightObserverContract(t *testing.T) {
	data, err := os.ReadFile("../../../../contracts/investigation-preflight-v1.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Observer          struct{ ContainerName, ScopeFlag, Namespace string } `json:"observer"`
		TelemetryResource struct{ Group, Version, Resource string }            `json:"telemetryResource"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	rendered, err := render(t, "--include-crds", "--set", "watchNamespaces[0]="+fixture.Observer.Namespace)
	if err != nil {
		t.Fatal(err)
	}
	deploymentFound, crdFound := false, false
	for _, object := range objects(t, rendered) {
		switch object.GetKind() {
		case "Deployment":
			var d appsv1.Deployment
			decode(t, object, &d)
			for _, c := range d.Spec.Template.Spec.Containers {
				if c.Name == fixture.Observer.ContainerName {
					for _, arg := range c.Args {
						if !strings.HasPrefix(arg, "--") || !strings.Contains(arg, "=") {
							t.Fatalf("manager argument %q is unsupported by preflight", arg)
						}
					}
					deploymentFound = deploymentFound || slices.Contains(
						c.Args,
						"--"+fixture.Observer.ScopeFlag+"="+fixture.Observer.Namespace,
					)
				}
			}
		case "CustomResourceDefinition":
			group, _, _ := unstructured.NestedString(object.Object, "spec", "group")
			plural, _, _ := unstructured.NestedString(object.Object, "spec", "names", "plural")
			if group != fixture.TelemetryResource.Group || plural != fixture.TelemetryResource.Resource {
				continue
			}
			versions, _, _ := unstructured.NestedSlice(object.Object, "spec", "versions")
			for _, version := range versions {
				v := version.(map[string]any)
				if v["name"] == fixture.TelemetryResource.Version && v["served"] == true {
					crdFound = true
				}
			}
		}
	}
	if !deploymentFound || !crdFound {
		t.Fatalf("observer=%v served telemetry=%v", deploymentFound, crdFound)
	}
}
