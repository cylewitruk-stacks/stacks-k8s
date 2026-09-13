package topology

import (
	"context"
	"testing"

	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// changingIdentityReader injects a concurrent update only during the final consistency pass.
type changingIdentityReader struct {
	client.Reader
	rootReads int
	change    func(client.Object)
}

func (r *changingIdentityReader) Get(ctx context.Context, key client.ObjectKey, object client.Object, options ...client.GetOption) error {
	if err := r.Reader.Get(ctx, key, object, options...); err != nil {
		return err
	}
	if _, root := object.(*api.StacksNetwork); root {
		r.rootReads++
		if r.rootReads == 1 {
			return nil
		}
	}
	// Participants arrive from List initially; their first GET is the final pass.
	r.change(object)
	return nil
}

func TestParticipantObservationIgnoresProgressWritesButRejectsIdentityChanges(t *testing.T) {
	tests := []struct {
		name   string
		change func(client.Object)
		reject bool
	}{
		{"root heartbeat", func(o client.Object) {
			if r, ok := o.(*api.StacksNetwork); ok {
				r.ResourceVersion = "new-heartbeat"
				r.Status.Phase = "Initializing"
				r.Status.Conditions = []metav1.Condition{{Type: "Operational", Status: metav1.ConditionFalse, Reason: "Waiting"}}
			}
		}, false},
		{"participant protocol heartbeat", func(o client.Object) {
			if p, ok := o.(*unstructured.Unstructured); ok && p.GetKind() == "StacksNetworkParticipant" {
				p.SetResourceVersion("new-heartbeat")
				_ = unstructured.SetNestedField(p.Object, "2026-09-11T21:00:00Z", "status", "runtime", "protocol", "observedAt")
			}
		}, false},
		{"root generation", func(o client.Object) {
			if r, ok := o.(*api.StacksNetwork); ok {
				r.Generation++
			}
		}, true},
		{"root allocation", func(o client.Object) {
			if r, ok := o.(*api.StacksNetwork); ok {
				r.Status.Identities[0].UID = "replacement"
			}
		}, true},
		{"participant process", func(o client.Object) {
			if p, ok := o.(*unstructured.Unstructured); ok && p.GetKind() == "StacksNetworkParticipant" {
				_ = unstructured.SetNestedField(p.Object, "containerd://replacement", "status", "runtime", "containerID")
			}
		}, true},
		{"participant admission", func(o client.Object) {
			if p, ok := o.(*unstructured.Unstructured); ok && p.GetKind() == "StacksNetworkParticipant" {
				_ = unstructured.SetNestedField(p.Object, "changed", "status", "admission", "policyDigest")
			}
		}, true},
		{"participant owner", func(o client.Object) {
			if p, ok := o.(*unstructured.Unstructured); ok && p.GetKind() == "StacksNetworkParticipant" {
				owners := p.GetOwnerReferences()
				owners[0].UID = "replacement"
				p.SetOwnerReferences(owners)
			}
		}, true},
		{"configuration report", func(o client.Object) {
			if p, ok := o.(*corev1.ConfigMap); ok {
				p.Data["report.json"] = "{}"
			}
		}, true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			f := participantFixture("StacksNode")
			reader := fixtureReader(t, f)
			reader.APIReader = &changingIdentityReader{Reader: reader.APIReader, change: test.change}
			snapshot, err := reader.Observe(t.Context(), testNamespace, "network", "")
			if test.reject {
				if !IsInconclusive(err) {
					t.Fatalf("expected identity rejection, got %v", err)
				}
				return
			}
			if err != nil || len(snapshot.Actors) != 1 || snapshot.Actors[0].ContainerID != "containerd://process" {
				t.Fatalf("heartbeat lost verified identity: %+v %v", snapshot, err)
			}
		})
	}
}

func TestIdentityInputsDoesNotMutateObservedResource(t *testing.T) {
	object := &unstructured.Unstructured{Object: map[string]any{"apiVersion": api.GroupVersion.String(), "kind": "StacksNetworkParticipant", "metadata": map[string]any{"resourceVersion": "original", "uid": "id"}, "status": map[string]any{"execution": map[string]any{"observedAt": "original"}}}}
	if _, err := identityInputs(object); err != nil {
		t.Fatal(err)
	}
	if object.GetResourceVersion() != "original" {
		t.Fatal("projection mutated observed metadata")
	}
	if _, found, _ := unstructured.NestedMap(object.Object, "status", "execution"); !found {
		t.Fatal("projection mutated observed status")
	}
}
