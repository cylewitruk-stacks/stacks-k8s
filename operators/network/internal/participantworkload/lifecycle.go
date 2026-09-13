package participantworkload

import (
	"context"
	"fmt"
	"reflect"

	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// reconcileService preserves allocated networking while converging owned selectors.
func (r *Reconciler) reconcileService(ctx context.Context, p *api.StacksNetworkParticipant, desired *corev1.Service) error {
	var current corev1.Service
	err := r.Reader.Get(ctx, client.ObjectKeyFromObject(desired), &current)
	if apierrors.IsNotFound(err) {
		return r.Client.Create(ctx, desired)
	}
	if err != nil {
		return err
	}
	if !owned(&current, p) || current.DeletionTimestamp != nil {
		return fmt.Errorf("Service ownership unavailable")
	}
	base := current.DeepCopy()
	current.Labels = desired.Labels
	current.Spec.Selector = desired.Spec.Selector
	current.Spec.Ports = desired.Spec.Ports
	current.Spec.PublishNotReadyAddresses = desired.Spec.PublishNotReadyAddresses
	if reflect.DeepEqual(base.Labels, current.Labels) && reflect.DeepEqual(base.Spec, current.Spec) {
		return nil
	}
	return r.Client.Patch(ctx, &current, client.MergeFromWithOptions(base, client.MergeFromWithOptimisticLock{}))
}

// reconcileStatefulSet updates only owned workload fields and expands existing claims.
func (r *Reconciler) reconcileStatefulSet(ctx context.Context, p *api.StacksNetworkParticipant, desired *appsv1.StatefulSet) error {
	var current appsv1.StatefulSet
	err := r.Reader.Get(ctx, client.ObjectKeyFromObject(desired), &current)
	if apierrors.IsNotFound(err) {
		return r.Client.Create(ctx, desired)
	}
	if err != nil {
		return err
	}
	if !owned(&current, p) || current.DeletionTimestamp != nil {
		return fmt.Errorf("StatefulSet ownership unavailable")
	}
	// Capture the existing Pod before changing its template or replica count.
	pod, err := r.actorPod(ctx, p, &current)
	if err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	if err == nil && pod.DeletionTimestamp == nil {
		if err := r.retainPod(ctx, pod); err != nil {
			return err
		}
	}
	if len(desired.Spec.VolumeClaimTemplates) != len(current.Spec.VolumeClaimTemplates) {
		return fmt.Errorf("storage mode change requires replacement")
	}
	if len(desired.Spec.VolumeClaimTemplates) == 1 {
		want := desired.Spec.VolumeClaimTemplates[0].Spec.Resources.Requests[corev1.ResourceStorage]
		old := current.Spec.VolumeClaimTemplates[0].Spec.Resources.Requests[corev1.ResourceStorage]
		if want.Cmp(old) < 0 {
			return fmt.Errorf("storage shrink is unsupported")
		}
		var pvc corev1.PersistentVolumeClaim
		if err := r.Reader.Get(ctx, client.ObjectKey{Namespace: p.Namespace, Name: "data-" + current.Name + "-0"}, &pvc); err == nil {
			if pvc.Labels[api.LabelParticipantUID] != string(p.UID) || pvc.Labels[api.LabelNetworkUID] != string(p.Spec.NetworkUID) {
				return fmt.Errorf("PVC identity is foreign")
			}
			if owner := metav1.GetControllerOf(&pvc); owner != nil && owner.UID != current.UID {
				return fmt.Errorf("PVC controller identity is foreign")
			}
			actual := pvc.Spec.Resources.Requests[corev1.ResourceStorage]
			if want.Cmp(actual) < 0 {
				return fmt.Errorf("storage shrink is unsupported")
			}
			if want.Cmp(actual) > 0 {
				base := pvc.DeepCopy()
				pvc.Spec.Resources.Requests[corev1.ResourceStorage] = want
				if err := r.Client.Patch(ctx, &pvc, client.MergeFromWithOptions(base, client.MergeFromWithOptimisticLock{})); err != nil {
					return err
				}
			}
		} else if !apierrors.IsNotFound(err) {
			return err
		}
	}
	base := current.DeepCopy()
	current.Spec.Replicas = desired.Spec.Replicas
	current.Spec.Template = desired.Spec.Template
	current.Spec.PersistentVolumeClaimRetentionPolicy = desired.Spec.PersistentVolumeClaimRetentionPolicy
	if !reflect.DeepEqual(base.Spec, current.Spec) {
		if err := r.Client.Patch(ctx, &current, client.MergeFromWithOptions(base, client.MergeFromWithOptimisticLock{})); err != nil {
			return err
		}
	}
	*desired = current
	return nil
}

// actorPod reads the current Pod and verifies its workload and participant binding.
func (r *Reconciler) actorPod(ctx context.Context, p *api.StacksNetworkParticipant, workload *appsv1.StatefulSet) (*corev1.Pod, error) {
	var pod corev1.Pod
	if err := r.Reader.Get(ctx, client.ObjectKey{Namespace: p.Namespace, Name: workload.Name + "-0"}, &pod); err != nil {
		return nil, err
	}
	owner := metav1.GetControllerOf(&pod)
	if owner == nil || owner.Kind != "StatefulSet" || owner.UID != workload.UID || pod.Labels[api.LabelParticipantUID] != string(p.UID) || pod.Labels[api.LabelNetworkUID] != string(p.Spec.NetworkUID) {
		return nil, fmt.Errorf("actor Pod identity is foreign")
	}
	return &pod, nil
}

// retainPod persists a finalizer before initiating process termination.
func (r *Reconciler) retainPod(ctx context.Context, pod *corev1.Pod) error {
	if controllerutil.ContainsFinalizer(pod, PodFinalizer) {
		return nil
	}
	if pod.DeletionTimestamp != nil {
		return fmt.Errorf("cannot retain already deleting Pod evidence")
	}
	base := pod.DeepCopy()
	controllerutil.AddFinalizer(pod, PodFinalizer)
	return r.Client.Patch(ctx, pod, client.MergeFromWithOptions(base, client.MergeFromWithOptimisticLock{}))
}

// releasePod releases retained termination evidence after a confirmed exit.
func (r *Reconciler) releasePod(ctx context.Context, pod *corev1.Pod) error {
	if !controllerutil.ContainsFinalizer(pod, PodFinalizer) {
		return nil
	}
	base := pod.DeepCopy()
	controllerutil.RemoveFinalizer(pod, PodFinalizer)
	return r.Client.Patch(ctx, pod, client.MergeFromWithOptions(base, client.MergeFromWithOptimisticLock{}))
}

// observePod records exact actor process and image identities.
func observePod(state *api.ParticipantRuntimeStatus, pod *corev1.Pod) {
	state.PodRef = binding("Pod", pod)
	state.PodIP = pod.Status.PodIP
	state.ContainerID, state.ImageID = "", ""
	name := actorContainer(api.ParticipantKind(pod.Labels[api.LabelParticipantKind]))
	for _, container := range pod.Status.ContainerStatuses {
		if container.Name == name {
			state.ContainerID = container.ContainerID
			state.ImageID = container.ImageID
		}
	}
}

// podReady requires a live Pod with acknowledged readiness.
func podReady(pod *corev1.Pod) bool {
	if pod.DeletionTimestamp != nil {
		return false
	}
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady {
			return condition.Status == corev1.ConditionTrue
		}
	}
	return false
}

