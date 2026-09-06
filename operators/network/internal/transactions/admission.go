package transactions

import (
	"context"
	"fmt"
	"net"
	"reflect"
	"strings"

	stacksv1alpha1 "github.com/cylewitruk-stacks/stacks-k8s/apis/network/stacks/v1alpha1"
	networkv1alpha1 "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha1"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/workload"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// admittedTarget pins the address and runtime identity used by one dispatch.
type admittedTarget struct {
	// endpoint is the admitted Pod's RPC address, without mutable Service resolution.
	endpoint string
	// actorUID identifies the StacksNode incarnation.
	actorUID string
	// podUID identifies the admitted Pod incarnation.
	podUID string
	// containerID identifies the container reported by kubelet at admission.
	containerID string
}

// admit uses direct reads and validates only this capability's required actor.
func (r *Reconciler) admit(ctx context.Context, policy *stacksv1alpha1.StacksTransactionProduction, parent *networkv1alpha1.StacksNetwork) (admittedTarget, error) {
	var zero admittedTarget
	if !metav1.IsControlledBy(policy, parent) || policy.Spec.NetworkUID != string(parent.UID) || parent.Status.TransactionProductionUID != string(policy.UID) {
		return zero, fmt.Errorf("production ledger is not bound to this network")
	}
	if !policy.Status.Outstanding && (parent.Spec.StacksTransactionProduction == nil || !reflect.DeepEqual(*parent.Spec.StacksTransactionProduction, policy.Spec.Policy)) {
		return zero, fmt.Errorf("compiled production policy is not current")
	}
	catalog := parent.Status.TargetDeclarations
	if catalog == nil || catalog.SchemaVersion != networkv1alpha1.TargetDeclarationsVersion || catalog.NetworkUID != string(parent.UID) || catalog.ObservedGeneration != parent.Generation {
		return zero, fmt.Errorf("current declaration catalog is unavailable")
	}
	var entry *networkv1alpha1.TargetDeclaration
	for i := range catalog.Actors {
		candidate := &catalog.Actors[i]
		if candidate.Kind == "StacksNode" && candidate.ActorName == policy.Spec.Policy.Target {
			if entry != nil {
				return zero, fmt.Errorf("target declaration is ambiguous")
			}
			entry = candidate
		}
	}
	if entry == nil || entry.Suspended {
		return zero, fmt.Errorf("target is absent or suspended")
	}
	actor := &networkv1alpha1.StacksNode{}
	if err := r.APIReader.Get(ctx, client.ObjectKey{Namespace: parent.Namespace, Name: entry.Name}, actor); err != nil {
		return zero, fmt.Errorf("target cannot be read")
	}
	digest, err := workload.SpecDigest(actor.Spec)
	if err != nil || digest != entry.SpecDigest || !actor.DeletionTimestamp.IsZero() || !metav1.IsControlledBy(actor, parent) || actor.Spec.NetworkRef.Name != parent.Name || actor.Spec.ActorName != entry.ActorName || actor.Spec.Suspended {
		return zero, fmt.Errorf("target declaration or ownership differs")
	}
	identity := actor.Status.Identity
	if !actor.Status.Ready || actor.Status.ObservedGeneration != actor.Generation || identity == nil || identity.SpecDigest != digest || identity.ResourceName != actor.Name {
		return zero, fmt.Errorf("target has no current ready identity")
	}
	ref := actor.Spec.Config.SecretRef
	if ref == nil || ref.ExpectedDigest != r.Profile.ConfigDigest || identity.ConfigDigest != r.Profile.ConfigDigest || actor.Spec.Container != nil {
		return zero, fmt.Errorf("ingress does not use the approved credential/configuration profile")
	}
	statefulSet := &appsv1.StatefulSet{}
	if err := r.APIReader.Get(ctx, client.ObjectKey{Namespace: parent.Namespace, Name: identity.StatefulSetName}, statefulSet); err != nil {
		return zero, fmt.Errorf("target StatefulSet cannot be read")
	}
	if !metav1.IsControlledBy(statefulSet, actor) || !statefulSet.DeletionTimestamp.IsZero() || string(statefulSet.UID) != identity.StatefulSetUID || statefulSet.Status.ObservedGeneration != statefulSet.Generation || statefulSet.Status.CurrentRevision != identity.ControllerRevision || statefulSet.Status.UpdateRevision != identity.ControllerRevision || statefulSet.Status.ReadyReplicas != 1 || statefulSet.Spec.Replicas == nil || *statefulSet.Spec.Replicas != 1 || statefulSet.Spec.Template.Annotations["network.stacks.org/config-digest"] != r.Profile.ConfigDigest {
		return zero, fmt.Errorf("target StatefulSet identity is not current")
	}
	pod := &corev1.Pod{}
	if err := r.APIReader.Get(ctx, client.ObjectKey{Namespace: parent.Namespace, Name: identity.PodName}, pod); err != nil {
		return zero, fmt.Errorf("target Pod cannot be read")
	}
	if !metav1.IsControlledBy(pod, statefulSet) || !pod.DeletionTimestamp.IsZero() || string(pod.UID) != identity.PodUID || pod.Labels[appsv1.StatefulSetRevisionLabel] != identity.ControllerRevision || net.ParseIP(pod.Status.PodIP) == nil {
		return zero, fmt.Errorf("target Pod identity is not current")
	}
	ready := false
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady {
			ready = condition.Status == corev1.ConditionTrue
		}
	}
	imageMatches := false
	for _, container := range pod.Spec.Containers {
		if container.Name == "actor" {
			imageMatches = container.Image == actor.Spec.Image
		}
	}
	containerID := ""
	for _, status := range pod.Status.ContainerStatuses {
		if status.Name == "actor" && status.Ready && status.State.Running != nil && strings.HasSuffix(status.ImageID, identity.RuntimeImageID) {
			containerID = status.ContainerID
		}
	}
	if !ready || !imageMatches || containerID == "" || identity.RuntimeImageID == "" {
		return zero, fmt.Errorf("target Stacks container is not ready")
	}
	return admittedTarget{endpoint: "http://" + net.JoinHostPort(pod.Status.PodIP, "20443"), actorUID: string(actor.UID), podUID: string(pod.UID), containerID: containerID}, nil
}
