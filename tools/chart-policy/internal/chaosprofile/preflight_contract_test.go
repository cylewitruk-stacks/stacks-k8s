package chaosprofile

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// Verify both namespace enrollment guards, including UPDATE cleanup semantics.
func TestPreflightEnrollmentContract(t *testing.T) {
	data, err := os.ReadFile("../../../../contracts/investigation-preflight-v1.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		ChaosEnrollment metav1.ObjectMeta `json:"chaosEnrollment"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.ChaosEnrollment.Labels) != 1 || len(fixture.ChaosEnrollment.Annotations) != 1 {
		t.Fatal("expected one label and annotation")
	}
	var label, value, annotation, annotationValue string
	for label, value = range fixture.ChaosEnrollment.Labels {
		break
	}
	for annotation, annotationValue = range fixture.ChaosEnrollment.Annotations {
		break
	}
	want := fmt.Sprintf(
		"oldObject != null || (has(namespaceObject.metadata.labels) && "+
			"'%s' in namespaceObject.metadata.labels && namespaceObject.metadata.labels['%s'] == '%s' && "+
			"has(namespaceObject.metadata.annotations) && '%s' in namespaceObject.metadata.annotations && "+
			"namespaceObject.metadata.annotations['%s'] == '%s')",
		label,
		label,
		value,
		annotation,
		annotation,
		annotationValue,
	)
	cmd := exec.CommandContext(
		t.Context(),
		"helm",
		"template",
		"test",
		"../../../../charts/stacks-chaos-profile",
		"--set",
		"networkDelay.enabled=true",
		"--set",
		"chaosMesh.externalVersion="+Version,
	)
	rendered, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	objects, err := Decode(bytes.NewReader(rendered))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, object := range objects {
		if object.GetKind() != "ValidatingAdmissionPolicy" {
			continue
		}
		validations, _, _ := unstructured.NestedSlice(object.Object, "spec", "validations")
		for _, v := range validations {
			expression, _, _ := unstructured.NestedString(v.(map[string]any), "expression")
			if strings.Join(strings.Fields(expression), " ") == want {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("rendered enrollment predicate differs from preflight contract")
	}
}
