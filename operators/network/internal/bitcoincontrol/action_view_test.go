package bitcoincontrol

import (
	"testing"

	action "github.com/cylewitruk-stacks/stacks-k8s/apis/network/actions/v1alpha2"
	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// TestActionViewKeepsConcreteIdentity verifies dispatch never infers a fallback action.
func TestActionViewKeepsConcreteIdentity(t *testing.T) {
	for _, kind := range []action.Kind{"BitcoinBlockGeneration", "BitcoinReorganization"} {
		t.Run(string(kind), func(t *testing.T) {
			object, err := actionObject(kind)
			if err != nil {
				t.Fatal(err)
			}
			view, err := actionFields(object)
			if err != nil {
				t.Fatal(err)
			}
			if view.object != object || view.kind != kind || view.status == nil {
				t.Fatalf("invalid view: %#v", view)
			}
			object.SetName("request")
			object.SetUID("request-uid")
			if ref := view.reference(); ref != (common.Binding{Kind: string(kind), Name: "request", UID: "request-uid"}) {
				t.Fatalf("incorrect serialized reference: %#v", ref)
			}
			view.status.Phase = action.PhaseActive
			switch object := object.(type) {
			case *action.BitcoinBlockGeneration:
				if view.generation != &object.Spec || view.reorganization != nil || object.Status.Phase != "Active" {
					t.Fatal("generation view lost identity or writable status")
				}
			case *action.BitcoinReorganization:
				if view.reorganization != &object.Spec || view.generation != nil || object.Status.Phase != "Active" {
					t.Fatal("reorganization view lost identity or writable status")
				}
			default:
				t.Fatalf("unexpected concrete type %T", object)
			}
		})
	}
}

// TestActionViewRejectsUnknownInputs pins nil and future-kind handling without panics.
func TestActionViewRejectsUnknownInputs(t *testing.T) {
	for _, kind := range []action.Kind{"", "Unknown", "bitcoinblockgeneration"} {
		if object, err := actionObject(kind); err == nil || object != nil {
			t.Fatalf("kind %q returned %T, %v", kind, object, err)
		}
	}
	for _, object := range []client.Object{nil, (*action.BitcoinBlockGeneration)(nil), (*action.BitcoinReorganization)(nil), &corev1.Pod{}} {
		if view, err := actionFields(object); err == nil || view.object != nil || view.status != nil || view.kind != "" {
			t.Fatalf("object %T returned %#v, %v", object, view, err)
		}
	}
}
