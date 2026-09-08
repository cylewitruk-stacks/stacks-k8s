package leaf

import (
	"context"
	"errors"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	operatorlabels "github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/labels"
)

func TestPodRequestSelectsOnlyExpectedActorKind(t *testing.T) {
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod", Namespace: "test", Labels: map[string]string{
		operatorlabels.ActorResourceKey: "network-node", operatorlabels.ActorKindKey: string(operatorlabels.StacksNode),
	}}}
	if requests := PodRequest(operatorlabels.Bitcoin)(context.Background(), pod); len(requests) != 0 {
		t.Fatalf("wrong-kind requests = %#v", requests)
	}
	requests := PodRequest(operatorlabels.StacksNode)(context.Background(), pod)
	if len(requests) != 1 || requests[0].Name != "network-node" || requests[0].Namespace != "test" {
		t.Fatalf("requests = %#v", requests)
	}
}

func TestIsTransientErrorCoversRoutineRetryClass(t *testing.T) {
	tests := map[string]error{
		"conflict":            apierrors.NewConflict(schema.GroupResource{Resource: "pods"}, "pod", errors.New("conflict")),
		"already exists":      apierrors.NewAlreadyExists(schema.GroupResource{Resource: "pods"}, "pod"),
		"too many requests":   apierrors.NewTooManyRequests("busy", 1),
		"timeout":             apierrors.NewTimeoutError("timeout", 1),
		"server timeout":      apierrors.NewServerTimeout(schema.GroupResource{Resource: "pods"}, "list", 1),
		"service unavailable": apierrors.NewServiceUnavailable("unavailable"),
		"internal error":      apierrors.NewInternalError(errors.New("internal")),
		"context deadline":    context.DeadlineExceeded,
		"context canceled":    context.Canceled,
	}
	for name, err := range tests {
		t.Run(name, func(t *testing.T) {
			if !IsTransientError(err) {
				t.Fatalf("%T was not classified as transient", err)
			}
		})
	}
	if IsTransientError(apierrors.NewForbidden(schema.GroupResource{Resource: "pods"}, "pod", errors.New("forbidden"))) {
		t.Fatal("forbidden error was classified as transient")
	}
}
