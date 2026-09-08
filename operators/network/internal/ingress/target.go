// Package ingress admits a declared Stacks RPC target independently of aggregate health.
package ingress

import (
	"context"
	"fmt"
	networkv1alpha1 "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha1"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/workload"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"net"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"strings"
)

// Target binds a direct Pod endpoint to a currently admitted actor runtime.
type Target struct {
	// Endpoint is the admitted Pod RPC endpoint, without mutable Service resolution.
	Endpoint string
	// ActorUID is the StacksNode incarnation.
	ActorUID string
	// PodUID is the admitted Pod incarnation.
	PodUID string
	// ContainerID identifies the admitted running container.
	ContainerID string
}

// Admit checks uncached compiled intent, actor, StatefulSet and Pod identity.
func Admit(ctx context.Context, reader client.Reader, parent *networkv1alpha1.StacksNetwork, actorName, configDigest string) (Target, error) {
	var zero Target
	catalog := parent.Status.TargetDeclarations
	if catalog == nil || catalog.SchemaVersion != networkv1alpha1.TargetDeclarationsVersion || catalog.NetworkUID != string(parent.UID) || catalog.ObservedGeneration != parent.Generation {
		return zero, fmt.Errorf("current declaration catalog is unavailable")
	}
	var entry *networkv1alpha1.TargetDeclaration
	for i := range catalog.Actors {
		candidate := &catalog.Actors[i]
		if candidate.Kind == "StacksNode" && candidate.ActorName == actorName {
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
	if err := reader.Get(ctx, client.ObjectKey{Namespace: parent.Namespace, Name: entry.Name}, actor); err != nil {
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
	if ref == nil || ref.ExpectedDigest != configDigest || identity.ConfigDigest != configDigest || actor.Spec.Container != nil {
		return zero, fmt.Errorf("ingress does not use the approved credential/configuration profile")
	}
	statefulSet := &appsv1.StatefulSet{}
	if err := reader.Get(ctx, client.ObjectKey{Namespace: parent.Namespace, Name: identity.StatefulSetName}, statefulSet); err != nil {
		return zero, fmt.Errorf("target StatefulSet cannot be read")
	}
	if !metav1.IsControlledBy(statefulSet, actor) || !statefulSet.DeletionTimestamp.IsZero() || string(statefulSet.UID) != identity.StatefulSetUID || statefulSet.Status.ObservedGeneration != statefulSet.Generation || statefulSet.Status.CurrentRevision != identity.ControllerRevision || statefulSet.Status.UpdateRevision != identity.ControllerRevision || statefulSet.Status.ReadyReplicas != 1 || statefulSet.Spec.Replicas == nil || *statefulSet.Spec.Replicas != 1 || statefulSet.Spec.Template.Annotations["network.stacks.org/config-digest"] != configDigest {
		return zero, fmt.Errorf("target StatefulSet identity is not current")
	}
	pod := &corev1.Pod{}
	if err := reader.Get(ctx, client.ObjectKey{Namespace: parent.Namespace, Name: identity.PodName}, pod); err != nil {
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
	return Target{Endpoint: "http://" + net.JoinHostPort(pod.Status.PodIP, "20443"), ActorUID: string(actor.UID), PodUID: string(pod.UID), ContainerID: containerID}, nil
}
