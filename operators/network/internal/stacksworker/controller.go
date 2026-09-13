package stacksworker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/foundation"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/participantstatus"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// Reconciler owns standalone worker resources and runtime facts, never root or execution status.
type Reconciler struct {
	// Client writes owned workloads and minimal domain status.
	Client client.Client
	// Reader supplies uncached identity and process observations.
	Reader client.Reader
	// Kind selects one management role.
	Kind api.ParticipantKind
	// ResolveProfile resolves immutable bootstrap/key identities before binding only.
	ResolveProfile func(context.Context, *api.StacksNetwork, *api.StacksNetworkParticipant) (Profile, error)
	// ResolveReads selects current named dependencies; pending operations retain earlier grants.
	ResolveReads func(context.Context, *api.StacksNetwork, *api.StacksNetworkParticipant) ([]ReadBinding, error)
}

// Supported identifies participant roles using the non-restarting Stacks worker contract.
func Supported(kind api.ParticipantKind) bool {
	return foundation.ManagementKind(kind)
}

// Reconcile provisions only inactive candidates and disposes only acknowledged bound sessions.
func (r *Reconciler) Reconcile(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {
	var p api.StacksNetworkParticipant
	if err := r.Reader.Get(ctx, request.NamespacedName, &p); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if p.Spec.Kind != r.Kind || !Supported(p.Spec.Kind) {
		return ctrl.Result{}, nil
	}
	state := api.ParticipantRuntimeStatus{ObservedGeneration: p.Generation}
	if p.Status.Runtime != nil {
		state = *p.Status.Runtime
		state.ObservedGeneration = p.Generation
	}
	var placement *metav1.Condition
	finish := func(status metav1.ConditionStatus, reason string) (ctrl.Result, error) {
		conditions := []metav1.Condition{}
		for _, c := range p.Status.Conditions {
			if c.Type == "WorkloadReady" || c.Type == "PlacementReady" {
				conditions = append(conditions, c)
			}
		}
		if placement != nil {
			meta.SetStatusCondition(&conditions, *placement)
		}
		meta.SetStatusCondition(&conditions, metav1.Condition{Type: "WorkloadReady", Status: status, Reason: reason, Message: reason, ObservedGeneration: p.Generation})
		owned := api.ParticipantStatus{Runtime: &state, Conditions: conditions}
		result := ctrl.Result{}
		if status == metav1.ConditionUnknown || reason == "WorkerDraining" || reason == "WorkerDisposing" {
			result.RequeueAfter = 5 * time.Second
		}
		if reflect.DeepEqual(p.Status.Runtime, owned.Runtime) && reflect.DeepEqual(conditions, workerConditions(p.Status.Conditions)) {
			return result, nil
		}
		return result, participantstatus.Apply(ctx, r.Client, &p, owned, "stacks-network-domain-"+strings.ToLower(string(r.Kind)))
	}
	var root api.StacksNetwork
	if err := r.Reader.Get(ctx, client.ObjectKey{Namespace: p.Namespace, Name: "network"}, &root); err != nil {
		return finish(metav1.ConditionUnknown, "RootIdentityUnavailable")
	}
	id, err := Session(&root, &p)
	if errors.Is(err, errParticipantAllocating) {
		return finish(metav1.ConditionUnknown, "AllocationPending")
	}
	if err != nil {
		return finish(metav1.ConditionFalse, "WorkerIdentityLost")
	}
	var pod corev1.Pod
	podErr := r.Reader.Get(ctx, client.ObjectKey{Namespace: p.Namespace, Name: Name(&p)}, &pod)
	if podErr != nil && !apierrors.IsNotFound(podErr) {
		return finish(metav1.ConditionUnknown, "WorkerObservationUnavailable")
	}
	if podErr == nil {
		placement = &metav1.Condition{Type: "PlacementReady", Status: metav1.ConditionUnknown, Reason: "Pending", Message: "Waiting for scheduler observation", ObservedGeneration: p.Generation}
		if previous := meta.FindStatusCondition(p.Status.Conditions, "PlacementReady"); previous != nil && previous.Status == metav1.ConditionFalse && previous.Reason == "PlacementError" {
			placement = previous.DeepCopy()
		} else if pod.Spec.NodeName != "" {
			placement.Status, placement.Reason, placement.Message = metav1.ConditionTrue, "Scheduled", "Worker is scheduled"
		} else {
			for _, condition := range pod.Status.Conditions {
				if condition.Type == corev1.PodScheduled && condition.Status == corev1.ConditionFalse && condition.Reason == corev1.PodReasonUnschedulable {
					placement.Status, placement.Reason, placement.Message = metav1.ConditionFalse, "PlacementError", "Worker placement requirements cannot be satisfied"
				}
			}
		}
	}
	if id.Worker != nil {
		session := id.Worker
		state.WorkerCandidate = &api.WorkerCandidate{Pod: session.Pod, ProfileDigest: session.ProfileDigest}
		if apierrors.IsNotFound(podErr) {
			if session.Disposal == nil || !session.Disposal.Terminated {
				return finish(metav1.ConditionFalse, "BoundWorkerLost")
			}
			state.Terminated = true
			if stoppedReason(&root, &p, id) == api.WorkerShutdownParticipantRemoved || p.DeletionTimestamp != nil || root.DeletionTimestamp != nil {
				if err := r.releaseParticipant(ctx, &p); err != nil {
					return ctrl.Result{}, err
				}
			}
			return finish(metav1.ConditionFalse, "WorkerDisposed")
		}
		if pod.UID != session.Pod.UID || !ownedPod(&pod, &p) || pod.Annotations[profileLabel] != session.ProfileDigest {
			return finish(metav1.ConditionFalse, "BoundWorkerReplaced")
		}
		profile, err := profileFromPod(&pod)
		if err != nil && session.Disposal == nil && session.Shutdown == nil {
			return finish(metav1.ConditionFalse, "WorkerProfileChanged")
		}
		if err := r.profileAvailable(ctx, p.Namespace, profile); err != nil && session.Shutdown == nil && session.Disposal == nil && stoppedReason(&root, &p, id) == "" {
			return finish(metav1.ConditionUnknown, "WorkerInputsUnavailable")
		}
		state.PodRef = objectBinding("Pod", &pod)
		state.WorkloadRefs = []common.Binding{*state.PodRef}
		if err == nil {
			state.ConfigRef = &profile.Configuration
		}
		if session.Disposal != nil {
			if pod.DeletionTimestamp == nil {
				uid := pod.UID
				if err := r.Client.Delete(ctx, &pod, &client.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid}}); err != nil && !apierrors.IsNotFound(err) {
					return ctrl.Result{}, err
				}
				return finish(metav1.ConditionFalse, "WorkerDisposing")
			}
			state.Terminated = Terminated(&pod)
			if state.Terminated && session.Disposal.Terminated {
				if controllerutil.ContainsFinalizer(&pod, PodFinalizer) {
					base := pod.DeepCopy()
					controllerutil.RemoveFinalizer(&pod, PodFinalizer)
					if err := r.Client.Patch(ctx, &pod, client.MergeFromWithOptions(base, client.MergeFromWithOptimisticLock{})); err != nil {
						return ctrl.Result{}, err
					}
				}
			}
			return finish(metav1.ConditionFalse, "WorkerDisposing")
		}
		if terminal(&pod) || pod.DeletionTimestamp != nil {
			return finish(metav1.ConditionFalse, "BoundWorkerExited")
		}
		if session.Shutdown != nil {
			return finish(metav1.ConditionFalse, "WorkerDraining")
		}
		if err := r.extraCandidates(ctx, &p); err != nil {
			return finish(metav1.ConditionFalse, "CandidateConflict")
		}
		if r.ResolveReads != nil {
			reads, err := r.ResolveReads(ctx, &root, &p)
			if err != nil {
				return finish(metav1.ConditionUnknown, "WorkerDependenciesUnavailable")
			}
			profile.Reads = reads
			if err := r.ensureSupport(ctx, &p, profile); err != nil {
				return finish(metav1.ConditionUnknown, "WorkerSupportUnavailable")
			}
		}
		if execution := p.Status.Execution; execution != nil && execution.PodUID == pod.UID && execution.ProfileDigest == session.ProfileDigest && execution.ProcessNonce != "" && (execution.Phase == api.WorkerPhaseActive || execution.Phase == api.WorkerPhasePaused) {
			return finish(metav1.ConditionTrue, "WorkerBound")
		}
		return finish(metav1.ConditionFalse, "WorkerInactive")
	}
	if p.Status.Execution != nil && p.Status.Execution.Phase != api.WorkerPhaseInactive {
		return finish(metav1.ConditionFalse, "WorkerBindingLost")
	}
	if stoppedReason(&root, &p, id) != "" {
		if podErr == nil {
			if !ownedPod(&pod, &p) {
				return finish(metav1.ConditionFalse, "CandidateConflict")
			}
			if _, err := profileFromPod(&pod); err != nil {
				return finish(metav1.ConditionFalse, "CandidateConflict")
			}
			// An exact framework candidate cannot activate without a durable root binding.
			if pod.DeletionTimestamp == nil {
				uid := pod.UID
				if err := r.Client.Delete(ctx, &pod, &client.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid}}); err != nil {
					return ctrl.Result{}, err
				}
			}
			if !Terminated(&pod) {
				return finish(metav1.ConditionUnknown, "InactiveCandidateTerminating")
			}
			base := pod.DeepCopy()
			controllerutil.RemoveFinalizer(&pod, PodFinalizer)
			if err := r.Client.Patch(ctx, &pod, client.MergeFromWithOptions(base, client.MergeFromWithOptimisticLock{})); err != nil {
				return ctrl.Result{}, err
			}
		} else {
			state.Terminated = true
			if err := r.releaseParticipant(ctx, &p); err != nil {
				return ctrl.Result{}, err
			}
		}
		return finish(metav1.ConditionFalse, "InactiveCandidateDisposed")
	}
	if root.Spec.Operation != api.NetworkOperationRunning || failed(&root) || root.Status.GenesisRef == nil {
		return finish(metav1.ConditionFalse, "WorkerActivationHeld")
	}
	if err := foundation.ValidateParticipantAdmission(ctx, r.Reader, &root, &p); err != nil {
		return finish(metav1.ConditionFalse, "AdmissionUnavailable")
	}
	if !controllerutil.ContainsFinalizer(&p, Finalizer) {
		base := p.DeepCopy()
		controllerutil.AddFinalizer(&p, Finalizer)
		if err := r.Client.Patch(ctx, &p, client.MergeFromWithOptions(base, client.MergeFromWithOptimisticLock{})); err != nil {
			return ctrl.Result{}, err
		}
	}
	if r.ResolveProfile == nil {
		return finish(metav1.ConditionFalse, "WorkerProfileUnavailable")
	}
	profile, err := r.ResolveProfile(ctx, &root, &p)
	if err != nil {
		return finish(metav1.ConditionFalse, "WorkerProfileUnavailable")
	}
	profile, err = profile.Normalize()
	if err != nil {
		return finish(metav1.ConditionFalse, "InvalidWorkerProfile")
	}
	if err := r.profileAvailable(ctx, p.Namespace, profile); err != nil {
		return finish(metav1.ConditionUnknown, "WorkerInputsUnavailable")
	}
	if err := r.extraCandidates(ctx, &p); err != nil {
		return finish(metav1.ConditionFalse, "CandidateConflict")
	}
	if err := r.ensureSupport(ctx, &p, profile); err != nil {
		return finish(metav1.ConditionFalse, "WorkerSupportUnavailable")
	}
	desired, err := Pod(&p, profile)
	if err != nil {
		return finish(metav1.ConditionFalse, "InvalidWorkerProfile")
	}
	if apierrors.IsNotFound(podErr) {
		if err := r.Client.Create(ctx, desired); err != nil && !apierrors.IsAlreadyExists(err) {
			return ctrl.Result{}, err
		}
		if err := r.Reader.Get(ctx, client.ObjectKeyFromObject(desired), &pod); err != nil {
			return ctrl.Result{}, err
		}
	}
	if !ownedPod(&pod, &p) || pod.Annotations[profileLabel] != profile.Digest() {
		return finish(metav1.ConditionFalse, "CandidateConflict")
	}
	if _, err := profileFromPod(&pod); err != nil {
		return finish(metav1.ConditionFalse, "CandidateConflict")
	}
	if terminal(&pod) || pod.DeletionTimestamp != nil {
		if pod.DeletionTimestamp == nil {
			uid := pod.UID
			if err := r.Client.Delete(ctx, &pod, &client.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid}}); err != nil {
				return ctrl.Result{}, err
			}
			return finish(metav1.ConditionUnknown, "InactiveCandidateTerminating")
		}
		if !Terminated(&pod) {
			return finish(metav1.ConditionUnknown, "InactiveCandidateTerminating")
		}
		base := pod.DeepCopy()
		controllerutil.RemoveFinalizer(&pod, PodFinalizer)
		if err := r.Client.Patch(ctx, &pod, client.MergeFromWithOptions(base, client.MergeFromWithOptimisticLock{})); err != nil {
			return ctrl.Result{}, err
		}
		state.WorkerCandidate, state.PodRef = nil, nil
		return finish(metav1.ConditionUnknown, "CandidateRetryPending")
	}
	state.WorkerCandidate = &api.WorkerCandidate{Pod: podBinding(&pod), ProfileDigest: profile.Digest()}
	state.PodRef = objectBinding("Pod", &pod)
	state.WorkloadRefs = []common.Binding{*state.PodRef}
	state.ConfigRef = &profile.Configuration
	state.Terminated = false
	return finish(metav1.ConditionFalse, "CandidateInactive")
}

