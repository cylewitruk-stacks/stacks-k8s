package workload

import (
	"context"
	"errors"
	"fmt"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	networkv1alpha1 "github.com/cylewitruk-stacks/stacks-k8s/operators/network/api/v1alpha1"
	operatorlabels "github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/labels"
)

// Engine converges and observes resources owned by one leaf actor.
type Engine struct {
	Client client.Client
	Reader client.Reader
	Scheme *runtime.Scheme
}

type serviceReplacementRequired struct {
	uid    types.UID
	reason string
}

func (e *serviceReplacementRequired) Error() string { return e.reason }

// Reconcile applies one normalized workload and reports its observed state.
func (e Engine) Reconcile(ctx context.Context, descriptor Descriptor) (networkv1alpha1.ActorStatus, error) {
	if e.Client == nil || e.Reader == nil || e.Scheme == nil {
		return networkv1alpha1.ActorStatus{}, fmt.Errorf("workload engine requires client, uncached reader, and scheme")
	}
	own := func(object metav1.Object) error {
		clientObject, ok := object.(client.Object)
		if !ok {
			return fmt.Errorf("managed object %T is not a Kubernetes client object", object)
		}
		return controllerutil.SetControllerReference(descriptor.Owner, clientObject, e.Scheme)
	}
	desired, err := render(descriptor, own)
	if err != nil {
		return networkv1alpha1.ActorStatus{}, Permanent(err)
	}
	if desired.configMap != nil {
		if err := e.applyConfigMap(ctx, descriptor.Owner, desired.configMap); err != nil {
			return networkv1alpha1.ActorStatus{}, err
		}
	} else if err := e.deleteGeneratedConfig(ctx, descriptor); err != nil {
		return networkv1alpha1.ActorStatus{}, err
	}
	if err := e.applyService(ctx, descriptor.Owner, desired.service); err != nil {
		return networkv1alpha1.ActorStatus{}, err
	}
	if err := e.applyStatefulSet(ctx, descriptor.Owner, desired.statefulSet); err != nil {
		return networkv1alpha1.ActorStatus{}, err
	}
	return e.observe(ctx, descriptor, desired.statefulSet)
}

func (e Engine) applyConfigMap(ctx context.Context, owner client.Object, desired *corev1.ConfigMap) error {
	current := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: desired.Name, Namespace: desired.Namespace}}
	_, err := controllerutil.CreateOrUpdate(ctx, e.Client, current, func() error {
		if err := assertOwnedOrNew(owner, current); err != nil {
			return err
		}
		current.Labels, current.Data, current.BinaryData = copyMap(desired.Labels), copyMap(desired.Data), desired.BinaryData
		return controllerutil.SetControllerReference(owner, current, e.Scheme)
	})
	return wrapApply("ConfigMap", desired.Name, err)
}

