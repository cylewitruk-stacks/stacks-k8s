package bitcoincontrol

import (
	"context"
	"time"

	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// OverrideReconciler owns request lifecycle independently of whether a network exists.
type OverrideReconciler struct {
	// Client writes request status and its cleanup finalizer.
	Client client.Client
	// Reader validates retained scheduler identity with uncached reads.
	Reader client.Reader
	// Now supplies admission and wall-clock expiration observations.
	Now func() time.Time
}

// SetupWithManager installs lifecycle polling without introducing a second timing authority.
func (r *OverrideReconciler) SetupWithManager(m ctrl.Manager) error {
	if r.Now == nil {
		r.Now = time.Now
	}
	return ctrl.NewControllerManagedBy(m).Named("bitcoin-schedule-override-v1alpha2").For(&bitcoin.BitcoinBlockScheduleOverride{}).Complete(r)
}

// Reconcile retains activation until the scheduler has withdrawn it, even after deletion.
func (r *OverrideReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	request := &bitcoin.BitcoinBlockScheduleOverride{}
	if err := r.Reader.Get(ctx, req.NamespacedName, request); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	root := &api.StacksNetwork{}
	err := r.Reader.Get(ctx, client.ObjectKey{Namespace: request.Namespace, Name: "network"}, root)
	if err != nil && !apierrors.IsNotFound(err) {
		return ctrl.Result{}, err
	}
	rootCurrent := err == nil && root.UID == request.Spec.NetworkUID
	var active *bitcoin.BitcoinActiveOverride
	if rootCurrent && root.Status.Bitcoin != nil && root.Status.Bitcoin.InitializationRef != nil {
		ref := root.Status.Bitcoin.InitializationRef
		var initial bitcoin.BitcoinInitialization
		if err := r.Reader.Get(ctx, client.ObjectKey{Namespace: root.Namespace, Name: ref.Name}, &initial); err != nil {
			return ctrl.Result{}, err
		}
		if initial.UID != ref.UID || initial.Spec.NetworkUID != root.UID || !metav1.IsControlledBy(&initial, root) {
			return ctrl.Result{RequeueAfter: time.Second}, nil
		}
		if candidate := initial.Status.Override; candidate != nil && candidate.Override == binding("BitcoinBlockScheduleOverride", request) {
			active = candidate.DeepCopy()
		}
	}
	status := *request.Status.DeepCopy()
	status.ObservedGeneration = request.Generation
	pending := metav1.NewTime(request.CreationTimestamp.Add(5 * time.Minute))
	status.PendingExpiresAt = &pending
	if active != nil {
		status.Admission = active
		status.Phase = "Active"
		status.Reason = "Activated"
		if request.DeletionTimestamp != nil {
			status.Phase = "Cancelled"
			status.Reason = "CancellationRequested"
		} else if !r.Now().Before(active.ExpiresAt.Time) {
			status.Phase = "Completed"
			status.Reason = "DurationElapsed"
		}
	} else if !overrideTerminal(status.Phase) {
		switch {
		case status.Admission != nil:
			status.Phase = "Cancelled"
			status.Reason = "ActivationWithdrawn"
		case request.DeletionTimestamp != nil:
			status.Phase = "Cancelled"
			status.Reason = "CancelledBeforeActivation"
		case !r.Now().Before(pending.Time):
			status.Phase = "Expired"
			status.Reason = "PendingDeadline"
		default:
			status.Phase = "Pending"
			status.Reason = "WaitingForActivation"
		}
	}
	if err := r.applyStatus(ctx, request, status); err != nil {
		return ctrl.Result{}, err
	}
	finalizer := controllerutil.ContainsFinalizer(request, OverrideFinalizer)
	if active == nil && overrideTerminal(status.Phase) {
		if finalizer {
			before := request.DeepCopy()
			controllerutil.RemoveFinalizer(request, OverrideFinalizer)
			return ctrl.Result{}, r.Client.Patch(ctx, request, client.MergeFromWithOptions(before, client.MergeFromWithOptimisticLock{}))
		}
		return ctrl.Result{}, nil
	}
	if !finalizer && request.DeletionTimestamp == nil {
		before := request.DeepCopy()
		controllerutil.AddFinalizer(request, OverrideFinalizer)
		return ctrl.Result{RequeueAfter: time.Millisecond}, r.Client.Patch(ctx, request, client.MergeFromWithOptions(before, client.MergeFromWithOptimisticLock{}))
	}
	return ctrl.Result{RequeueAfter: time.Second}, nil
}

// applyStatus publishes only request lifecycle fields with identity and resource-version guards.
func (r *OverrideReconciler) applyStatus(ctx context.Context, request *bitcoin.BitcoinBlockScheduleOverride, status bitcoin.BitcoinBlockScheduleOverrideStatus) error {
	if equality.Semantic.DeepEqual(request.Status, status) {
		return nil
	}
	patch := &bitcoin.BitcoinBlockScheduleOverride{TypeMeta: metav1.TypeMeta{APIVersion: bitcoin.GroupVersion.String(), Kind: "BitcoinBlockScheduleOverride"}, ObjectMeta: metav1.ObjectMeta{Name: request.Name, Namespace: request.Namespace, UID: request.UID, ResourceVersion: request.ResourceVersion}, Status: status}
	if err := r.Client.Status().Patch(ctx, patch, client.Apply, client.FieldOwner(OverrideFieldManager), client.ForceOwnership); err != nil {
		return err
	}
	*request = *patch
	return nil
}