// profileFromPod validates the retained immutable bootstrap manifest against mutable image fields.
func profileFromPod(pod *corev1.Pod) (Profile, error) {
	var profile Profile
	raw := pod.Annotations[profileJSONAnnotation]
	if len(raw) > 48*1024 || json.Unmarshal([]byte(raw), &profile) != nil {
		return profile, fmt.Errorf("worker profile missing")
	}
	profile, err := profile.Normalize()
	if err != nil {
		return profile, err
	}
	if profile.Digest() != pod.Annotations[profileLabel] || pod.Spec.RestartPolicy != corev1.RestartPolicyNever || len(pod.Spec.Containers) != 1 || pod.Spec.Containers[0].Name != "worker" || pod.Spec.Containers[0].Image != profile.Image || len(pod.Spec.InitContainers) != 0 || len(pod.Spec.EphemeralContainers) != 0 {
		return profile, fmt.Errorf("worker profile changed")
	}
	owner := metav1.GetControllerOf(pod)
	if owner == nil {
		return profile, fmt.Errorf("worker owner unavailable")
	}
	p := &api.StacksNetworkParticipant{ObjectMeta: metav1.ObjectMeta{Name: owner.Name, Namespace: pod.Namespace, UID: owner.UID}, Spec: api.StacksNetworkParticipantSpec{NetworkUID: types.UID(pod.Labels[api.LabelNetworkUID]), ParticipantName: pod.Labels[api.LabelParticipant], Kind: api.ParticipantKind(pod.Labels[api.LabelParticipantKind])}}
	wanted, err := Pod(p, profile)
	if err != nil {
		return profile, err
	}
	actual, expected := pod.Spec.Containers[0], wanted.Spec.Containers[0]
	if pod.Name != wanted.Name || pod.Spec.ServiceAccountName != wanted.Spec.ServiceAccountName || !reflect.DeepEqual(actual.Command, expected.Command) || !reflect.DeepEqual(actual.Args, expected.Args) || !reflect.DeepEqual(actual.Env, expected.Env) || len(actual.EnvFrom) != 0 || !reflect.DeepEqual(actual.SecurityContext, expected.SecurityContext) || !reflect.DeepEqual(actual.Resources, expected.Resources) {
		return profile, fmt.Errorf("worker process configuration changed")
	}
	// Kubernetes injects the service-account volume; compare every explicitly declared mount.
	for _, mount := range expected.VolumeMounts {
		found := false
		for _, current := range actual.VolumeMounts {
			if reflect.DeepEqual(current, mount) {
				found = true
				break
			}
		}
		if !found {
			return profile, fmt.Errorf("worker key/config mount changed")
		}
	}
	for _, mount := range actual.VolumeMounts {
		found := false
		for _, expected := range expected.VolumeMounts {
			if reflect.DeepEqual(mount, expected) {
				found = true
				break
			}
		}
		if !found && !(strings.HasPrefix(mount.Name, "kube-api-access-") && mount.MountPath == "/var/run/secrets/kubernetes.io/serviceaccount" && mount.ReadOnly) {
			return profile, fmt.Errorf("unexpected worker key/config mount")
		}
	}
	for _, volume := range wanted.Spec.Volumes {
		found := false
		for _, current := range pod.Spec.Volumes {
			if reflect.DeepEqual(current, volume) {
				found = true
				break
			}
		}
		if !found {
			return profile, fmt.Errorf("worker key/config volume changed")
		}
	}
	for _, volume := range pod.Spec.Volumes {
		found := false
		for _, expected := range wanted.Spec.Volumes {
			if reflect.DeepEqual(volume, expected) {
				found = true
				break
			}
		}
		if !found {
			if !strings.HasPrefix(volume.Name, "kube-api-access-") || volume.Projected == nil {
				return profile, fmt.Errorf("unexpected worker volume")
			}
			for _, source := range volume.Projected.Sources {
				if source.Secret != nil || (source.ConfigMap != nil && source.ConfigMap.Name != "kube-root-ca.crt") {
					return profile, fmt.Errorf("unexpected service account projection")
				}
			}
		}
	}
	if pod.Spec.HostNetwork || pod.Spec.HostPID || pod.Spec.HostIPC || !reflect.DeepEqual(pod.Spec.SecurityContext, wanted.Spec.SecurityContext) {
		return profile, fmt.Errorf("worker isolation changed")
	}
	return profile, nil
}