func (e Engine) applyService(ctx context.Context, owner client.Object, desired *corev1.Service) error {
	current := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: desired.Name, Namespace: desired.Namespace}}
	_, err := controllerutil.CreateOrUpdate(ctx, e.Client, current, func() error {
		if err := assertOwnedOrNew(owner, current); err != nil {
			return err
		}
		created := current.CreationTimestamp.IsZero()
		clusterIP := current.Spec.ClusterIP
		clusterIPs := append([]string(nil), current.Spec.ClusterIPs...)
		ipFamilies := append([]corev1.IPFamily(nil), current.Spec.IPFamilies...)
		ipFamilyPolicy := current.Spec.IPFamilyPolicy
		current.Labels = copyMap(desired.Labels)
		current.Spec = *desired.Spec.DeepCopy()
		if !created {
			if clusterIP != "" && clusterIP != corev1.ClusterIPNone {
				return &serviceReplacementRequired{uid: current.UID, reason: fmt.Sprintf("Service %s has non-headless cluster IP %q", current.Name, clusterIP)}
			}
			for _, value := range clusterIPs {
				if value != corev1.ClusterIPNone {
					return &serviceReplacementRequired{uid: current.UID, reason: fmt.Sprintf("Service %s has non-headless cluster IP state", current.Name)}
				}
			}
			// Preserve only API-assigned immutable IP-family state. All mutable
			// routing fields come from the desired headless-Service contract.
			current.Spec.ClusterIP = clusterIP
			current.Spec.ClusterIPs = clusterIPs
			current.Spec.IPFamilies = ipFamilies
			current.Spec.IPFamilyPolicy = ipFamilyPolicy
			if current.Spec.ClusterIP == "" {
				current.Spec.ClusterIP = corev1.ClusterIPNone
			}
			if len(current.Spec.ClusterIPs) == 0 {
				current.Spec.ClusterIPs = []string{corev1.ClusterIPNone}
			}
		}
		return controllerutil.SetControllerReference(owner, current, e.Scheme)
	})
	var replacement *serviceReplacementRequired
	if errors.As(err, &replacement) {
		uid := replacement.uid
		candidate := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: desired.Name, Namespace: desired.Namespace}}
		deleteErr := e.Client.Delete(ctx, candidate, &client.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid}})
		if deleteErr != nil && !apierrors.IsNotFound(deleteErr) {
			// A failed identity-preconditioned delete means admitted Service
			// identity is uncertain. Keep this non-permanent so the leaf both
			// withdraws readiness and retries.
			return fmt.Errorf("replace owned Service %s after routing drift: %v", desired.Name, deleteErr)
		}
		return fmt.Errorf("replace owned Service %s after routing drift: recreation pending", desired.Name)
	}
	return wrapApply("Service", desired.Name, err)
}

func (e Engine) applyStatefulSet(ctx context.Context, owner client.Object, desired *appsv1.StatefulSet) error {
	current := &appsv1.StatefulSet{ObjectMeta: metav1.ObjectMeta{Name: desired.Name, Namespace: desired.Namespace}}
	_, err := controllerutil.CreateOrUpdate(ctx, e.Client, current, func() error {
		if err := assertOwnedOrNew(owner, current); err != nil {
			return err
		}
		if !current.CreationTimestamp.IsZero() {
			if current.Spec.ServiceName != desired.Spec.ServiceName || !apiequality.Semantic.DeepEqual(current.Spec.Selector, desired.Spec.Selector) || !volumeClaimTemplatesCompatible(current.Spec.VolumeClaimTemplates, desired.Spec.VolumeClaimTemplates) {
				return Permanent(fmt.Errorf("StatefulSet identity or storage is immutable; create a replacement actor"))
			}
		}
		current.Labels = copyMap(desired.Labels)
		if current.CreationTimestamp.IsZero() {
			current.Spec = *desired.Spec.DeepCopy()
		} else {
			current.Spec.Replicas = desired.Spec.Replicas
			current.Spec.Template = *desired.Spec.Template.DeepCopy()
			current.Spec.UpdateStrategy = desired.Spec.UpdateStrategy
			current.Spec.PersistentVolumeClaimRetentionPolicy = desired.Spec.PersistentVolumeClaimRetentionPolicy
		}
		return controllerutil.SetControllerReference(owner, current, e.Scheme)
	})
	return wrapApply("StatefulSet", desired.Name, err)
}

func volumeClaimTemplatesCompatible(current, desired []corev1.PersistentVolumeClaim) bool {
	if len(current) != len(desired) {
		return false
	}
	for index := range desired {
		if current[index].Name != desired[index].Name || !apiequality.Semantic.DeepDerivative(&desired[index].Spec, &current[index].Spec) {
			return false
		}
	}
	return true
}

func assertOwnedOrNew(owner, object client.Object) error {
	if object.GetUID() == "" {
		return nil
	}
	controller := metav1.GetControllerOf(object)
	if controller == nil || controller.UID != owner.GetUID() {
		return fmt.Errorf("refuse to adopt %T %s without matching owner UID", object, object.GetName())
	}
	return nil
}

