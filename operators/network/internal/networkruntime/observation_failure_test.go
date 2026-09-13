package networkruntime

import (
	"context"
	"errors"
	"reflect"
	"testing"

	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// unavailableInventory injects an API outage before any runtime observations can be read.
type unavailableInventory struct{ client.Client }

func (c unavailableInventory) List(context.Context, client.ObjectList, ...client.ListOption) error {
	return errors.New("injected inventory outage")
}

func TestInventoryFailurePreservesKnownLifecycleFacts(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		mutate               func(*api.StacksNetwork)
		operational, running metav1.ConditionStatus
		reason               string
	}{
		{
			"initialized",
			func(*api.StacksNetwork) {},
			metav1.ConditionUnknown,
			metav1.ConditionTrue,
			"ObservationUnavailable",
		},
		{
			"paused",
			func(r *api.StacksNetwork) { r.Spec.Operation = "Paused" },
			metav1.ConditionFalse,
			metav1.ConditionFalse,
			"DesiredPause",
		},
		{
			"stopped",
			func(r *api.StacksNetwork) { r.Spec.Operation = "Stopped" },
			metav1.ConditionFalse,
			metav1.ConditionFalse,
			"Stopped",
		},
		{
			"failed",
			func(r *api.StacksNetwork) { r.Status.Phase = "Failed" },
			metav1.ConditionFalse,
			metav1.ConditionFalse,
			"ExperimentFailed",
		},
		{
			"deleting",
			func(r *api.StacksNetwork) { now := metav1.Now(); r.DeletionTimestamp = &now },
			metav1.ConditionFalse,
			metav1.ConditionFalse,
			"Deleting",
		},
		{"initializing", func(r *api.StacksNetwork) {
			set(r, "Initialized", metav1.ConditionFalse, "BootstrapPending", "pending")
		}, metav1.ConditionFalse, metav1.ConditionFalse, "Initializing"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := rootFixture()
			root.Spec.Operation = "Running"
			set(root, "Initialized", metav1.ConditionTrue, "ProtocolInitialized", "frozen bootstrap completed")
			tc.mutate(root)
			before := *meta.FindStatusCondition(root.Status.Conditions, "Initialized")
			root.Generation++
			if before.Status == metav1.ConditionTrue {
				before.ObservedGeneration = root.Generation
			}
			r := fixtureReconciler(t)
			r.Client = unavailableInventory{r.Client}
			if _, err := r.Reconcile(context.Background(), root); err == nil {
				t.Fatal("inventory failure was hidden")
			}
			if got := meta.FindStatusCondition(
				root.Status.Conditions,
				"Initialized",
			); !reflect.DeepEqual(
				*got,
				before,
			) {
				t.Fatalf("initialization history changed: %+v", got)
			}
			for kind, want := range map[string]metav1.ConditionStatus{
				"Running":     tc.running,
				"Operational": tc.operational,
			} {
				if got := meta.FindStatusCondition(
					root.Status.Conditions,
					kind,
				); got == nil || got.Status != want ||
					got.ObservedGeneration != root.Generation {
					t.Fatalf("%s = %+v, want %s at current generation", kind, got, want)
				}
			}
			if got := meta.FindStatusCondition(root.Status.Conditions, "Operational"); got.Reason != tc.reason {
				t.Fatalf("operational reason %s, want %s", got.Reason, tc.reason)
			}
		})
	}
}