// terminated distinguishes kubelet terminal evidence from missing or unknown processes.
func terminated(pod *corev1.Pod) bool {
	if pod.DeletionTimestamp != nil && pod.Spec.NodeName == "" && len(pod.Status.ContainerStatuses) == 0 && len(pod.Status.InitContainerStatuses) == 0 {
		return true
	}
	if pod.Status.Phase != corev1.PodSucceeded && pod.Status.Phase != corev1.PodFailed {
		return false
	}
	if len(pod.Spec.Containers) == 0 {
		return false
	}
	for _, declared := range pod.Spec.Containers {
		confirmed := false
		for _, status := range pod.Status.ContainerStatuses {
			if status.Name == declared.Name && status.State.Terminated != nil && status.State.Terminated.Reason != "ContainerStatusUnknown" {
				confirmed = true
			}
		}
		if !confirmed {
			return false
		}
	}
	for _, status := range pod.Status.InitContainerStatuses {
		if status.State.Running != nil {
			return false
		}
	}

	return true
}

// stopActor preserves the RPC process until the executor acknowledges terminal drainage.
func (r *Reconciler) stopActor(ctx context.Context, p *api.StacksNetworkParticipant, state *api.ParticipantRuntimeStatus, removing bool) (string, error) {
	// A previously confirmed, absent process cannot need a second drain. Its
	// execution record may already have been garbage-collected during root deletion.
	if state.Terminated {
		var pod corev1.Pod
		err := r.Reader.Get(ctx, client.ObjectKey{Namespace: p.Namespace, Name: Name(p, "actor") + "-0"}, &pod)
		if apierrors.IsNotFound(err) {
			return r.shutdown(ctx, p, state, removing)
		}
		if err != nil {
			return "TerminationUnknown", err
		}
	}
	if r.BeforeStop != nil {
		ready, err := r.BeforeStop(ctx, p)
		if err != nil {
			return "TerminationUnknown", err
		}
		if !ready {
			return "DrainingControl", nil
		}
	}
	return r.shutdown(ctx, p, state, removing)
}

