// Package leaf contains controller helpers shared by actor reconcilers.
package leaf

import (
	"context"
	"errors"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	networkv1alpha1 "github.com/cylewitruk-stacks/stacks-k8s/operators/network/api/v1alpha1"
	operatorlabels "github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/labels"
)

// IsTransientError reports API and context failures that should retry without
// replacing the last authoritative status.
func IsTransientError(err error) bool {
	return apierrors.IsConflict(err) || apierrors.IsAlreadyExists(err) || apierrors.IsTooManyRequests(err) ||
		apierrors.IsTimeout(err) || apierrors.IsServerTimeout(err) || apierrors.IsServiceUnavailable(err) ||
		apierrors.IsInternalError(err) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled)
}

// PodRequest maps only Pods of the expected actor kind to their leaf resource.
func PodRequest(expectedKind operatorlabels.ActorKind) func(context.Context, client.Object) []ctrl.Request {
	return func(_ context.Context, object client.Object) []ctrl.Request {
		labels := object.GetLabels()
		name := labels[operatorlabels.ActorResourceKey]
		if name == "" || labels[operatorlabels.ActorKindKey] != string(expectedKind) {
			return nil
		}
		return []ctrl.Request{{NamespacedName: client.ObjectKey{Namespace: object.GetNamespace(), Name: name}}}
	}
}

// ReconcileError suppresses rate-limited retries for deterministic declarations.
func ReconcileError(err error, permanent bool) error {
	if err != nil && permanent {
		return reconcile.TerminalError(err)
	}
	return err
}

// DegradedStatus returns a leaf status that cannot be mistaken for readiness.
func DegradedStatus(generation int64, err error, previous []metav1.Condition) networkv1alpha1.ActorStatus {
	status := networkv1alpha1.ActorStatus{ObservedGeneration: generation, Phase: "Degraded", Conditions: append([]metav1.Condition(nil), previous...)}
	meta.SetStatusCondition(&status.Conditions, metav1.Condition{
		Type: "Ready", Status: metav1.ConditionFalse, ObservedGeneration: generation, Reason: "ReconciliationFailed", Message: err.Error(),
	})
	return status
}

// WithReadyCondition adds the canonical Ready condition to an observed status.
func WithReadyCondition(status networkv1alpha1.ActorStatus) networkv1alpha1.ActorStatus {
	conditionStatus, reason, message := metav1.ConditionFalse, "WorkloadProgressing", "The actor workload has not admitted a ready identity"
	if status.Ready {
		conditionStatus, reason, message = metav1.ConditionTrue, "WorkloadReady", "The actor workload has an admitted runtime identity"
	}
	if status.Phase == "Suspended" {
		reason, message = "WorkloadSuspended", "The actor workload is suspended"
	}
	meta.SetStatusCondition(&status.Conditions, metav1.Condition{Type: "Ready", Status: conditionStatus, ObservedGeneration: status.ObservedGeneration, Reason: reason, Message: message})
	return status
}
