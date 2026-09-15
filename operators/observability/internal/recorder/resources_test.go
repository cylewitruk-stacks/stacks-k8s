package recorder

import (
	"testing"

	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	observation "github.com/cylewitruk-stacks/stacks-k8s/operators/observability/api/v1alpha2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	metadatafake "k8s.io/client-go/metadata/fake"
	clienttesting "k8s.io/client-go/testing"
)

func TestContainerResourcesAreIdentityBoundAndDeduplicated(t *testing.T) {
	pod := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1", "kind": "Pod",
		"metadata": map[string]any{
			"name": "actor", "namespace": "lab", "uid": "pod-uid",
			"labels": map[string]any{
				api.LabelNetworkUID: "root", api.LabelParticipantUID: "participant", api.LabelRole: api.RoleActor,
			},
		},
	}}
	support := pod.DeepCopy()
	support.SetName("worker")
	support.SetUID("worker-uid")
	support.SetLabels(map[string]string{
		api.LabelNetworkUID: "root", api.LabelParticipantUID: "participant", api.LabelRole: api.RoleSupport,
	})
	metrics := func(name, cpu, memory string) *unstructured.Unstructured {
		return &unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "metrics.k8s.io/v1beta1", "kind": "PodMetrics",
			"metadata":  map[string]any{"name": name, "namespace": "lab"},
			"timestamp": "2026-09-15T00:00:00Z", "window": "15s",
			"containers": []any{map[string]any{
				"name": "node", "usage": map[string]any{"cpu": cpu, "memory": memory},
			}},
		}}
	}
	listKinds := map[schema.GroupVersionResource]string{
		podResource: "PodList", podMetricsResource: "PodMetricsList",
	}
	actorMetrics, workerMetrics := metrics("actor", "250m", "128Mi"), metrics("worker", "1", "1Gi")
	dynamic := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), listKinds, pod, support)
	dynamic.PrependReactor("list", "pods", func(action clienttesting.Action) (bool, runtime.Object, error) {
		if action.GetResource().Group != "metrics.k8s.io" {
			return false, nil, nil
		}
		return true, &unstructured.UnstructuredList{Items: []unstructured.Unstructured{
			*actorMetrics, *workerMetrics,
		}}, nil
	})
	scheme := runtime.NewScheme()
	scheme.AddKnownTypeWithName(schema.GroupVersionKind{Version: "v1", Kind: "Pod"}, &metav1.PartialObjectMetadata{})
	inventory := metadatafake.NewSimpleMetadataClient(scheme, metadataPod(pod), metadataPod(support))
	sink := &failingSink{}
	r := &Recorder{
		Dynamic: dynamic, Metadata: inventory,
		Telemetry: &observation.NetworkTelemetry{
			ObjectMeta: metav1.ObjectMeta{Namespace: "lab"},
			Spec:       observation.NetworkTelemetrySpec{NetworkUID: types.UID("root")},
		},
		Sink: sink, resourceSamples: map[string]string{}, states: map[string]observation.SourceStatus{},
		pendingGaps: map[string]bool{},
	}
	listedPods, err := dynamic.Resource(podResource).Namespace("lab").List(t.Context(), metav1.ListOptions{})
	if err != nil || len(listedPods.Items) != 2 {
		t.Fatalf("pod fixture list: count=%d err=%v", len(listedPods.Items), err)
	}
	listedMetrics, err := dynamic.Resource(podMetricsResource).Namespace("lab").List(t.Context(), metav1.ListOptions{})
	if err != nil || len(listedMetrics.Items) != 2 {
		t.Fatalf("metrics fixture list: count=%d err=%v", len(listedMetrics.Items), err)
	}
	badPod := metadataPod(pod)
	badPod.Name = "broken"
	badMetric := metrics("broken", "invalid", "1Mi")
	inventory.PrependReactor("list", "pods", func(action clienttesting.Action) (bool, runtime.Object, error) {
		selector := action.(clienttesting.ListAction).GetListRestrictions().Labels
		if !selector.Matches(labels.Set(pod.GetLabels())) || selector.Matches(labels.Set(support.GetLabels())) ||
			selector.Matches(labels.Set{api.LabelNetworkUID: "other", api.LabelRole: api.RoleActor}) {
			t.Fatal("actor selector missing")
		}
		return true, &metav1.List{Items: []runtime.RawExtension{{Object: metadataPod(pod)}, {Object: badPod}}}, nil
	})
	dynamic.PrependReactor("list", "pods", func(action clienttesting.Action) (bool, runtime.Object, error) {
		if action.GetResource().Group != "metrics.k8s.io" {
			return false, nil, nil
		}
		return true, &unstructured.UnstructuredList{Items: []unstructured.Unstructured{*badMetric, *actorMetrics}}, nil
	})
	if err := r.collectContainerResources(t.Context()); err == nil {
		t.Fatal("malformed peer did not report partial failure")
	}
	if err := r.collectContainerResources(t.Context()); err == nil {
		t.Fatal("malformed peer did not report partial failure")
	}
	wantBody := `{"container":"node","cpuCores":0.25,"memoryBytes":134217728,` +
		`"sampledAt":"2026-09-15T00:00:00Z","window":"15s"}`
	if len(sink.records) != 1 || sink.records[0].ParticipantUID != "participant" ||
		sink.records[0].PodUID != "pod-uid" || sink.records[0].ObjectUID != "pod-uid" ||
		sink.records[0].Source != SourceContainerResource ||
		sink.records[0].EventType != EventContainerResource ||
		sink.records[0].Body != wantBody {
		t.Fatalf("unexpected actor resource records: %+v", sink.records)
	}
}

