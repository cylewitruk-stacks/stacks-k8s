package execution

import (
	"context"
	"testing"

	core "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func TestBindingRejectsOtherNamespacesNamesAndReplacementUIDs(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := core.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	object := &core.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "same", Namespace: "one", UID: types.UID("original")}}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(object).Build()
	calls := 0
	next := reconcile.Func(func(context.Context, ctrl.Request) (ctrl.Result, error) { calls++; return ctrl.Result{}, nil })
	bound := (Binding{Namespace: "one", Name: "same", UID: "original"}).Wrap(c, &core.ConfigMap{}, next)
	for _, key := range []client.ObjectKey{{Namespace: "two", Name: "same"}, {Namespace: "one", Name: "other"}, {Namespace: "one", Name: "same"}} {
		if _, err := bound.Reconcile(context.Background(), ctrl.Request{NamespacedName: key}); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 1 {
		t.Fatalf("executed %d capabilities, want only assigned identity", calls)
	}
	if err := c.Delete(context.Background(), object); err != nil {
		t.Fatal(err)
	}
	replacement := object.DeepCopy()
	replacement.ResourceVersion = ""
	replacement.UID = "replacement"
	if err := c.Create(context.Background(), replacement); err != nil {
		t.Fatal(err)
	}
	if _, err := bound.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(object)}); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatal("stale worker executed replacement identity")
	}
}
