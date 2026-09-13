package foundation

import (
	"context"
	"errors"
	"reflect"

	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// NetworkRuntime composes shared record allocation and observations under the aggregate.
// Reconcile may mutate root.Status; only the aggregate publishes that status.
// It consumes admitted state, never grants participant admission, and checks current
// identities before allocating records or authorizing work. Return RequeueAfter for
// the next observation expiry even when no new watch event is expected.
type NetworkRuntime interface {
	Reconcile(context.Context, *api.StacksNetwork) (ctrl.Result, error)
}

// networkRequest separates validation and observation queue keys inside one controller.
// The scope is constructed only by internal handlers, never from custom-resource fields.
type networkRequest struct {
	// key identifies the root; each path reads its current UID/state from the API.
	key types.NamespacedName
	// runtimeOnly selects observation rather than admission resolution.
	runtimeOnly bool
}

// reconcileRequest preserves independent retries and deadlines for each queue path.
func (r *Reconciler) reconcileRequest(ctx context.Context, request networkRequest) (ctrl.Result, error) {
	key := ctrl.Request{NamespacedName: request.key}
	if request.runtimeOnly {
		return r.reconcileRuntime(ctx, key)
	}
	return r.reconcileTopology(ctx, key)
}

// Reconcile preserves full reconciliation for direct callers and foundation tests.
// The installed controller uses distinct queue keys so runtime timers never poll topology.
func (r *Reconciler) Reconcile(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {
	topology, topologyErr := r.reconcileTopology(ctx, request)
	operation, operationErr := r.reconcileRuntime(ctx, request)
	if operation.RequeueAfter > 0 && (topology.RequeueAfter == 0 || operation.RequeueAfter < topology.RequeueAfter) {
		topology.RequeueAfter = operation.RequeueAfter
	}
	return topology, errors.Join(topologyErr, operationErr)
}

// reconcileRuntime projects admitted runtime facts without rebuilding or admitting topology.
func (r *Reconciler) reconcileRuntime(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {
	if r.Runtime == nil {
		return ctrl.Result{}, nil
	}
	var root api.StacksNetwork
	if err := r.Reader.Get(ctx, request.NamespacedName, &root); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	base := root.DeepCopy()
	operation, err := r.Runtime.Reconcile(ctx, &root)
	if failed := meta.FindStatusCondition(
		base.Status.Conditions,
		api.ConditionFailed,
	); failed != nil &&
		failed.Status == metav1.ConditionTrue {
		meta.SetStatusCondition(&root.Status.Conditions, *failed)
	}
	if base.Status.Phase == api.NetworkPhaseFailed &&
		!meta.IsStatusConditionTrue(root.Status.Conditions, api.ConditionFailed) {
		root.Status.Phase = api.NetworkPhaseFailed
	}
	if !reflect.DeepEqual(base.Status, root.Status) {
		err = errors.Join(
			err,
			r.Client.Status().
				Patch(ctx, &root, client.MergeFromWithOptions(base, client.MergeFromWithOptimisticLock{})),
		)
	}
	return operation, err
}
