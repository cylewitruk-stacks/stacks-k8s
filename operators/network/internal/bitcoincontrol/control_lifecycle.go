package bitcoincontrol

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"time"

	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/foundation"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/naming"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/participantstatus"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

const (
	// ControlPodFinalizer retains kubelet termination evidence across lost status writes.
	ControlPodFinalizer = "network.stacks.org/bitcoin-control-termination"
	// ControlParticipantFinalizer retains the status owner until control processes terminate.
	ControlParticipantFinalizer = "network.stacks.org/bitcoin-control"
	// ControlLifecycleManager owns only participant status.bitcoinControl.
	ControlLifecycleManager = "stacks-network-domain-bitcoincontrol"
	controlPodLimit         = 128
)

// ReconcileControlLifecycle observes exact control processes before releasing their evidence.
// Call before workload mutation and after creation, including terminal paths without enrollment.
func (r *WorkloadReconciler) ReconcileControlLifecycle(ctx context.Context, p *api.StacksNetworkParticipant, root *api.StacksNetwork) (ctrl.Result, error) {
	if root.UID != p.Spec.NetworkUID || !metav1.IsControlledBy(p, root) {
		return ctrl.Result{}, fmt.Errorf("control participant ownership unavailable")
	}
	if p.DeletionTimestamp == nil && !controllerutil.ContainsFinalizer(p, ControlParticipantFinalizer) {
		base := p.DeepCopy()
		controllerutil.AddFinalizer(p, ControlParticipantFinalizer)
		if err := r.Client.Patch(ctx, p, client.MergeFromWithOptions(base, client.MergeFromWithOptimisticLock{})); err != nil {
			return ctrl.Result{}, err
		}
	}
	state := api.BitcoinControlRuntimeStatus{}
	if p.Status.BitcoinControl != nil {
		state = *p.Status.BitcoinControl
		state.Pods = slices.Clone(state.Pods)
	}
	previouslyTerminated := state.Terminated
	deployments := map[types.UID]common.Binding{}
	if state.DeploymentRef != nil {
		deployments[state.DeploymentRef.UID] = *state.DeploymentRef
	}
	for _, pod := range state.Pods {
		deployments[pod.Deployment.UID] = pod.Deployment
	}
	state.ObservedGeneration, state.NetworkGeneration = p.Generation, root.Generation
	state.Terminated, state.Reason = false, "TerminationUnknown"
	finish := func(reason string, err error) (ctrl.Result, error) {
		state.Reason = reason
		if !equality.Semantic.DeepEqual(p.Status.BitcoinControl, &state) {
			if applyErr := participantstatus.Apply(ctx, r.Client, p, api.ParticipantStatus{BitcoinControl: &state}, ControlLifecycleManager); applyErr != nil {
				return ctrl.Result{}, applyErr
			}
		}
		result := ctrl.Result{}
		if !state.Terminated {
			result.RequeueAfter = 5 * time.Second
		}
		return result, err
	}
	name := controlDeploymentName(p)
	var deployment appsv1.Deployment
	err := r.Reader.Get(ctx, client.ObjectKey{Namespace: p.Namespace, Name: name}, &deployment)
	absent := apierrors.IsNotFound(err)
	if err != nil && !absent {
		return finish("TerminationUnknown", err)
	}
	if !absent {
		if !metav1.IsControlledBy(&deployment, p) || deployment.UID == "" {
			return finish("OwnershipConflict", fmt.Errorf("control Deployment identity differs"))
		}
		ref := binding("Deployment", &deployment)
		state.DeploymentRef = &ref
		deployments[ref.UID] = ref
	}
	var list corev1.PodList
	selector := client.MatchingLabels{api.LabelParticipantUID: string(p.UID), api.LabelNetworkUID: string(root.UID), workerRoleLabel: "bitcoin-control"}
	var replicas appsv1.ReplicaSetList
	if err := r.Reader.List(ctx, &replicas, client.InNamespace(p.Namespace), selector, client.Limit(controlPodLimit+1)); err != nil {
		return finish("TerminationUnknown", err)
	}
	if replicas.Continue != "" || len(replicas.Items) > controlPodLimit {
		return finish("TerminationUnknown", fmt.Errorf("control ReplicaSet inventory exceeds bound"))
	}
	replicasQuiesced := true
	for i := range replicas.Items {
		replica := &replicas.Items[i]
		parent := metav1.GetControllerOf(replica)
		if parent == nil {
			return finish("OwnershipConflict", fmt.Errorf("control ReplicaSet owner unavailable"))
		}
		ref, verified := deployments[parent.UID]
		if !verified || !controllerMatches(replica, "Deployment", ref.Name, ref.UID) {
			return finish("OwnershipConflict", fmt.Errorf("control ReplicaSet identity unavailable"))
		}
		replicasQuiesced = replicasQuiesced && replicaQuiesced(replica)
	}
	if err := r.Reader.List(ctx, &list, client.InNamespace(p.Namespace), selector, client.Limit(controlPodLimit+1)); err != nil {
		return finish("TerminationUnknown", err)
	}
	if list.Continue != "" || len(list.Items) > controlPodLimit {
		return finish("TerminationUnknown", fmt.Errorf("control Pod inventory exceeds bound"))
	}
	pods := make(map[string]*corev1.Pod, len(list.Items))
	for i := range list.Items {
		pods[string(list.Items[i].UID)] = &list.Items[i]
	}
	previous := make(map[string]api.BitcoinControlPodStatus, len(state.Pods))
	missing := false
	for _, known := range state.Pods {
		previous[known.UID] = known
		if _, ok := pods[known.UID]; ok {
			continue
		}
		var pod corev1.Pod
		err := r.Reader.Get(ctx, client.ObjectKey{Namespace: p.Namespace, Name: known.Pod.Name}, &pod)
		if err != nil && !apierrors.IsNotFound(err) {
			return finish("TerminationUnknown", err)
		}
		if err == nil && string(pod.UID) == known.UID {
			pods[known.UID] = &pod
		} else if !known.Terminated {
			missing = true
		}
	}
	observed := make([]api.BitcoinControlPodStatus, 0, len(pods))
	var release []*corev1.Pod
	allTerminated := !missing
	unknown := missing
	for uid, pod := range pods {
		owner := metav1.GetControllerOf(pod)
		if owner == nil || owner.APIVersion != appsv1.SchemeGroupVersion.String() || owner.Kind != "ReplicaSet" {
			return finish("OwnershipConflict", fmt.Errorf("control Pod ReplicaSet identity unavailable"))
		}
		known, pinned := previous[uid]
		if pinned && (known.Pod.Name != pod.Name || known.ReplicaSet.Name != owner.Name || known.ReplicaSet.UID != owner.UID) {
			return finish("OwnershipConflict", fmt.Errorf("bound control Pod owner differs"))
		}
		var replica appsv1.ReplicaSet
		deploymentRef := known.Deployment
		err := r.Reader.Get(ctx, client.ObjectKey{Namespace: p.Namespace, Name: owner.Name}, &replica)
		if err != nil {
			if !apierrors.IsNotFound(err) || !pinned || !controlPodTerminated(pod) {
				return finish("TerminationUnknown", err)
			}
		} else {
			replicasQuiesced = replicasQuiesced && replicaQuiesced(&replica)
			parent := metav1.GetControllerOf(&replica)
			if parent == nil {
				return finish("OwnershipConflict", fmt.Errorf("control ReplicaSet owner unavailable"))
			}
			ref, verified := deployments[parent.UID]
			if !verified || replica.UID != owner.UID || !controllerMatches(&replica, "Deployment", ref.Name, ref.UID) || pinned && ref != known.Deployment {
				return finish("OwnershipConflict", fmt.Errorf("control ReplicaSet Deployment identity differs"))
			}
			deploymentRef = ref
		}
		terminal := !controlPodUnknown(pod) && controlPodTerminated(pod)
		unknown = unknown || controlPodUnknown(pod)
		if pinned && known.Terminated && !terminal {
			return finish("TerminationUnknown", fmt.Errorf("terminal control Pod evidence regressed"))
		}
		observed = append(observed, api.BitcoinControlPodStatus{UID: uid, Pod: binding("Pod", pod), ReplicaSet: common.Binding{Kind: "ReplicaSet", Name: owner.Name, UID: owner.UID}, Deployment: deploymentRef, Terminated: terminal})
		allTerminated = allTerminated && terminal
		if terminal && controllerutil.ContainsFinalizer(pod, ControlPodFinalizer) {
			release = append(release, pod)
		}
	}
	for _, known := range state.Pods {
		if _, ok := pods[known.UID]; !ok && !known.Terminated {
			observed = append(observed, known)
		}
	}
	if len(observed) > controlPodLimit {
		return finish("TerminationUnknown", fmt.Errorf("unresolved control process inventory exceeds bound"))
	}
	sort.Slice(observed, func(i, j int) bool { return observed[i].UID < observed[j].UID })
	state.Pods = observed
	// Foreground destruction may remove controllers before their retained Pods.
	// Only exact-root destruction and a complete, empty controller inventory permit
	// that ordering; actual Pod termination is still required below.
	destroyedControllers := root.DeletionTimestamp != nil && len(replicas.Items) == 0
	quiesced := absent && (state.DeploymentRef == nil || previouslyTerminated || destroyedControllers)
	if !absent {
		quiesced = ptr.Deref(deployment.Spec.Replicas, 1) == 0 && deployment.Status.ObservedGeneration >= deployment.Generation && deployment.Status.Replicas == 0
	}
	state.Terminated = allTerminated && !unknown && quiesced && replicasQuiesced
	reason := "Running"
	if unknown || absent && !quiesced {
		reason = "TerminationUnknown"
	} else if state.Terminated {
		reason = "Terminated"
	} else if stopReason(root, p) != "" || !absent && ptr.Deref(deployment.Spec.Replicas, 1) == 0 {
		reason = "Stopping"
	}
	result, err := finish(reason, nil)
	if err != nil {
		return result, err
	}
	// The status CAS must succeed before a retained Pod can disappear.
	for _, pod := range release {
		base := pod.DeepCopy()
		controllerutil.RemoveFinalizer(pod, ControlPodFinalizer)
		if err := r.Client.Patch(ctx, pod, client.MergeFromWithOptions(base, client.MergeFromWithOptimisticLock{})); err != nil {
			return ctrl.Result{}, err
		}
	}
	if state.Terminated && p.DeletionTimestamp != nil && controllerutil.ContainsFinalizer(p, ControlParticipantFinalizer) {
		base := p.DeepCopy()
		controllerutil.RemoveFinalizer(p, ControlParticipantFinalizer)
		if err := r.Client.Patch(ctx, p, client.MergeFromWithOptions(base, client.MergeFromWithOptimisticLock{})); err != nil {
			return ctrl.Result{}, err
		}
	}
	return result, nil
}

