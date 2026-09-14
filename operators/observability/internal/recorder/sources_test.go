package recorder

import (
	"fmt"
	"testing"

	observation "github.com/cylewitruk-stacks/stacks-k8s/operators/observability/api/v1alpha2"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/watch"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	clienttesting "k8s.io/client-go/testing"
)

func TestOptionalSourceAbsenceDoesNotHideAvailableSourceAndRelistRecovers(t *testing.T) {
	pods := schema.GroupVersionResource{Version: "v1", Resource: "pods"}
	chaos := schema.GroupVersionResource{Group: "chaos-mesh.org", Version: "v1alpha1", Resource: "networkchaos"}
	pod := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1", "kind": "Pod", "metadata": map[string]any{
			"namespace": "lab",
			"name":      "actor",
			"uid":       "pod",
			"labels":    map[string]any{"network.stacks.org/network-uid": "root"},
		},
	}}
	c := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(
		runtime.NewScheme(),
		map[schema.GroupVersionResource]string{pods: "PodList", chaos: "NetworkChaosList"},
		pod,
	)
	installed := false
	c.PrependReactor("list", "networkchaos", func(clienttesting.Action) (bool, runtime.Object, error) {
		if !installed {
			return true, nil, apierrors.NewNotFound(chaos.GroupResource(), "")
		}
		return false, nil, nil
	})
	sink := &failingSink{}
	r := &Recorder{
		Dynamic: c,
		Telemetry: &observation.NetworkTelemetry{
			ObjectMeta: metav1.ObjectMeta{Namespace: "lab"},
			Spec:       observation.NetworkTelemetrySpec{NetworkUID: "root"},
		},
		Sink:        sink,
		states:      map[string]observation.SourceStatus{},
		pendingGaps: map[string]bool{},
		known:       map[types.UID]bool{"root": true},
	}
	_, err := r.snapshot(t.Context(), c.Resource(chaos).Namespace("lab"), chaos.String())
	r.sourceError(chaos.String(), err)
	if !apierrors.IsNotFound(err) || r.states[chaos.String()].Reason != observation.SourceAPINotInstalled {
		t.Fatal("missing optional source not distinguished")
	}
	if _, err := r.snapshot(t.Context(), c.Resource(pods).Namespace("lab"), pods.String()); err != nil {
		t.Fatal(err)
	}
	if !r.states[pods.String()].Available || len(sink.records) != 1 || sink.records[0].ObjectUID != "pod" {
		t.Fatal("independent source did not record")
	}
	installed = true
	r.gap(t.Context(), chaos.String(), "reconnect")
	if _, err := r.snapshot(t.Context(), c.Resource(chaos).Namespace("lab"), chaos.String()); err != nil {
		t.Fatal(err)
	}
	if !r.states[chaos.String()].Available || sink.records[1].EventType != EventGap {
		t.Fatal("optional source did not recover with a boundary")
	}
	stream := watch.NewRaceFreeFake()
	stream.Add(pod)
	stream.Add(pod)
	stream.Stop()
	r.consume(t.Context(), stream, pods.String())
	r.sourceError(pods.String(), nil)
	if len(sink.records) != 4 || r.states[pods.String()].Reason != observation.SourceWatchInterrupted {
		t.Fatal("watch records or interruption lost")
	}
}

func TestSourceErrorsPublishOnlyBoundedReasons(t *testing.T) {
	for _, tc := range []struct {
		err    error
		reason observation.SourceReason
	}{
		{nil, observation.SourceWatchInterrupted},
		{
			apierrors.NewForbidden(schema.GroupResource{Resource: "pods"}, "", fmt.Errorf("PRIVATE")),
			observation.SourceAccessDenied,
		},
		{fmt.Errorf("PRIVATE"), observation.SourceReadUnavailable},
	} {
		r := &Recorder{states: map[string]observation.SourceStatus{}}
		r.sourceError("source", tc.err)
		s := r.states["source"]
		if s.Reason != tc.reason || s.Available || s.ObservedAt.Time.IsZero() {
			t.Fatalf("incorrect bounded status %+v", s)
		}
	}
}

func TestEventAttributionWaitsForObservedSourceIdentity(t *testing.T) {
	sink := &failingSink{}
	r := &Recorder{
		Telemetry: &observation.NetworkTelemetry{
			ObjectMeta: metav1.ObjectMeta{Namespace: "lab"},
			Spec:       observation.NetworkTelemetrySpec{NetworkUID: "root"},
		},
		Sink:        sink,
		known:       map[types.UID]bool{"root": true},
		states:      map[string]observation.SourceStatus{},
		pendingGaps: map[string]bool{},
	}
	event := &unstructured.Unstructured{
		Object: map[string]any{
			"kind":           "Event",
			"metadata":       map[string]any{"namespace": "lab", "uid": "event"},
			"involvedObject": map[string]any{"uid": "pod"},
		},
	}
	r.object(t.Context(), event, "events.core/v1", "ADDED")
	if len(sink.records) != 0 {
		t.Fatal("unobserved UID attributed")
	}
	pod := &unstructured.Unstructured{
		Object: map[string]any{
			"kind": "Pod",
			"metadata": map[string]any{
				"namespace": "lab",
				"uid":       "pod",
				"labels":    map[string]any{"network.stacks.org/network-uid": "root"},
			},
		},
	}
	r.object(t.Context(), pod, "pods.core/v1", "ADDED")
	r.object(t.Context(), event, "events.core/v1", "Snapshot")
	if len(sink.records) != 2 || sink.records[1].ObjectUID != "event" {
		t.Fatal("later event snapshot did not recover attribution")
	}
}
