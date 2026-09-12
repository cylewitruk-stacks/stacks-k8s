// Package topology verifies participant runtime identities through direct API reads.
package topology

import (
	"context"
	"errors"
	"fmt"
	"strings"

	networkapi "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	observationv1alpha1 "github.com/cylewitruk-stacks/stacks-k8s/operators/observability/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// AddNetworkTypes registers only the supported public participant API.
func AddNetworkTypes(scheme *runtime.Scheme) { _ = networkapi.AddToScheme(scheme) }

// NetworkObject returns the supported typed root watch target.
func NetworkObject() client.Object { return &networkapi.StacksNetwork{} }

// NotReadyError means the topology has not published a complete inventory yet.
type NotReadyError struct{ Reason string }

func (e *NotReadyError) Error() string { return e.Reason }

// InconclusiveError means identity could not be established safely.
type InconclusiveError struct{ Reason string }

func (e *InconclusiveError) Error() string { return e.Reason }

// IsNotReady reports whether an observation should remain pending.
func IsNotReady(err error) bool {
	var target *NotReadyError
	return errors.As(err, &target)
}

// IsInconclusive reports whether an observation should terminate without a claim.
func IsInconclusive(err error) bool {
	var target *InconclusiveError
	return errors.As(err, &target)
}

// Snapshot is a verified topology identity observation.
type Snapshot struct {
	Binding observationv1alpha1.NetworkBinding
	Actors  []observationv1alpha1.ObservedActorIdentity
}

// Reader verifies topology status against live Kubernetes objects.
type Reader struct{ APIReader client.Reader }

// Observe reads one current participant snapshot without version fallback or informer caching.
func (r Reader) Observe(ctx context.Context, namespace, name, expectedDigest string) (Snapshot, error) {
	if r.APIReader == nil {
		return Snapshot{}, fmt.Errorf("topology reader requires an uncached API reader")
	}
	return r.observeParticipants(ctx, namespace, name, expectedDigest)
}
func containerImage(containers []corev1.Container, name string) string {
	for _, container := range containers {
		if container.Name == name {
			return container.Image
		}
	}
	return ""
}

func controlledBy(object client.Object, kind string, uid types.UID) bool {
	owner := controllerOwner(object)
	return owner != nil && owner.Kind == kind && owner.UID == uid
}

func controllerOwner(object client.Object) *metav1.OwnerReference {
	return metav1.GetControllerOf(object)
}

func podReady(pod *corev1.Pod) bool {
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady {
			return condition.Status == corev1.ConditionTrue
		}
	}
	return false
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