// profileAvailable reads public config and key metadata only in the shared controller.
func (r *Reconciler) profileAvailable(ctx context.Context, namespace string, profile Profile) error {
	var configuration corev1.ConfigMap
	if err := r.Reader.Get(ctx, client.ObjectKey{Namespace: namespace, Name: profile.Configuration.Name}, &configuration); err != nil {
		return err
	}
	if configuration.UID != profile.Configuration.UID || configuration.DeletionTimestamp != nil || !ptr.Deref(configuration.Immutable, false) {
		return fmt.Errorf("immutable worker config unavailable")
	}
	for _, key := range profile.Keys {
		metadata := &metav1.PartialObjectMetadata{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Secret"}}
		if err := r.Reader.Get(ctx, client.ObjectKey{Namespace: namespace, Name: key.Secret.Name}, metadata); err != nil {
			return err
		}
		if metadata.UID != key.Secret.UID || metadata.DeletionTimestamp != nil {
			return fmt.Errorf("worker key identity changed")
		}
	}
	return nil
}

// ensureSupport maintains named public reads while preserving pending-operation grants.
func (r *Reconciler) ensureSupport(ctx context.Context, p *api.StacksNetworkParticipant, profile Profile) error {
	profile, err := profile.Normalize()
	if err != nil {
		return err
	}
	meta := metadata(p)
	objects := []client.Object{&corev1.ServiceAccount{ObjectMeta: meta}, &rbacv1.Role{ObjectMeta: meta, Rules: Rules(p, profile)}, &rbacv1.RoleBinding{ObjectMeta: meta, RoleRef: rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "Role", Name: meta.Name}, Subjects: []rbacv1.Subject{{Kind: "ServiceAccount", Name: meta.Name, Namespace: p.Namespace}}}}
	for _, object := range objects {
		if err := r.Client.Create(ctx, object); err != nil {
			if !apierrors.IsAlreadyExists(err) {
				return err
			}
			current := object.DeepCopyObject().(client.Object)
			if err := r.Reader.Get(ctx, client.ObjectKeyFromObject(object), current); err != nil {
				return err
			}
			owner := metav1.GetControllerOf(current)
			if owner == nil || owner.UID != p.UID || current.GetDeletionTimestamp() != nil {
				return fmt.Errorf("foreign worker support resource")
			}
			if role, ok := current.(*rbacv1.Role); ok {
				rules := Rules(p, profile)
				if p.Status.Execution != nil && (p.Status.Execution.Pending > 0 || p.Status.Admission == nil || p.Status.Execution.AppliedPolicyDigest != p.Status.Admission.PolicyDigest) {
					for _, old := range role.Rules {
						if !safeRule(p, old) {
							return fmt.Errorf("worker prior permissions exceed scope")
						}
						found := false
						for _, rule := range rules {
							if reflect.DeepEqual(old, rule) {
								found = true
								break
							}
						}
						if !found {
							rules = append(rules, old)
						}
					}
				}
				if len(rules) > 104 {
					return fmt.Errorf("worker dependency grant bound exceeded")
				}
				if !reflect.DeepEqual(role.Rules, rules) {
					base := role.DeepCopy()
					role.Rules = rules
					if err := r.Client.Patch(ctx, role, client.MergeFromWithOptions(base, client.MergeFromWithOptimisticLock{})); err != nil {
						return err
					}
				}
			}
			if binding, ok := current.(*rbacv1.RoleBinding); ok {
				wanted := object.(*rbacv1.RoleBinding)
				if binding.RoleRef != wanted.RoleRef || !reflect.DeepEqual(binding.Subjects, wanted.Subjects) {
					return fmt.Errorf("worker role binding changed")
				}
			}
		}
	}
	return nil
}

