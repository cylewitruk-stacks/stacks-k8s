// Package observation reconciles one-shot trusted identity observations.
package observation

import (
	"context"
	"fmt"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"

	observationv1alpha1 "github.com/cylewitruk-stacks/stacks-k8s/operators/observability/api/v1alpha1"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/observability/internal/topology"
)

const (
	pendingRequeue        = 30 * time.Second
	networkReferenceField = "spec.networkRef.name"
)

// Reconciler produces compact identity-bound observations.
type Reconciler struct {
	client.Client
	APIReader        client.Reader
	TopologyObserver TopologyObserver
	Now              func() time.Time
}

// TopologyObserver verifies one admitted topology snapshot.
type TopologyObserver interface {
	Observe(context.Context, string, string, string) (topology.Snapshot, error)
}

// Reconcile observes a resource once per generation.
func (r *Reconciler) Reconcile(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {
	object := &observationv1alpha1.NetworkObservation{}
	if err := r.Get(ctx, request.NamespacedName, object); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	patchBase := object.DeepCopy()
	if !object.DeletionTimestamp.IsZero() || terminalForGeneration(object) {
		return ctrl.Result{}, nil
	}
	observer := r.observer()
	if observer == nil {
		return ctrl.Result{}, fmt.Errorf("observation reconciler requires an uncached API reader")
	}
	now := r.now()
	timeout := 10 * time.Second
	if object.Spec.TimeoutSeconds != nil {
		timeout = time.Duration(*object.Spec.TimeoutSeconds) * time.Second
	}
	readContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	snapshot, err := observer.Observe(readContext, object.Namespace, object.Spec.NetworkRef.Name, object.Spec.ExpectedInventoryDigest)
	if err != nil {
		if topology.IsNotReady(err) {
			if pendingDeadlineExceeded(object, now) {
				return ctrl.Result{}, r.writeStatus(ctx, object, patchBase, inconclusiveStatus(object, now, "the topology did not become ready before pendingTimeoutSeconds elapsed"))
			}
			if statusErr := r.writeStatus(ctx, object, patchBase, pendingStatus(object, now, err.Error())); statusErr != nil {
				return ctrl.Result{}, statusErr
			}
			return ctrl.Result{RequeueAfter: pendingRequeue}, nil
		}
		if topology.IsInconclusive(err) {
			return ctrl.Result{}, r.writeStatus(ctx, object, patchBase, inconclusiveStatus(object, now, err.Error()))
		}
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, r.writeStatus(ctx, object, patchBase, readyStatus(object, snapshot, now))
}

// SetupWithManager registers the one-shot observation controller.
func (r *Reconciler) SetupWithManager(manager ctrl.Manager, concurrency int) error {
	if r.observer() == nil {
		return fmt.Errorf("observation reconciler requires an uncached API reader")
	}
	if err := manager.GetFieldIndexer().IndexField(context.Background(), &observationv1alpha1.NetworkObservation{}, networkReferenceField, func(object client.Object) []string {
		observation := object.(*observationv1alpha1.NetworkObservation)
		return []string{observation.Spec.NetworkRef.Name}
	}); err != nil {
		return fmt.Errorf("index observations by network reference: %w", err)
	}
	return ctrl.NewControllerManagedBy(manager).
		For(&observationv1alpha1.NetworkObservation{}).
		Watches(topology.NetworkObject(), handler.EnqueueRequestsFromMapFunc(r.observationsForNetwork)).
		WithOptions(controller.Options{MaxConcurrentReconciles: concurrency}).
		Complete(r)
}

func (r *Reconciler) observationsForNetwork(ctx context.Context, object client.Object) []ctrl.Request {
	observations := &observationv1alpha1.NetworkObservationList{}
	if err := r.List(ctx, observations, client.InNamespace(object.GetNamespace()), client.MatchingFields{networkReferenceField: object.GetName()}); err != nil {
		ctrl.LoggerFrom(ctx).Error(err, "list observations for topology event", "network", object.GetName())
		return nil
	}
	requests := make([]ctrl.Request, 0, len(observations.Items))
	for index := range observations.Items {
		observation := &observations.Items[index]
		if terminalForGeneration(observation) {
			continue
		}
		requests = append(requests, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(observation)})
	}
	return requests
}

func (r *Reconciler) observer() TopologyObserver {
	if r.TopologyObserver != nil {
		return r.TopologyObserver
	}
	if r.APIReader == nil {
		return nil
	}
	return topology.Reader{APIReader: r.APIReader}
}

func (r *Reconciler) writeStatus(ctx context.Context, object, patchBase *observationv1alpha1.NetworkObservation, status observationv1alpha1.NetworkObservationStatus) error {
	object.Status = status
	if err := r.Status().Patch(ctx, object, client.MergeFrom(patchBase)); err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	return nil
}

func (r *Reconciler) now() time.Time {
	if r.Now != nil {
		return r.Now().UTC()
	}
	return time.Now().UTC()
}

func terminalForGeneration(object *observationv1alpha1.NetworkObservation) bool {
	if object.Status.ObservedGeneration != object.Generation {
		return false
	}
	return object.Status.Phase == observationv1alpha1.ObservationReady || object.Status.Phase == observationv1alpha1.ObservationInconclusive
}

func pendingDeadlineExceeded(object *observationv1alpha1.NetworkObservation, now time.Time) bool {
	if object.Status.StartedAt == nil || object.Status.ObservedGeneration != object.Generation {
		return false
	}
	seconds := int32(300)
	if object.Spec.PendingTimeoutSeconds != nil {
		seconds = *object.Spec.PendingTimeoutSeconds
	}
	return !now.Before(object.Status.StartedAt.Add(time.Duration(seconds) * time.Second))
}

func pendingStatus(object *observationv1alpha1.NetworkObservation, now time.Time, message string) observationv1alpha1.NetworkObservationStatus {
	status := object.Status
	status.Conditions = append([]metav1.Condition(nil), object.Status.Conditions...)
	initializeStatus(&status, object.Generation, now)
	status.Phase = observationv1alpha1.ObservationPending
	status.Binding = nil
	status.Actors = nil
	status.CompletedAt = nil
	meta.SetStatusCondition(&status.Conditions, condition(object.Generation, metav1.ConditionFalse, "TopologyNotReady", message, now))
	return status
}

func inconclusiveStatus(object *observationv1alpha1.NetworkObservation, now time.Time, message string) observationv1alpha1.NetworkObservationStatus {
	status := object.Status
	status.Conditions = append([]metav1.Condition(nil), object.Status.Conditions...)
	initializeStatus(&status, object.Generation, now)
	status.Phase = observationv1alpha1.ObservationInconclusive
	status.Binding = nil
	status.Actors = nil
	completed := metav1.NewTime(now)
	status.CompletedAt = &completed
	meta.SetStatusCondition(&status.Conditions, condition(object.Generation, metav1.ConditionFalse, "IdentityNotEstablished", message, now))
	return status
}

func readyStatus(object *observationv1alpha1.NetworkObservation, snapshot topology.Snapshot, now time.Time) observationv1alpha1.NetworkObservationStatus {
	status := object.Status
	status.Conditions = append([]metav1.Condition(nil), object.Status.Conditions...)
	initializeStatus(&status, object.Generation, now)
	status.Phase = observationv1alpha1.ObservationReady
	status.Binding = &snapshot.Binding
	status.Actors = snapshot.Actors
	completed := metav1.NewTime(now)
	status.CompletedAt = &completed
	meta.SetStatusCondition(&status.Conditions, condition(object.Generation, metav1.ConditionTrue, "IdentityVerified", "Every admitted actor identity was verified through direct API reads", now))
	return status
}

func initializeStatus(status *observationv1alpha1.NetworkObservationStatus, generation int64, now time.Time) {
	if status.ObservedGeneration != generation || status.StartedAt == nil {
		started := metav1.NewTime(now)
		status.StartedAt = &started
		status.Conditions = nil
	}
	status.ObservedGeneration = generation
}

func condition(generation int64, status metav1.ConditionStatus, reason, message string, now time.Time) metav1.Condition {
	return metav1.Condition{Type: "Ready", Status: status, ObservedGeneration: generation, Reason: reason, Message: message, LastTransitionTime: metav1.NewTime(now)}
}
