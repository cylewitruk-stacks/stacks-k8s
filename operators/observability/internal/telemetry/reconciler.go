package telemetry

import (
	"context"
	"fmt"
	"time"

	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	observation "github.com/cylewitruk-stacks/stacks-k8s/operators/observability/api/v1alpha2"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

// Reconciler admits a source incarnation and maintains only recording-owned resources.
type Reconciler struct {
	client.Client
	// APIReader provides current ownership and source identity checks.
	APIReader client.Reader
	// WorkerImage selects the independently versioned recorder executable.
	WorkerImage string
	// CollectorImage selects the pinned upstream OTel collector.
	CollectorImage string
}

// SetupWithManager registers the controller without subscribing to every actor status update.
func (r *Reconciler) SetupWithManager(m ctrl.Manager, concurrency int) error {
	return ctrl.NewControllerManagedBy(m).
		For(&observation.NetworkTelemetry{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Owns(&appsv1.Deployment{}).
		Owns(&appsv1.DaemonSet{}).
		WithOptions(controller.Options{MaxConcurrentReconciles: concurrency}).
		Complete(r)
}

// Reconcile never mutates source networks, actors, actions or their credentials.
func (r *Reconciler) Reconcile(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {
	t := &observation.NetworkTelemetry{}
	if err := r.APIReader.Get(ctx, request.NamespacedName, t); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if !t.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, nil
	}
	if !t.Status.Admitted {
		root := &api.StacksNetwork{}
		err := r.APIReader.Get(ctx, client.ObjectKey{Namespace: t.Namespace, Name: t.Spec.NetworkName}, root)
		if err != nil || root.UID != t.Spec.NetworkUID || !root.DeletionTimestamp.IsZero() {
			err = r.report(ctx, t, false, observation.ConditionNetworkResolved, metav1.ConditionFalse,
				observation.ReasonNetworkUnavailable, "Waiting for the exact live network UID")
			return ctrl.Result{RequeueAfter: 5 * time.Second}, err
		}
		if err := r.report(ctx, t, true, observation.ConditionNetworkResolved, metav1.ConditionTrue,
			observation.ReasonAdmitted, "Recording is bound to the selected network incarnation"); err != nil {
			return ctrl.Result{}, err
		}
		// Admission must be persisted before workloads can start; no root owner reference is added.
		return ctrl.Result{RequeueAfter: 100 * time.Millisecond}, nil
	}
	objects, err := Resources(t, r.WorkerImage, r.CollectorImage)
	if err != nil {
		return ctrl.Result{}, err
	}
	for _, object := range objects {
		if err := r.ensure(ctx, t, object); err != nil {
			if statusErr := r.report(
				ctx,
				t,
				true,
				observation.ConditionWorkloadsReady,
				metav1.ConditionFalse,
				observation.ReasonOwnershipConflict,
				"A recording resource is unavailable or has foreign ownership",
			); statusErr != nil {
				return ctrl.Result{}, statusErr
			}
			return ctrl.Result{}, err
		}
	}
	if !NeedsCollector(t) {
		if err := r.removeCollector(ctx, t); err != nil {
			return ctrl.Result{}, err
		}
	}
	deployment := &appsv1.Deployment{}
	daemonset := &appsv1.DaemonSet{}
	if err := r.APIReader.Get(
		ctx,
		client.ObjectKey{Namespace: t.Namespace, Name: Name(t) + "-recorder"},
		deployment,
	); err != nil {
		return ctrl.Result{}, err
	}
	if NeedsCollector(t) {
		if err := r.APIReader.Get(
			ctx,
			client.ObjectKey{Namespace: t.Namespace, Name: Name(t) + "-collector"},
			daemonset,
		); err != nil {
			return ctrl.Result{}, err
		}
	}
	ready := deployment.Status.ObservedGeneration == deployment.Generation &&
		deployment.Status.AvailableReplicas == 1 &&
		(!NeedsCollector(t) || (daemonset.Status.ObservedGeneration == daemonset.Generation &&
			daemonset.Status.DesiredNumberScheduled > 0 &&
			daemonset.Status.NumberReady == daemonset.Status.DesiredNumberScheduled))
	status, reason := metav1.ConditionFalse, observation.ReasonProvisioning
	message := "Waiting for enabled recording workloads"
	if ready {
		status, reason = metav1.ConditionTrue, observation.ReasonAvailable
		message = "Workloads ready; inspect recording freshness and source gaps separately"
	}
	return ctrl.Result{}, r.report(ctx, t, true, observation.ConditionWorkloadsReady, status, reason, message)
}

// ensure creates without adoption and applies only explicitly rendered fields of an owned object.
func (r *Reconciler) ensure(ctx context.Context, t *observation.NetworkTelemetry, desired client.Object) error {
	current := desired.DeepCopyObject().(client.Object)
	err := r.APIReader.Get(ctx, client.ObjectKeyFromObject(desired), current)
	if apierrors.IsNotFound(err) {
		return r.Create(ctx, desired, client.FieldOwner(ControllerFieldManager))
	}
	if err != nil {
		return err
	}
	if !metav1.IsControlledBy(current, t) || !current.GetDeletionTimestamp().IsZero() {
		return fmt.Errorf("recording resource %s is foreign or terminating", current.GetName())
	}
	// Kubernetes volume sources are a union. An omitted SSA branch can remain owned
	// by the initial Create operation; replace storage fields atomically, preserving
	// unrelated Pod fields and API-server defaults.
	if next, ok := desired.(*appsv1.DaemonSet); ok {
		old := current.(*appsv1.DaemonSet)
		if collectorUsesHostStorage(old) != collectorUsesHostStorage(next) {
			base := old.DeepCopy()
			desiredSpec := next.Spec.Template.Spec.DeepCopy()
			old.Spec.Template.Spec.Volumes = desiredSpec.Volumes
			for i := range old.Spec.Template.Spec.Containers {
				for _, container := range desiredSpec.Containers {
					if old.Spec.Template.Spec.Containers[i].Name == container.Name {
						old.Spec.Template.Spec.Containers[i].VolumeMounts = container.VolumeMounts
					}
				}
			}
			if old.Spec.Template.Spec.SecurityContext == nil {
				old.Spec.Template.Spec.SecurityContext = &corev1.PodSecurityContext{}
			}
			security := old.Spec.Template.Spec.SecurityContext
			security.RunAsUser = desiredSpec.SecurityContext.RunAsUser
			security.RunAsNonRoot = desiredSpec.SecurityContext.RunAsNonRoot
			security.FSGroup = desiredSpec.SecurityContext.FSGroup
			if old.Spec.Template.Annotations == nil {
				old.Spec.Template.Annotations = map[string]string{}
			}
			for key, value := range next.Spec.Template.Annotations {
				old.Spec.Template.Annotations[key] = value
			}
			return r.Patch(
				ctx,
				old,
				client.MergeFromWithOptions(base, client.MergeFromWithOptimisticLock{}),
				client.FieldOwner(ControllerFieldManager),
			)
		}
	}
	gvk, err := r.GroupVersionKindFor(desired)
	if err != nil {
		return err
	}
	value, err := runtime.DefaultUnstructuredConverter.ToUnstructured(desired)
	if err != nil {
		return err
	}
	u := &unstructured.Unstructured{Object: value}
	u.SetGroupVersionKind(gvk)
	delete(u.Object, "status")
	u.SetResourceVersion(current.GetResourceVersion())
	u.SetUID(current.GetUID())
	//nolint:staticcheck // Minimal ownership-aware SSA; no generated apply configurations exist for these CRDs.
	return r.Patch(ctx, u, client.Apply, client.FieldOwner(ControllerFieldManager), client.ForceOwnership)
}

// report applies only controller-owned status fields; recorder status is never read back into a patch.
func (r *Reconciler) report(ctx context.Context, t *observation.NetworkTelemetry, admitted bool,
	condition string, status metav1.ConditionStatus, reason, message string,
) error {
	current := &observation.NetworkTelemetry{}
	if err := r.APIReader.Get(ctx, client.ObjectKeyFromObject(t), current); err != nil {
		return err
	}
	if current.UID != t.UID || current.Generation != t.Generation || !current.DeletionTimestamp.IsZero() {
		return fmt.Errorf("recording changed before status publication")
	}
	admitted = admitted || current.Status.Admitted
	if current.Status.Admitted && condition == observation.ConditionNetworkResolved {
		status, reason = metav1.ConditionTrue, observation.ReasonAdmitted
		message = "Recording is bound to the selected network incarnation"
	}
	conditions := append([]metav1.Condition(nil), current.Status.Conditions...)
	meta.SetStatusCondition(&conditions, metav1.Condition{
		Type: condition, Status: status, Reason: reason,
		Message: message, ObservedGeneration: t.Generation, LastTransitionTime: metav1.Now(),
	})
	value, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&observation.NetworkTelemetryStatus{
		Admitted: admitted, TablePrefix: TablePrefix(t), Conditions: conditions,
	})
	if err != nil {
		return err
	}
	value["admitted"] = admitted
	u := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": observation.GroupVersion.String(),
		"kind":       observation.KindNetworkTelemetry, "metadata": map[string]any{
			"name": t.Name, "namespace": t.Namespace,
			"uid": string(t.UID), "resourceVersion": current.ResourceVersion,
		}, "status": value,
	}}
	//nolint:staticcheck // Minimal SSA preserves disjoint status ownership without a generated apply configuration.
	return r.Status().Patch(ctx, u, client.Apply, client.FieldOwner(ControllerFieldManager), client.ForceOwnership)
}

