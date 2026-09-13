package foundation

import (
	"context"
	"fmt"
	"time"

	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

// ArtifactFinalizer retains shared inputs and execution evidence until all participant disposal finishes.
const ArtifactFinalizer = "network.stacks.org/participant-consumers"

// ArtifactReconciler releases one kind of network-owned shared artifact after its consumers disappear.
// Participant finalizers own process termination; this controller adds no termination claims.
type ArtifactReconciler struct {
	// Client removes only the artifact retention finalizer.
	Client client.Client
	// Reader supplies current artifact ownership and complete participant membership.
	Reader client.Reader
	// Object selects the served artifact kind without importing its domain runtime.
	Object client.Object
	// Name uniquely identifies this resource-focused controller.
	Name string
}

// Reconcile preserves artifacts throughout normal operation and ordered participant destruction.
func (r *ArtifactReconciler) Reconcile(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {
	object := r.Object.DeepCopyObject().(client.Object)
	if err := r.Reader.Get(ctx, request.NamespacedName, object); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if object.GetDeletionTimestamp() == nil || !controllerutil.ContainsFinalizer(object, ArtifactFinalizer) {
		return ctrl.Result{}, nil
	}
	owner := metav1.GetControllerOf(object)
	if owner == nil || owner.APIVersion != api.GroupVersion.String() || owner.Kind != api.KindStacksNetwork || owner.Name != "network" || owner.UID == "" {
		return ctrl.Result{}, fmt.Errorf("shared artifact network ownership unavailable")
	}
	var participants api.StacksNetworkParticipantList
	if err := r.Reader.List(ctx, &participants, client.InNamespace(object.GetNamespace()), client.Limit(1001)); err != nil {
		return ctrl.Result{}, err
	}
	if participants.Continue != "" || len(participants.Items) > 1000 {
		return ctrl.Result{}, fmt.Errorf("shared artifact consumer inventory incomplete")
	}
	for i := range participants.Items {
		p := &participants.Items[i]
		parent := metav1.GetControllerOf(p)
		if p.Spec.NetworkUID == owner.UID || parent != nil && parent.UID == owner.UID {
			return ctrl.Result{RequeueAfter: 2 * time.Second}, nil
		}
	}
	base := object.DeepCopyObject().(client.Object)
	controllerutil.RemoveFinalizer(object, ArtifactFinalizer)
	return ctrl.Result{}, r.Client.Patch(ctx, object, client.MergeFromWithOptions(base, client.MergeFromWithOptimisticLock{}))
}

// SetupWithManager watches deletion requests; retained artifacts poll only while disposal is pending.
func (r *ArtifactReconciler) SetupWithManager(manager ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(manager).Named(r.Name).For(r.Object).
		WithEventFilter(predicate.Funcs{UpdateFunc: func(e event.UpdateEvent) bool {
			a, b := e.ObjectOld, e.ObjectNew
			return a.GetUID() != b.GetUID() || !equal(a.GetDeletionTimestamp(), b.GetDeletionTimestamp()) || !equal(a.GetOwnerReferences(), b.GetOwnerReferences()) || !equal(a.GetFinalizers(), b.GetFinalizers())
		}}).Complete(r)
}
