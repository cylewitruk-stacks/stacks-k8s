package stacksworker

import (
	"reflect"
	"testing"

	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// TestNamedWatchFiltersObservationFeedback preserves immediate control without self-triggered RPC loops.
func TestNamedWatchFiltersObservationFeedback(t *testing.T) {
	base := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": api.GroupVersion.String(), "kind": "StacksNetworkParticipant",
		"metadata": map[string]any{"name": "worker", "uid": "original", "generation": int64(1), "resourceVersion": "1"},
		"spec":     map[string]any{"control": map[string]any{"paused": false}},
		"status": map[string]any{
			"admission": map[string]any{"policyDigest": "original"},
			"runtime": map[string]any{
				"podRef":   map[string]any{"uid": "pod"},
				"protocol": map[string]any{"observedAt": "old", "stacksHeight": int64(1)},
			},
			"execution": map[string]any{"observedAt": "old", "pending": int64(1)},
		},
	}}
	for _, tc := range []struct {
		name  string
		path  []string
		value any
		want  bool
	}{
		{"execution heartbeat", []string{"status", "execution", "observedAt"}, "new", false},
		{"execution receipt", []string{"status", "execution", "pending"}, int64(0), false},
		{"native observation", []string{"status", "runtime", "protocol", "stacksHeight"}, int64(2), false},
		{"resource version", []string{"metadata", "resourceVersion"}, "2", false},
		{"replacement", []string{"metadata", "uid"}, "new", true},
		{"deletion", []string{"metadata", "deletionTimestamp"}, "now", true},
		{"pause", []string{"spec", "control", "paused"}, true, true},
		{"admission", []string{"status", "admission", "policyDigest"}, "new", true},
		{"runtime replacement", []string{"status", "runtime", "podRef", "uid"}, "new", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before, after := base.DeepCopy(), base.DeepCopy()
			if err := unstructured.SetNestedField(after.Object, tc.value, tc.path...); err != nil {
				t.Fatal(err)
			}
			untouched := after.DeepCopy()
			if got := namedUpdateRelevant(before, after); got != tc.want {
				t.Fatalf("notification = %v, want %v", got, tc.want)
			}
			if !reflect.DeepEqual(before, base) || !reflect.DeepEqual(after, untouched) {
				t.Fatal("mutated informer inputs")
			}
		})
	}
	root := base.DeepCopy()
	root.SetKind("StacksNetwork")
	for _, path := range [][]string{{"status", "phase"}, {"status", "identities"}, {"status", "initialization"}} {
		changed := root.DeepCopy()
		if err := unstructured.SetNestedField(changed.Object, "changed", path...); err != nil {
			t.Fatal(err)
		}
		if !namedUpdateRelevant(root, changed) {
			t.Fatal("root lifecycle notification filtered")
		}
	}
}