func TestContainerResourcesRejectMalformedUsage(t *testing.T) {
	pod := &unstructured.Unstructured{Object: map[string]any{
		"metadata": map[string]any{
			"name": "actor", "namespace": "lab", "uid": "pod",
			"labels": map[string]any{
				api.LabelNetworkUID: "root", api.LabelParticipantUID: "participant", api.LabelRole: api.RoleActor,
			},
		},
	}}
	metric := &unstructured.Unstructured{Object: map[string]any{
		"metadata":   map[string]any{"name": "actor", "namespace": "lab"},
		"timestamp":  "2026-09-15T00:00:00Z",
		"containers": []any{map[string]any{"name": "node", "usage": map[string]any{"cpu": "PRIVATE", "memory": "1Mi"}}},
	}}
	r := &Recorder{resourceSamples: map[string]string{}}
	if err := r.recordContainerResources(t.Context(), pod, metric); err == nil {
		t.Fatal("accepted malformed resource usage")
	}
}

func TestContainerResourceSampleRetriesAfterExportFailure(t *testing.T) {
	pod := &unstructured.Unstructured{Object: map[string]any{
		"metadata": map[string]any{
			"name": "actor", "namespace": "lab", "uid": "pod",
			"labels": map[string]any{api.LabelParticipantUID: "participant"},
		},
	}}
	metric := &unstructured.Unstructured{Object: map[string]any{
		"metadata":  map[string]any{"name": "actor", "namespace": "lab"},
		"timestamp": "2026-09-15T00:00:00Z",
		"containers": []any{map[string]any{
			"name": "node", "usage": map[string]any{"cpu": "1m", "memory": "1Mi"},
		}},
	}}
	sink := &failingSink{fail: true}
	r := &Recorder{
		Telemetry: &observation.NetworkTelemetry{Spec: observation.NetworkTelemetrySpec{NetworkUID: "root"}},
		Sink:      sink, resourceSamples: map[string]string{}, states: map[string]observation.SourceStatus{},
		pendingGaps: map[string]bool{},
	}
	if err := r.recordContainerResources(t.Context(), pod, metric); err != nil {
		t.Fatal(err)
	}
	sink.fail = false
	if err := r.recordContainerResources(t.Context(), pod, metric); err != nil {
		t.Fatal(err)
	}
	if len(sink.records) != 2 || sink.records[0].EventType != EventGap ||
		sink.records[1].EventType != EventContainerResource {
		t.Fatalf("unacknowledged sample was not retried with a gap: %+v", sink.records)
	}
}

// metadataPod retains only the metadata returned by the Pod inventory endpoint.
func metadataPod(p metav1.Object) *metav1.PartialObjectMetadata {
	return &metav1.PartialObjectMetadata{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Pod"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      p.GetName(),
			Namespace: p.GetNamespace(),
			UID:       p.GetUID(),
			Labels:    p.GetLabels(),
		},
	}
}