// controlPodUnknown distinguishes missing kubelet evidence from an observed live process.
func controlPodUnknown(pod *corev1.Pod) bool {
	if pod.Status.Phase == corev1.PodUnknown || pod.Status.Reason == "NodeLost" {
		return true
	}
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionUnknown {
			return true
		}
	}
	for _, statuses := range [][]corev1.ContainerStatus{pod.Status.ContainerStatuses, pod.Status.InitContainerStatuses, pod.Status.EphemeralContainerStatuses} {
		for _, status := range statuses {
			if status.State.Terminated != nil && status.State.Terminated.Reason == "ContainerStatusUnknown" || status.State.Waiting != nil && status.State.Waiting.Reason == "ContainerStatusUnknown" {
				return true
			}
		}
	}
	return false
}

// replicaQuiesced excludes replacement Pods from retained old Deployment epochs.
func replicaQuiesced(replica *appsv1.ReplicaSet) bool {
	return ptr.Deref(replica.Spec.Replicas, 1) == 0 && replica.Status.ObservedGeneration >= replica.Generation && replica.Status.Replicas == 0
}

// controlDeploymentName selects the participant's deterministic control workload.
func controlDeploymentName(p *api.StacksNetworkParticipant) string {
	return naming.RuntimeName(string(p.Spec.NetworkUID), string(p.UID), string(api.ParticipantBitcoinNode), p.Spec.ParticipantName, "control")
}

