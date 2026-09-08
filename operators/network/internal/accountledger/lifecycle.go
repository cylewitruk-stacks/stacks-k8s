package accountledger

import (
	"context"
	"time"

	stacks "github.com/cylewitruk-stacks/stacks-k8s/apis/network/stacks/v1alpha1"
	network "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// Reconciler retains account ledgers until explicit removal of their owning environment.
type Reconciler struct {
	// Client writes only account status and finalizers.
	Client client.Client
	// Reader checks the parent incarnation without relying on cache absence.
	Reader client.Reader
}

// SetupWithManager installs administrative account-ledger lifecycle reconciliation.
func (r *Reconciler) SetupWithManager(m ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(m).For(&stacks.StacksAccount{}).Complete(r)
}

// Reconcile never treats account deletion as permission to reuse its nonce authority.
func (r *Reconciler) Reconcile(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {
	account := &stacks.StacksAccount{}
	if err := r.Reader.Get(ctx, request.NamespacedName, account); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	parent := &network.StacksNetwork{}
	err := r.Reader.Get(ctx, client.ObjectKey{Namespace: account.Namespace, Name: account.Spec.NetworkName}, parent)
	retired := apierrors.IsNotFound(err) || (err == nil && (string(parent.UID) != account.Spec.NetworkUID || !parent.DeletionTimestamp.IsZero()))
	if err != nil && !apierrors.IsNotFound(err) {
		return ctrl.Result{}, err
	}
	if !retired {
		return ctrl.Result{RequeueAfter: 2 * time.Second}, nil
	}
	if account.Status.Phase != "Abandoned" {
		base := account.DeepCopy()
		account.Status.Phase = "Abandoned"
		if err := r.Client.Status().Patch(ctx, account, client.MergeFromWithOptions(base, client.MergeFromWithOptimisticLock{})); err != nil {
			return ctrl.Result{}, err
		}
	}
	// Administrative removal preserves the unknown outcome in status; it makes no quiescence claim.
	if controllerutil.ContainsFinalizer(account, Finalizer) {
		base := account.DeepCopy()
		controllerutil.RemoveFinalizer(account, Finalizer)
		if err := r.Client.Patch(ctx, account, client.MergeFromWithOptions(base, client.MergeFromWithOptimisticLock{})); err != nil {
			return ctrl.Result{}, err
		}
	}
	return ctrl.Result{}, nil
}
