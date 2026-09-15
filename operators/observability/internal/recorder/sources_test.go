package recorder

import (
	"context"
	"fmt"
	"slices"
	"testing"

	observation "github.com/cylewitruk-stacks/stacks-k8s/operators/observability/api/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/observability/internal/telemetry"
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

func TestSourcesContainQualifiedNativeFaultKinds(t *testing.T) {
	want := map[string]bool{}
	for _, resource := range telemetry.ChaosResources() {
		want[resource] = true
	}
	for _, source := range sources {
		if source.Group == telemetry.ChaosAPIGroup && source.Version == telemetry.ChaosAPIVersion {
			delete(want, source.Resource)
		}
	}
	if len(want) != 0 {
		t.Fatalf("native fault sources omitted: %v", want)
	}
}

func TestOrdinaryWatchReconnectContinuesFromLastResourceVersion(t *testing.T) {
	pods := schema.GroupVersionResource{Version: "v1", Resource: "pods"}
	pod := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1", "kind": "Pod", "metadata": map[string]any{
			"namespace": "lab", "name": "actor", "uid": "pod", "resourceVersion": "10",
			"labels": map[string]any{"network.stacks.org/network-uid": "root"},
		},
	}}
	client := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(
		runtime.NewScheme(), map[schema.GroupVersionResource]string{pods: "PodList"},
	)
	client.PrependReactor("list", "pods", func(clienttesting.Action) (bool, runtime.Object, error) {
		list := &unstructured.UnstructuredList{Items: []unstructured.Unstructured{*pod.DeepCopy()}}
		list.SetResourceVersion("10")
		return true, list, nil
	})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var cursors []string
	client.PrependWatchReactor("pods", func(action clienttesting.Action) (bool, watch.Interface, error) {
		cursor := action.(clienttesting.WatchAction).GetWatchRestrictions().ResourceVersion
		cursors = append(cursors, cursor)
		stream := watch.NewRaceFreeFake()
		if len(cursors) == 1 {
			modified := pod.DeepCopy()
			modified.SetResourceVersion("11")
			stream.Modify(modified)
			stream.Stop()
		} else {
			cancel()
			stream.Stop()
		}
		return true, stream, nil
	})
	sink := &failingSink{}
	r := &Recorder{
		Dynamic: client,
		Telemetry: &observation.NetworkTelemetry{
			ObjectMeta: metav1.ObjectMeta{Namespace: "lab"},
			Spec:       observation.NetworkTelemetrySpec{NetworkUID: "root"},
		},
		Sink: sink, states: map[string]observation.SourceStatus{}, pendingGaps: map[string]bool{},
		known: map[types.UID]bool{"root": true},
	}
	r.observe(ctx, pods)
	if !slices.Equal(cursors, []string{"10", "11"}) {
		t.Fatalf("watch did not resume from the delivered cursor: %v", cursors)
	}
	if r.states[sourceID(pods)].Gaps != 1 {
		t.Fatalf("ordinary reconnect created a false capture gap: %+v", r.states[sourceID(pods)])
	}
}

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
	pod.SetResourceVersion("12")
	stream.Add(pod)
	stream.Add(pod)
	stream.Stop()
	rv, err := r.consume(t.Context(), stream, pods.String(), "10")
	if err != nil || rv != "12" {
		t.Fatalf("watch cursor: rv=%q err=%v", rv, err)
	}
	if len(sink.records) != 4 || !r.states[pods.String()].Available {
		t.Fatal("watch records or interruption lost")
	}
}

func TestWatchErrorDistinguishesExpiredHistory(t *testing.T) {
	stream := watch.NewRaceFreeFake()
	stream.Error(&metav1.Status{Reason: metav1.StatusReasonExpired, Code: 410})
	stream.Stop()
	r := &Recorder{states: map[string]observation.SourceStatus{}}
	rv, err := r.consume(t.Context(), stream, "pods.core/v1", "17")
	if rv != "17" || !resourceHistoryUnavailable(err) {
		t.Fatalf("expired history not preserved: rv=%q err=%v", rv, err)
	}
}

func TestSourceErrorsPublishOnlyBoundedReasons(t *testing.T) {
	for _, tc := range []struct {
		err    error
		reason observation.SourceReason
	}{
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