// controllerMatches checks the full supported workload-controller binding.
func controllerMatches(object metav1.Object, kind, name string, uid types.UID) bool {
	owner := metav1.GetControllerOf(object)
	return owner != nil && owner.APIVersion == appsv1.SchemeGroupVersion.String() && owner.Kind == kind && owner.Name == name && owner.UID == uid
}

// controlPodTerminated requires kubelet exit evidence for every declared process.
func controlPodTerminated(pod *corev1.Pod) bool {
	if pod.DeletionTimestamp != nil && pod.Spec.NodeName == "" && len(pod.Status.ContainerStatuses) == 0 && len(pod.Status.InitContainerStatuses) == 0 && len(pod.Status.EphemeralContainerStatuses) == 0 {
		return true
	}
	if pod.Status.Phase != corev1.PodSucceeded && pod.Status.Phase != corev1.PodFailed || len(pod.Spec.Containers) == 0 {
		return false
	}
	confirmed := func(name string, statuses []corev1.ContainerStatus) bool {
		for _, status := range statuses {
			if status.Name == name {
				return status.State.Terminated != nil && status.State.Terminated.Reason != "ContainerStatusUnknown"
			}
		}
		return false
	}
	for _, container := range pod.Spec.Containers {
		if !confirmed(container.Name, pod.Status.ContainerStatuses) {
			return false
		}
	}
	for _, container := range pod.Spec.InitContainers {
		if !confirmed(container.Name, pod.Status.InitContainerStatuses) {
			return false
		}
	}
	for _, container := range pod.Spec.EphemeralContainers {
		if !confirmed(container.Name, pod.Status.EphemeralContainerStatuses) {
			return false
		}
	}
	return true
}

