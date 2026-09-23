package main

import (
	"encoding/json"
	"os"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// The consumer decodes shared bytes through its production wire type, not an operator import.
func TestPreflightWireContract(t *testing.T) {
	data, err := os.ReadFile("../../contracts/investigation-preflight-v1.json")
	if err != nil {
		t.Fatal(err)
	}
	var contract struct {
		TelemetryResource schema.GroupVersionResource                          `json:"telemetryResource"`
		Telemetry         telemetryWire                                        `json:"telemetry"`
		Observer          struct{ ContainerName, ScopeFlag, Namespace string } `json:"observer"`
		ChaosEnrollment   metav1.ObjectMeta                                    `json:"chaosEnrollment"`
	}
	if err := json.Unmarshal(data, &contract); err != nil {
		t.Fatal(err)
	}
	if contract.TelemetryResource != telemetryResource {
		t.Fatal("telemetry resource drift")
	}
	v := contract.Telemetry
	if !v.Status.Admitted || v.Spec.NetworkUID != "12345678-1234-1234-1234-123456789abc" ||
		v.Spec.NetworkName != "network" ||
		len(v.Status.Conditions) != 1 ||
		v.Status.Conditions[0].Type != conditionWorkloadsReady ||
		v.Status.Conditions[0].Status != metav1.ConditionTrue ||
		v.Status.Conditions[0].ObservedGeneration != v.Metadata.Generation ||
		v.Status.Recording == nil ||
		!v.Status.Recording.BackendReady ||
		v.Status.Recording.ObservedGeneration != v.Metadata.Generation ||
		v.Status.Recording.HeartbeatAt.IsZero() ||
		len(v.Status.Recording.Sources) != 1 ||
		v.Status.Recording.Sources[0].Name != "container-resource" ||
		!v.Status.Recording.Sources[0].Available ||
		v.Status.Recording.Sources[0].ObservedAt.IsZero() {
		t.Fatal("telemetry decode drift", v)
	}
	if contract.Observer.ContainerName != managerContainer || contract.Observer.ScopeFlag != watchNamespaceOption ||
		!watchScope(
			[]string{"--" + contract.Observer.ScopeFlag + "=" + contract.Observer.Namespace},
			contract.Observer.Namespace,
		) {
		t.Fatal("observer scope drift")
	}
	if contract.ChaosEnrollment.Labels[chaosProfileLabel] != chaosProfileValue ||
		contract.ChaosEnrollment.Annotations[chaosInjectAnnotation] != chaosInjectValue {
		t.Fatal("namespace enrollment drift")
	}
}
