// Package execution binds an execution process to one immutable capability identity.
package execution

import (
	"context"

	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// Binding prevents a worker from acting on another capability or a replacement object.
// The underlying executor still performs its current policy and ledger admission checks.
type Binding struct {
	// Namespace and Name identify the assigned resource.
	Namespace, Name string
	// UID pins the resource incarnation across deletion and name reuse.
	UID types.UID
}

// Wrap filters reconcile requests using an uncached identity read. Empty bindings are for library tests.
func (b Binding) Wrap(reader client.Reader, object client.Object, next reconcile.Reconciler) reconcile.Reconciler {
	if b.UID == "" {
		return next
	}
	return reconcile.Func(func(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {
		if request.Namespace != b.Namespace || request.Name != b.Name {
			return ctrl.Result{}, nil
		}
		current := object.DeepCopyObject().(client.Object)
		if err := reader.Get(ctx, request.NamespacedName, current); err != nil {
			return ctrl.Result{}, client.IgnoreNotFound(err)
		}
		if current.GetUID() != b.UID {
			return ctrl.Result{}, nil
		}
		return next.Reconcile(ctx, request)
	})
}