// extraCandidates removes only never-scheduled extras; every uncertain process is a conflict.
func (r *Reconciler) extraCandidates(ctx context.Context, p *api.StacksNetworkParticipant) error {
	var pods corev1.PodList
	if err := r.Reader.List(ctx, &pods, client.InNamespace(p.Namespace), client.MatchingLabels{api.LabelParticipantUID: string(p.UID), workloadLabel: "stacks-worker"}, client.Limit(3)); err != nil {
		return err
	}
	if pods.Continue != "" || len(pods.Items) > 2 {
		return fmt.Errorf("worker candidate inventory conflicts")
	}
	for i := range pods.Items {
		pod := &pods.Items[i]
		if pod.Name == Name(p) {
			continue
		}
		if !ownedPod(pod, p) || pod.Spec.NodeName != "" || len(pod.Status.ContainerStatuses) > 0 || len(pod.Status.InitContainerStatuses) > 0 {
			return fmt.Errorf("extra worker process cannot be proven inactive")
		}
		uid := pod.UID
		if err := r.Client.Delete(ctx, pod, &client.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid}}); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
		var current corev1.Pod
		if err := r.Reader.Get(ctx, client.ObjectKeyFromObject(pod), &current); err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}
			return err
		}
		if current.UID != uid || !ownedPod(&current, p) || !Terminated(&current) {
			return fmt.Errorf("extra worker termination unavailable")
		}
		if controllerutil.ContainsFinalizer(&current, PodFinalizer) {
			base := current.DeepCopy()
			controllerutil.RemoveFinalizer(&current, PodFinalizer)
			if err := r.Client.Patch(ctx, &current, client.MergeFromWithOptions(base, client.MergeFromWithOptimisticLock{})); err != nil {
				return err
			}
		}
	}
	return nil
}

