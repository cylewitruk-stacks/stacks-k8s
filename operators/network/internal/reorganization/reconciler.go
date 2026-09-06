// Package reorganization owns the compensated finite action lifecycle without issuing RPCs.
package reorganization

import (
	"context"
	"reflect"
	"time"

	actionv1 "github.com/cylewitruk-stacks/stacks-k8s/apis/network/actions/v1alpha1"
	bitcoinv1 "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha1"
	networkv1 "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha1"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/actionstatus"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// Reconciler projects durable branch and cleanup facts into one action kind.
type Reconciler struct {
	// Client writes only this action's status and metadata.
	client.Client
	// APIReader reads current ownership and execution facts directly.
	APIReader client.Reader
	// Now supplies lifecycle observation time.
	Now func() time.Time
}

// SetupWithManager registers the independently enabled lifecycle controller.
func (r *Reconciler) SetupWithManager(m ctrl.Manager) error {
	if r.Now == nil {
		r.Now = time.Now
	}
	return ctrl.NewControllerManagedBy(m).For(&actionv1.BitcoinReorganization{}).Complete(r)
}

// Reconcile retains unresolved cleanup even when the visible outcome is terminal.
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	a := &actionv1.BitcoinReorganization{}
	if err := r.APIReader.Get(ctx, req.NamespacedName, a); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	base := a.DeepCopy()
	status := &a.Status.BitcoinBlockGenerationStatus
	expires := metav1.NewTime(a.CreationTimestamp.Add(a.Spec.Timeout.Duration))
	status.ExpiresAt = &expires
	status.ObservedGeneration = a.Generation
	n := &networkv1.StacksNetwork{}
	key := client.ObjectKey{Namespace: a.Namespace, Name: a.Spec.NetworkRef.Name}
	ne := r.APIReader.Get(ctx, key, n)
	if ne != nil && !apierrors.IsNotFound(ne) {
		return ctrl.Result{}, ne
	}
	p := &bitcoinv1.BitcoinBlockProduction{}
	pe := r.APIReader.Get(ctx, key, p)
	if pe != nil && !apierrors.IsNotFound(pe) {
		return ctrl.Result{}, pe
	}
	bound := ne == nil && pe == nil && metav1.IsControlledBy(p, n) && p.Spec.NetworkUID == string(n.UID) && n.Status.BitcoinProductionUID == string(p.UID)
	reserved := bound && p.Status.Reorganization != nil && p.Status.Reorganization.UID == string(a.UID)
	gone := ne != nil || !n.DeletionTimestamp.IsZero() || (status.AdmittedNetwork != nil && status.AdmittedNetwork.UID != string(n.UID))
	finish := func(phase, reason, message string) {
		actionstatus.Finish(status, a.Generation, r.Now(), phase, reason, message, a.Status.InvalidationAcknowledged)
	}
	project := func(phase, reason, message string) {
		actionstatus.Project(status, a.Generation, r.Now(), phase, reason, message, a.Status.InvalidationAcknowledged)
	}
	if reserved && !gone {
		record := p.Status.Reorganization
		if !controllerutil.ContainsFinalizer(a, actionstatus.Finalizer) {
			controllerutil.AddFinalizer(a, actionstatus.Finalizer)
			return ctrl.Result{RequeueAfter: time.Millisecond}, r.Patch(ctx, a, client.MergeFromWithOptions(base, client.MergeFromWithOptimisticLock{}))
		}
		status.AdmittedAt = record.AdmittedAt.DeepCopy()
		status.StartedAt = record.StartedAt.DeepCopy()
		status.AdmittedNetwork = &record.Network
		status.AdmittedTarget = &record.Target
		status.AdmittedPolicy = &record.Policy
		status.CorrelationID = record.CorrelationID
		status.BlocksGenerated = record.BlocksGenerated
		status.LastBlockHash = record.LastBlockHash
		status.LastDispatchID = record.LastDispatchID
		a.Status.OriginalChain = &record.OriginalChain
		a.Status.ForkParent = &record.ForkParent
		a.Status.InvalidatedHash = record.InvalidatedHash
		a.Status.ReplacementBlockHashes = append([]string(nil), record.ReplacementBlockHashes...)
		a.Status.FinalChain = record.FinalChain.DeepCopy()
		a.Status.InvalidationAcknowledged = record.InvalidationAcknowledged
		a.Status.CleanupAcknowledged = record.CleanupAcknowledged
		clean := record.CleanupAcknowledged || record.StartedAt == nil
		if !actionstatus.Terminal(status.Phase) {
			switch {
			case record.EffectUncertain || record.CleanupUnsafe:
				finish("Inconclusive", "CleanupUncertain", "Unresolved server work or cleanup boundary retains target exclusion")
			case record.FinalChain != nil && record.CleanupAcknowledged && record.StopReason == "":
				finish("Completed", "ActionCompleted", "Replacement is locally canonical with higher work and acknowledged marker cleanup")
			case clean && record.StopReason != "":
				phase := "Failed"
				if record.StopReason == "IdentityDiverged" && record.StartedAt != nil {
					phase = "Inconclusive"
				}
				finish(phase, record.StopReason, "Finite replacement stopped with known cleanup state")
			case !a.DeletionTimestamp.IsZero() || !r.Now().Before(expires.Time):
				if record.StartedAt == nil {
					reason := "DeadlineExceeded"
					if !a.DeletionTimestamp.IsZero() {
						reason = "ActionCancelled"
					}
					project("Admitted", reason, "Waiting for executor to withdraw first authorization")
				} else if p.Status.DispatchState == "Armed" && record.Step == "Reconsider" && r.Now().Before(expires.Add(30*time.Second)) {
					project("Recovering", "RecoveryInProgress", "Collecting the authorized compensation receipt")
				} else if p.Status.DispatchState == "Armed" {
					finish("Inconclusive", "EffectUncertain", "Deadline or deletion reached with unresolved mutation")
				} else {
					project("Recovering", "RecoveryInProgress", "Waiting for executor stop and compensation state")
				}
			case record.StopReason != "" || record.BlocksGenerated == record.Spec.Depth+1 || record.CleanupAcknowledged:
				project("Recovering", "RecoveryInProgress", "Accounting compensation and final local-chain evidence")
			case record.StartedAt != nil:
				project("Active", "ActionApplying", "Replacing the admitted local suffix")
			default:
				project("Admitted", "AdmissionSucceeded", "Exact local suffix and exclusive reservation admitted")
			}
		}
		cleanupStatus := metav1.ConditionUnknown
		reason, message := "CleanupUncertain", "Original marker cleanup is not yet acknowledged"
		if clean {
			cleanupStatus = metav1.ConditionTrue
			reason, message = "ActionRecovered", "No marker was authorized or its compensation receipt is accounted"
		}
		meta.SetStatusCondition(&status.Conditions, metav1.Condition{Type: "CleanupComplete", Status: cleanupStatus, ObservedGeneration: a.Generation, Reason: reason, Message: message, LastTransitionTime: metav1.NewTime(r.Now().UTC())})
	} else if !actionstatus.Terminal(status.Phase) {
		switch {
		case status.AdmittedAt != nil:
			finish("Inconclusive", "CleanupUncertain", "Admitted record or owning environment is unavailable")
		case !r.Now().Before(expires.Time):
			finish("Failed", "DeadlineExceeded", "Request expired before admission")
		default:
			reason, message := "TargetNotReady", "Waiting for eligible target, chain state and reorganization executor"
			if bound && (p.Status.Action != nil || p.Status.Reorganization != nil || p.Status.DispatchState == "Armed") {
				reason, message = "TargetBusy", "Waiting for eligibility and the shared execution reservation"
			}
			project("Pending", reason, message)
		}
	}
	if !reflect.DeepEqual(base.Status, a.Status) {
		if err := r.Status().Patch(ctx, a, client.MergeFromWithOptions(base, client.MergeFromWithOptimisticLock{})); err != nil {
			return ctrl.Result{}, err
		}
	}
	if controllerutil.ContainsFinalizer(a, actionstatus.Finalizer) && (gone || !reserved && actionstatus.Terminal(status.Phase)) {
		before := a.DeepCopy()
		controllerutil.RemoveFinalizer(a, actionstatus.Finalizer)
		return ctrl.Result{}, r.Patch(ctx, a, client.MergeFromWithOptions(before, client.MergeFromWithOptimisticLock{}))
	}
	if actionstatus.Terminal(status.Phase) && !reserved {
		return ctrl.Result{}, nil
	}
	return ctrl.Result{RequeueAfter: time.Second}, nil
}