// shutdown scales down and confirms process termination before disposal.
func (r *Reconciler) shutdown(ctx context.Context, p *api.StacksNetworkParticipant, state *api.ParticipantRuntimeStatus, removing bool) (string, error) {
	var workload appsv1.StatefulSet
	err := r.Reader.Get(ctx, client.ObjectKey{Namespace: p.Namespace, Name: Name(p, "actor")}, &workload)
	if apierrors.IsNotFound(err) {
		var remaining corev1.Pod
		podErr := r.Reader.Get(ctx, client.ObjectKey{Namespace: p.Namespace, Name: Name(p, "actor") + "-0"}, &remaining)
		if podErr == nil {
			if state.PodRef == nil || state.PodRef.UID != remaining.UID || !terminated(&remaining) {
				return "TerminationUnknown", nil
			}
			state.Terminated = true
			if !terminationRecorded(p, &remaining) {
				return "Stopping", nil
			}
			if err := r.releasePod(ctx, &remaining); err != nil {
				return "TerminationUnknown", err
			}
		} else if !apierrors.IsNotFound(podErr) {
			return "TerminationUnknown", podErr
		}
		if state.PodRef != nil && !state.Terminated {
			return "TerminationUnknown", nil
		}
		state.Terminated = true
		return "Terminated", nil
	}
	if err != nil {
		return "TerminationUnknown", err
	}
	if !owned(&workload, p) {
		return "OwnershipConflict", fmt.Errorf("cannot stop foreign StatefulSet")
	}
	pod, podErr := r.actorPod(ctx, p, &workload)
	if podErr != nil && !apierrors.IsNotFound(podErr) {
		return "TerminationUnknown", podErr
	}
	if podErr == nil {
		if state.PodRef != nil && state.PodRef.UID != pod.UID && !state.Terminated {
			if ptr.Deref(workload.Spec.Replicas, 1) != 0 {
				base := workload.DeepCopy()
				workload.Spec.Replicas = ptr.To[int32](0)
				if err := r.Client.Patch(ctx, &workload, client.MergeFromWithOptions(base, client.MergeFromWithOptimisticLock{})); err != nil {
					return "TerminationUnknown", err
				}
			}
			return "TerminationUnknown", nil
		}
		observePod(state, pod)
		state.Terminated = terminated(pod)
		if !state.Terminated && pod.DeletionTimestamp == nil {
			if err := r.retainPod(ctx, pod); err != nil {
				return "TerminationUnknown", err
			}
		}
	}
	if ptr.Deref(workload.Spec.Replicas, 1) != 0 {
		base := workload.DeepCopy()
		workload.Spec.Replicas = ptr.To[int32](0)
		if err := r.Client.Patch(ctx, &workload, client.MergeFromWithOptions(base, client.MergeFromWithOptimisticLock{})); err != nil {
			return "Stopping", err
		}
		return "Stopping", nil
	}
	if podErr != nil {
		if state.PodRef != nil && !state.Terminated {
			return "TerminationUnknown", nil
		}
		// A never-scheduled replica can be absent after a scale-down is observed.
		if workload.Status.ObservedGeneration < workload.Generation || workload.Status.Replicas != 0 {
			return "Stopping", nil
		}
		state.Terminated = true
	}
	if !state.Terminated {
		return "Stopping", nil
	}
	if podErr == nil {
		if !terminationRecorded(p, pod) {
			return "Stopping", nil
		}
		if err := r.releasePod(ctx, pod); err != nil {
			return "Stopping", err
		}
		return "Stopping", nil
	}
	if removing && workload.DeletionTimestamp == nil {
		uid := workload.UID
		if err := r.Client.Delete(ctx, &workload, &client.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid}}); err != nil && !apierrors.IsNotFound(err) {
			return "Stopping", err
		}
	}
	return "Terminated", nil
}

// terminationRecorded requires durable evidence before deleting its source Pod.
func terminationRecorded(p *api.StacksNetworkParticipant, pod *corev1.Pod) bool {
	runtime := p.Status.Runtime
	return runtime != nil && runtime.Terminated && runtime.PodRef != nil && runtime.PodRef.UID == pod.UID && runtime.PodRef.Name == pod.Name
}
