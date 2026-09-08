// Package ledgerlifecycle releases production ledgers during explicit environment disposal.
package ledgerlifecycle

import (
	"context"
	"time"

	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha1"
	stacks "github.com/cylewitruk-stacks/stacks-k8s/apis/network/stacks/v1alpha1"
	network "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// PolicyFinalizer retains the Bitcoin policy and its target inventory.
const PolicyFinalizer = "bitcoin.stacks.org/retain-production-policy"

// Kind selects the resource retained by a lifecycle controller.
type Kind string

const (
	// Policy selects the Bitcoin production policy root.
	Policy Kind = "bitcoin-policy"
	// Target selects a Bitcoin execution ledger.
	Target Kind = "bitcoin-target"
	// Transfer selects a Stacks transfer ledger.
	Transfer Kind = "transfer"
)

// SetupWithManager registers all administrative lifecycle controllers independently of scheduling.
func SetupWithManager(m ctrl.Manager) error {
	for _, kind := range []Kind{Policy, Target, Transfer} {
		if err := (&Reconciler{Client: m.GetClient(), Reader: m.GetAPIReader(), Kind: kind}).SetupWithManager(m); err != nil {
			return err
		}
	}
	return nil
}

// BitcoinFinalizer retains potentially unresolved Bitcoin dispatch evidence.
const BitcoinFinalizer = "bitcoin.stacks.org/retain-production-ledger"

// TransferFinalizer retains potentially unresolved transfer nonce evidence.
const TransferFinalizer = "stacks.stacks.org/retain-transaction-ledger"

// Reconciler performs administrative disposal without RPC, credentials or worker availability.
type Reconciler struct {
	// Client patches status and finalizers; Reader establishes current parent identity.
	Client client.Client
	Reader client.Reader
	// Kind selects the independently watched ledger resource.
	Kind Kind
}

// object selects one independently watched ledger kind.
func (r *Reconciler) object() client.Object {
	switch r.Kind {
	case Policy:
		return &bitcoin.BitcoinBlockProduction{}
	case Target:
		return &bitcoin.BitcoinProductionTarget{}
	case Transfer:
		return &stacks.StacksTransactionProduction{}
	default:
		panic("unsupported ledger lifecycle kind")
	}
}

// SetupWithManager installs an operator-side lifecycle controller for one ledger kind.
func (r *Reconciler) SetupWithManager(m ctrl.Manager) error {
	name := string(r.Kind) + "-ledger-lifecycle"
	return ctrl.NewControllerManagedBy(m).Named(name).For(r.object()).Complete(r)
}

// Reconcile preserves unresolved facts and releases retention only when the parent is retired.
func (r *Reconciler) Reconcile(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {
	object := r.object()
	if err := r.Reader.Get(ctx, request.NamespacedName, object); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	var name, uid, finalizer string
	switch o := object.(type) {
	case *bitcoin.BitcoinBlockProduction:
		name, uid, finalizer = o.Spec.NetworkName, o.Spec.NetworkUID, PolicyFinalizer
	case *bitcoin.BitcoinProductionTarget:
		name, uid, finalizer = o.Spec.NetworkName, o.Spec.NetworkUID, BitcoinFinalizer
	case *stacks.StacksTransactionProduction:
		name, uid, finalizer = o.Spec.NetworkName, o.Spec.NetworkUID, TransferFinalizer
	}
	if !controllerutil.ContainsFinalizer(object, finalizer) {
		return ctrl.Result{}, nil
	}
	parent := &network.StacksNetwork{}
	err := r.Reader.Get(ctx, client.ObjectKey{Namespace: object.GetNamespace(), Name: name}, parent)
	if err != nil && !apierrors.IsNotFound(err) {
		return ctrl.Result{}, err
	}
	if err == nil && string(parent.UID) == uid && parent.DeletionTimestamp.IsZero() {
		return ctrl.Result{RequeueAfter: 2 * time.Second}, nil
	}
	base := object.DeepCopyObject().(client.Object)
	switch o := object.(type) {
	case *bitcoin.BitcoinBlockProduction:
		o.Status.Phase, o.Status.Message = "Abandoned", "Owning network removed"
	case *bitcoin.BitcoinProductionTarget:
		o.Status.Phase, o.Status.Message = "Abandoned", "Owning environment removed; execution outcome is not a recovery claim"
	case *stacks.StacksTransactionProduction:
		o.Status.Phase, o.Status.Message = "Abandoned", "Owning environment removed; signed transactions may still execute"
	}
	if err := r.Client.Status().Patch(ctx, object, client.MergeFromWithOptions(base, client.MergeFromWithOptimisticLock{})); err != nil {
		return ctrl.Result{}, err
	}
	base = object.DeepCopyObject().(client.Object)
	controllerutil.RemoveFinalizer(object, finalizer)
	return ctrl.Result{}, r.Client.Patch(ctx, object, client.MergeFromWithOptions(base, client.MergeFromWithOptimisticLock{}))
}
