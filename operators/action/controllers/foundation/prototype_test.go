package foundation

import (
	"testing"

	action "github.com/cylewitruk-stacks/stacks-k8s/apis/network/actions/v1alpha2"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// TestActionPrototypeIsolation pins each status view to a fresh supported object.
func TestActionPrototypeIsolation(t *testing.T) {
	for _, prototype := range []client.Object{
		&action.BitcoinBlockGeneration{},
		&action.BitcoinReorganization{},
	} {
		r := &Reconciler{Prototype: prototype}
		first, status, err := r.object()
		if err != nil {
			t.Fatal(err)
		}
		first.SetName("fetched")
		status.Phase = "Active"
		switch object := first.(type) {
		case *action.BitcoinBlockGeneration:
			if object.Status.Phase != "Active" {
				t.Fatal("generation status is detached")
			}
		case *action.BitcoinReorganization:
			if object.Status.Phase != "Active" {
				t.Fatal("reorganization status is detached")
			}
		default:
			t.Fatalf("unexpected object %T", first)
		}
		next, nextStatus, err := r.object()
		if err != nil {
			t.Fatal(err)
		}
		if first == next || next == prototype || next.GetName() != "" || nextStatus.Phase != "" {
			t.Fatal("prototype or fetched state reused")
		}
	}
}

// TestActionPrototypeRejectsUnsupportedTypes keeps scheme registration separate from authority.
func TestActionPrototypeRejectsUnsupportedTypes(t *testing.T) {
	for _, prototype := range []client.Object{
		nil,
		(*action.BitcoinBlockGeneration)(nil),
		(*action.BitcoinReorganization)(nil),
		&corev1.Pod{},
	} {
		r := &Reconciler{Prototype: prototype}
		if object, status, err := r.object(); err == nil || object != nil || status != nil {
			t.Fatalf("accepted %T", prototype)
		}
	}
}
