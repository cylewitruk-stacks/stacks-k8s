package recorder

import (
	"context"
	"errors"
	"testing"
	"time"

	observation "github.com/cylewitruk-stacks/stacks-k8s/operators/observability/api/v1alpha2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

func TestPublicationPinsDeclarationAndOwnsOnlyRecording(t *testing.T) {
	for _, mode := range []string{"current", "replaced", "generation", "deleted"} {
		t.Run(mode, func(t *testing.T) {
			scheme := runtime.NewScheme()
			if err := observation.AddToScheme(scheme); err != nil {
				t.Fatal(err)
			}
			initial := &observation.NetworkTelemetry{
				ObjectMeta: metav1.ObjectMeta{Name: "capture", Namespace: "lab", UID: "initial", Generation: 1},
				Status:     observation.NetworkTelemetryStatus{Admitted: true},
			}
			current := initial.DeepCopy()
			switch mode {
			case "replaced":
				current.UID = "replacement"
			case "generation":
				current.Generation = 2
			case "deleted":
				now := metav1.Now()
				current.DeletionTimestamp = &now
				current.Finalizers = []string{"test"}
			}
			writes := 0
			c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(current).WithInterceptorFuncs(interceptor.Funcs{
				SubResourcePatch: func(
					_ context.Context, _ client.Client, name string, object client.Object,
					_ client.Patch, _ ...client.SubResourcePatchOption,
				) error {
					writes++
					value := object.(*unstructured.Unstructured)
					status := value.Object["status"].(map[string]any)
					if name != "status" || len(status) != 1 || status["recording"] == nil {
						t.Fatal("publication crossed status ownership boundary")
					}
					return nil
				},
			}).Build()
			r := &Recorder{
				Client:    c,
				Telemetry: initial,
				PodUID:    "worker",
				states:    map[string]observation.SourceStatus{},
			}
			err := r.publish(t.Context())
			if mode == "current" {
				if err != nil || writes != 1 {
					t.Fatalf("current publication: %v writes=%d", err, writes)
				}
			} else if err == nil || writes != 0 {
				t.Fatal("stale declaration published")
			}
		})
	}
}

func TestPublicationReadHasDeadline(t *testing.T) {
	c := fake.NewClientBuilder().WithInterceptorFuncs(interceptor.Funcs{
		Get: func(ctx context.Context, _ client.WithWatch, _ client.ObjectKey, _ client.Object, _ ...client.GetOption) error {
			deadline, ok := ctx.Deadline()
			if !ok || time.Until(deadline) > 5*time.Second {
				t.Fatal("unbounded publication read")
			}
			<-ctx.Done()
			return ctx.Err()
		},
	}).Build()
	r := &Recorder{Client: c, Telemetry: &observation.NetworkTelemetry{}}
	if err := r.publish(t.Context()); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("read did not time out: %v", err)
	}
}