// ControlLifecycleEvents routes process evidence and acknowledged scale-down changes.
func ControlLifecycleEvents() predicate.Predicate {
	return predicate.Funcs{UpdateFunc: func(e event.UpdateEvent) bool {
		if e.ObjectOld.GetUID() != e.ObjectNew.GetUID() || e.ObjectOld.GetGeneration() != e.ObjectNew.GetGeneration() || !equality.Semantic.DeepEqual(e.ObjectOld.GetDeletionTimestamp(), e.ObjectNew.GetDeletionTimestamp()) || !equality.Semantic.DeepEqual(e.ObjectOld.GetOwnerReferences(), e.ObjectNew.GetOwnerReferences()) || !equality.Semantic.DeepEqual(e.ObjectOld.GetFinalizers(), e.ObjectNew.GetFinalizers()) || !equality.Semantic.DeepEqual(e.ObjectOld.GetLabels(), e.ObjectNew.GetLabels()) {
			return true
		}
		switch old := e.ObjectOld.(type) {
		case *corev1.Pod:
			current, ok := e.ObjectNew.(*corev1.Pod)
			return !ok || !equality.Semantic.DeepEqual(old.Status, current.Status)
		case *appsv1.Deployment:
			current, ok := e.ObjectNew.(*appsv1.Deployment)
			return !ok || old.Status.ObservedGeneration != current.Status.ObservedGeneration || old.Status.Replicas != current.Status.Replicas
		}
		return false
	}}
}

// ControlLifecycleRequests maps exact owner chains using cached metadata; routing grants no authority.
func (r *WorkloadReconciler) ControlLifecycleRequests(ctx context.Context, object client.Object) []ctrl.Request {
	current := object
	for range 3 {
		owner := metav1.GetControllerOf(current)
		if owner == nil {
			break
		}
		if owner.Kind == "StacksNetworkParticipant" && owner.APIVersion == api.GroupVersion.String() {
			return []ctrl.Request{{NamespacedName: client.ObjectKey{Namespace: object.GetNamespace(), Name: owner.Name}}}
		}
		var parent client.Object
		switch owner.Kind {
		case "ReplicaSet":
			parent = &appsv1.ReplicaSet{}
		case "Deployment":
			parent = &appsv1.Deployment{}
		default:
			return nil
		}
		if owner.APIVersion != appsv1.SchemeGroupVersion.String() || r.Client.Get(ctx, client.ObjectKey{Namespace: object.GetNamespace(), Name: owner.Name}, parent) != nil || parent.GetUID() != owner.UID {
			break
		}
		current = parent
	}
	labels := object.GetLabels()
	if labels[workerRoleLabel] != "bitcoin-control" || labels[api.LabelNetworkUID] == "" || labels[api.LabelParticipant] == "" {
		return nil
	}
	return []ctrl.Request{{NamespacedName: client.ObjectKey{Namespace: object.GetNamespace(), Name: foundation.ParticipantName(labels[api.LabelNetworkUID], labels[api.LabelParticipant])}}}
}