// releaseParticipant removes only this domain's disposal finalizer.
func (r *Reconciler) releaseParticipant(ctx context.Context, p *api.StacksNetworkParticipant) error {
	if !controllerutil.ContainsFinalizer(p, Finalizer) {
		return nil
	}
	base := p.DeepCopy()
	controllerutil.RemoveFinalizer(p, Finalizer)
	return r.Client.Patch(ctx, p, client.MergeFromWithOptions(base, client.MergeFromWithOptimisticLock{}))
}

// objectBinding identifies an observed resource without its private contents.
func objectBinding(kind string, object metav1.Object) *common.Binding {
	return &common.Binding{Kind: kind, Name: object.GetName(), UID: object.GetUID()}
}

// workerConditions selects only the domain's condition ownership.
func workerConditions(conditions []metav1.Condition) []metav1.Condition {
	result := []metav1.Condition{}
	for _, condition := range conditions {
		if condition.Type == "WorkloadReady" || condition.Type == "PlacementReady" {
			result = append(result, condition)
		}
	}
	return result
}

// SetupWithManager watches participant/root intent and exact owned Pod facts.
func (r *Reconciler) SetupWithManager(manager ctrl.Manager) error {
	if r.Client == nil {
		r.Client = manager.GetClient()
	}
	if r.Reader == nil {
		r.Reader = manager.GetAPIReader()
	}
	rootMap := handler.EnqueueRequestsFromMapFunc(func(_ context.Context, object client.Object) []reconcile.Request {
		root, ok := object.(*api.StacksNetwork)
		if !ok {
			return nil
		}
		var out []reconcile.Request
		for _, id := range root.Status.Identities {
			out = append(out, reconcile.Request{NamespacedName: client.ObjectKey{Namespace: root.Namespace, Name: foundation.ParticipantName(string(root.UID), id.Name)}})
		}
		return out
	})
	return ctrl.NewControllerManagedBy(manager).Named("stacks-worker-"+strings.ToLower(string(r.Kind))).For(&api.StacksNetworkParticipant{}).Owns(&corev1.Pod{}).Watches(&api.StacksNetwork{}, rootMap).Complete(r)
}