func (e Engine) deleteGeneratedConfig(ctx context.Context, descriptor Descriptor) error {
	object := &corev1.ConfigMap{}
	key := client.ObjectKey{Namespace: descriptor.Owner.GetNamespace(), Name: configName(descriptor.Owner.GetName())}
	if err := e.Reader.Get(ctx, key, object); err != nil {
		return client.IgnoreNotFound(err)
	}
	if err := assertOwnedOrNew(descriptor.Owner, object); err != nil {
		return err
	}
	uid := object.UID
	return client.IgnoreNotFound(e.Client.Delete(ctx, object, &client.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid}}))
}

func (e Engine) observe(ctx context.Context, descriptor Descriptor, desired *appsv1.StatefulSet) (networkv1alpha1.ActorStatus, error) {
	status := networkv1alpha1.ActorStatus{ObservedGeneration: descriptor.Owner.GetGeneration(), Phase: "Progressing"}
	if descriptor.Suspended {
		status.Phase = "Suspended"
		return status, nil
	}
	statefulSet := &appsv1.StatefulSet{}
	if err := e.Reader.Get(ctx, client.ObjectKeyFromObject(desired), statefulSet); err != nil {
		return status, err
	}
	pods := &corev1.PodList{}
	if err := e.Reader.List(ctx, pods, client.InNamespace(descriptor.Owner.GetNamespace()), client.MatchingLabels{
		operatorlabels.ManagedByKey: operatorlabels.ManagedByValue, operatorlabels.NetworkKey: descriptor.Network,
		operatorlabels.ActorKey: descriptor.Actor, operatorlabels.ActorResourceKey: descriptor.Owner.GetName(),
	}); err != nil {
		return status, err
	}
	if len(pods.Items) != 1 || statefulSet.Status.ObservedGeneration != statefulSet.Generation || statefulSet.Status.ReadyReplicas != 1 || statefulSet.Status.CurrentRevision == "" || statefulSet.Status.CurrentRevision != statefulSet.Status.UpdateRevision {
		return status, nil
	}
	pod := &pods.Items[0]
	if !metav1.IsControlledBy(pod, statefulSet) || !podReady(pod) || pod.Labels[appsv1.StatefulSetRevisionLabel] != statefulSet.Status.CurrentRevision {
		return status, nil
	}
	requestedImage := actorContainerImage(statefulSet.Spec.Template.Spec.Containers)
	if requestedImage != descriptor.Image || actorContainerImage(pod.Spec.Containers) != requestedImage {
		return status, nil
	}
	imageID := actorImageID(pod)
	match := immutableImageID(imageID)
	if match == "" {
		return status, nil
	}
	configDigest := statefulSet.Spec.Template.Annotations[configDigestAnnotation]
	status.Phase, status.Ready = "Ready", true
	status.Identity = &networkv1alpha1.ActorIdentity{Kind: descriptor.Kind, Name: descriptor.Actor, Role: descriptor.Role,
		ResourceName: descriptor.Owner.GetName(), ServiceName: statefulSet.Spec.ServiceName, StatefulSetName: statefulSet.Name, StatefulSetUID: string(statefulSet.UID),
		ControllerRevision: statefulSet.Status.CurrentRevision, PodName: pod.Name, PodUID: string(pod.UID), RequestedImage: requestedImage,
		RuntimeImageID: match, ConfigDigest: configDigest, SpecDigest: descriptor.SpecDigest}
	return status, nil
}

func immutableImageID(value string) string {
	index := strings.LastIndex(value, "sha256:")
	if index < 0 || len(value[index:]) != 71 {
		return ""
	}
	digest := value[index:]
	for _, character := range strings.TrimPrefix(digest, "sha256:") {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return ""
		}
	}
	return digest
}

func podReady(pod *corev1.Pod) bool {
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady {
			return condition.Status == corev1.ConditionTrue
		}
	}
	return false
}
func actorImageID(pod *corev1.Pod) string {
	for _, status := range pod.Status.ContainerStatuses {
		if status.Name == "actor" {
			return status.ImageID
		}
	}
	return ""
}
func actorContainerImage(containers []corev1.Container) string {
	for _, container := range containers {
		if container.Name == "actor" {
			return container.Image
		}
	}
	return ""
}
func wrapApply(kind, name string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("apply %s %s: %w", kind, name, err)
}