// RegisterWorkloadTypes installs owned resource kinds for standalone test clients.
func RegisterWorkloadTypes(s *runtime.Scheme) error {
	for _, add := range []func(*runtime.Scheme) error{
		corev1.AddToScheme, appsv1.AddToScheme, rbacv1.AddToScheme, observation.AddToScheme, api.AddToScheme,
	} {
		if err := add(s); err != nil {
			return err
		}
	}
	return nil
}

// removeCollector withdraws only exact-owned resources when node collection is disabled.
func (r *Reconciler) removeCollector(ctx context.Context, t *observation.NetworkTelemetry) error {
	for _, object := range []client.Object{
		&appsv1.DaemonSet{}, &rbacv1.RoleBinding{}, &rbacv1.Role{}, &corev1.ServiceAccount{}, &corev1.ConfigMap{},
	} {
		name := Name(t) + "-collector"
		if _, ok := object.(*corev1.ConfigMap); ok {
			name = Name(t)
		}
		if err := r.APIReader.Get(ctx, client.ObjectKey{Namespace: t.Namespace, Name: name}, object); err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}
			return err
		}
		if !metav1.IsControlledBy(object, t) {
			return fmt.Errorf("refusing to remove foreign collector resource")
		}
		uid := object.GetUID()
		if err := r.Delete(ctx, object, client.Preconditions{UID: &uid}); client.IgnoreNotFound(err) != nil {
			return err
		}
	}
	return nil
}

// collectorUsesHostStorage distinguishes the two supported checkpoint volume sources.
func collectorUsesHostStorage(object *appsv1.DaemonSet) bool {
	for _, volume := range object.Spec.Template.Spec.Volumes {
		if volume.Name == "checkpoints" {
			return volume.HostPath != nil
		}
	}
	return false
}
