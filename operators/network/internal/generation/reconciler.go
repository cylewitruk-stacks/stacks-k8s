// Package generation owns the finite Bitcoin action lifecycle without issuing RPCs.
package generation

import (
	"context"
	"reflect"
	"time"

	actionv1 "github.com/cylewitruk-stacks/stacks-k8s/apis/network/actions/v1alpha1"
	bitcoinv1 "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha1"
	networkv1 "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha1"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/actionstatus"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// Finalizer retains actions while an executable intent lacks a receipt.
const Finalizer = actionstatus.Finalizer

// Terminal reports a frozen common-lifecycle outcome.
func Terminal(phase string) bool {
	return actionstatus.Terminal(phase)
}

// Reconciler projects the executor ledger into this kind's lifecycle and finalizer.
type Reconciler struct {
	// Client writes action status and primary metadata only.
	client.Client
	// APIReader reads current admitted identity and durable execution facts.
	APIReader client.Reader
	// Now supplies lifecycle observation times.
	Now func() time.Time
}

// SetupWithManager registers the resource-focused action controller.
func (r *Reconciler) SetupWithManager(m ctrl.Manager) error {
	if r.Now == nil {
		r.Now = time.Now
	}
	return ctrl.NewControllerManagedBy(m).For(&actionv1.BitcoinBlockGeneration{}).Complete(r)
}

// Reconcile never grants RPC authority or clears unresolved ledger state.
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	a := &actionv1.BitcoinBlockGeneration{}
	if err := r.APIReader.Get(ctx, req.NamespacedName, a); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	base := a.DeepCopy()
	expires := metav1.NewTime(a.CreationTimestamp.Add(a.Spec.Timeout.Duration))
	a.Status.ExpiresAt = &expires
	a.Status.ObservedGeneration = a.Generation
	parent := &networkv1.StacksNetwork{}
	parentErr := r.APIReader.Get(ctx, client.ObjectKey{Namespace: a.Namespace, Name: a.Spec.NetworkRef.Name}, parent)
	if parentErr != nil && !apierrors.IsNotFound(parentErr) {
		return ctrl.Result{}, parentErr
	}
	ledger := &bitcoinv1.BitcoinBlockProduction{}
	ledgerErr := r.APIReader.Get(ctx, client.ObjectKey{Namespace: a.Namespace, Name: a.Spec.NetworkRef.Name}, ledger)
	if ledgerErr != nil && !apierrors.IsNotFound(ledgerErr) {
		return ctrl.Result{}, ledgerErr
	}
	bound := parentErr == nil && ledgerErr == nil && metav1.IsControlledBy(ledger, parent) && ledger.Spec.NetworkUID == string(parent.UID) && parent.Status.BitcoinProductionUID == string(ledger.UID)
	reserved := bound && ledger.Status.Action != nil && ledger.Status.Action.UID == string(a.UID)
	gone := parentErr != nil || !parent.DeletionTimestamp.IsZero() || (a.Status.AdmittedNetwork != nil && a.Status.AdmittedNetwork.UID != string(parent.UID))
	if reserved && !gone {
		record := ledger.Status.Action
		if !controllerutil.ContainsFinalizer(a, Finalizer) {
			controllerutil.AddFinalizer(a, Finalizer)
			return ctrl.Result{RequeueAfter: time.Millisecond}, r.Patch(ctx, a, client.MergeFromWithOptions(base, client.MergeFromWithOptimisticLock{}))
		}
		a.Status.AdmittedAt = record.AdmittedAt.DeepCopy()
		a.Status.StartedAt = record.StartedAt.DeepCopy()
		a.Status.AdmittedNetwork = &record.Network
		a.Status.AdmittedTarget = &record.Target
		a.Status.AdmittedPolicy = &record.Policy
		a.Status.CorrelationID = record.CorrelationID
		a.Status.BlocksGenerated = record.BlocksGenerated
		a.Status.LastBlockHash = record.LastBlockHash
		a.Status.LastDispatchID = record.LastDispatchID
		if !Terminal(a.Status.Phase) {
			switch {
			case record.EffectUncertain:
				actionstatus.Finish(&a.Status, a.Generation, r.Now(), "Inconclusive", "EffectUncertain", "Authorized call has no known receipt; target exclusion is retained", false)
			case record.StopReason != "":
				phase := "Failed"
				if record.StopReason == "IdentityDiverged" && record.StartedAt != nil {
					phase = "Inconclusive"
				}
				actionstatus.Finish(&a.Status, a.Generation, r.Now(), phase, record.StopReason, "Finite generation stopped; acknowledged progress is retained", false)
			case record.BlocksGenerated >= record.Spec.Count && record.LastCompletedAt != nil && !record.LastCompletedAt.After(expires.Time) && (a.DeletionTimestamp.IsZero() || !record.LastCompletedAt.After(a.DeletionTimestamp.Time)):
				actionstatus.Finish(&a.Status, a.Generation, r.Now(), "Completed", "ActionCompleted", "Requested block receipts accounted; chain adoption is not asserted", false)
			case !a.DeletionTimestamp.IsZero() || !r.Now().Before(expires.Time):
				if ledger.Status.DispatchState != "Armed" && record.StopReason == "" && record.BlocksGenerated < record.Spec.Count {
					phase := "Admitted"
					if record.StartedAt != nil {
						phase = "Active"
					}
					reason := "DeadlineExceeded"
					if !a.DeletionTimestamp.IsZero() {
						reason = "ActionCancelled"
					}
					actionstatus.Project(&a.Status, a.Generation, r.Now(), phase, reason, "Waiting for executor to withdraw further authorization", false)
					break
				}
				phase, reason, message := "Failed", "DeadlineExceeded", "Action deadline exhausted; no further dispatch is authorized"
				if !a.DeletionTimestamp.IsZero() {
					reason, message = "ActionCancelled", "Deletion stops future generation; prior blocks are retained"
				}
				if ledger.Status.DispatchState == "Armed" {
					phase, reason, message = "Inconclusive", "EffectUncertain", "Deadline or deletion reached with unresolved server work"
				}
				actionstatus.Finish(&a.Status, a.Generation, r.Now(), phase, reason, message, false)
			default:
				phase, reason, message := "Admitted", "AdmissionSucceeded", "Exact target and execution reservation admitted"
				if record.StartedAt != nil {
					phase, reason, message = "Active", "ActionApplying", "Finite generation is in progress"
				}
				actionstatus.Project(&a.Status, a.Generation, r.Now(), phase, reason, message, false)
			}
		}
	} else if !Terminal(a.Status.Phase) {
		switch {
		case a.Status.AdmittedAt != nil:
			actionstatus.Finish(&a.Status, a.Generation, r.Now(), "Inconclusive", "EffectUncertain", "Admitted execution record or owning environment is unavailable", false)
		case !r.Now().Before(expires.Time):
			actionstatus.Finish(&a.Status, a.Generation, r.Now(), "Failed", "DeadlineExceeded", "Action expired before admission", false)
		default:
			reason, message := "TargetNotReady", "Waiting for eligible target and an action-enabled executor"
			if bound && (ledger.Status.Action != nil || ledger.Status.Reorganization != nil || ledger.Status.DispatchState == "Armed") {
				reason, message = "TargetBusy", "Waiting for eligibility and the shared execution reservation"
			}
			actionstatus.Project(&a.Status, a.Generation, r.Now(), "Pending", reason, message, false)
		}
	}
	if !reflect.DeepEqual(base.Status, a.Status) {
		if err := r.Status().Patch(ctx, a, client.MergeFromWithOptions(base, client.MergeFromWithOptimisticLock{})); err != nil {
			return ctrl.Result{}, err
		}
	}
	// Only executor release or explicit environment disposal permits finalizer removal.
	if controllerutil.ContainsFinalizer(a, Finalizer) && (gone || (!reserved && Terminal(a.Status.Phase))) {
		before := a.DeepCopy()
		controllerutil.RemoveFinalizer(a, Finalizer)
		return ctrl.Result{}, r.Patch(ctx, a, client.MergeFromWithOptions(before, client.MergeFromWithOptimisticLock{}))
	}
	if Terminal(a.Status.Phase) && !reserved {
		return ctrl.Result{}, nil
	}
	return ctrl.Result{RequeueAfter: time.Second}, nil
}
