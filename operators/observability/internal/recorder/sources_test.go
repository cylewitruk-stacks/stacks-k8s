package recorder

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
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
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/apimachinery/pkg/watch"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	clienttesting "k8s.io/client-go/testing"
)

func TestSourcesContainNamespacedNativeChaosKinds(t *testing.T) {
	want := []string{
		"awschaos", "azurechaos", "blockchaos", "dnschaos", "gcpchaos",
		"httpchaos", "iochaos", "jvmchaos", "kernelchaos", "networkchaos",
		"physicalmachinechaos", "podchaos", "podhttpchaos", "podiochaos",
		"podnetworkchaos", "schedules", "statuschecks", "stresschaos",
		"timechaos", "workflownodes", "workflows",
	}
	if !slices.Equal(telemetry.ChaosResources(), want) {
		t.Fatalf("Chaos source contract differs: %v", telemetry.ChaosResources())
	}
	found := map[string]bool{}
	for _, source := range sources {
		if source.Group == telemetry.ChaosAPIGroup && source.Version == telemetry.ChaosAPIVersion {
			if found[source.Resource] {
				t.Fatalf("duplicate Chaos source: %s", source.Resource)
			}
			found[source.Resource] = true
		}
	}
	if len(found) != len(want) {
		t.Fatalf("Chaos source inventory differs: %v", found)
	}
	for _, resource := range want {
		if !found[resource] {
			t.Fatalf("Chaos source omitted: %s", resource)
		}
	}
}

func TestRecordingStatusSchemaFitsSourceInventory(t *testing.T) {
	path := filepath.Join("..", "..", "..", "..", "charts", "stacks-observability-operator", "crds",
		"observation.stacks.org_networktelemetries.yaml")
	data, err := os.ReadFile(path) // #nosec G304 -- Fixed repository CRD path, not user input.
	if err != nil {
		t.Fatal(err)
	}
	crd := &unstructured.Unstructured{}
	if err := utilyaml.NewYAMLOrJSONDecoder(bytes.NewReader(data), 4096).Decode(crd); err != nil {
		t.Fatal(err)
	}
	versions, found, err := unstructured.NestedSlice(crd.Object, "spec", "versions")
	if err != nil || !found || len(versions) != 1 {
		t.Fatalf("recording CRD versions unavailable: %v", err)
	}
	version, ok := versions[0].(map[string]any)
	if !ok {
		t.Fatal("recording CRD version is not an object")
	}
	maxEntries, found, err := unstructured.NestedInt64(version, "schema", "openAPIV3Schema", "properties",
		"status", "properties", "recording", "properties", "sources", "maxItems")
	healthSources := []string{SourceRecorder, SourceContainerResource, sourceCollectors}
	if err != nil || !found || maxEntries < int64(len(sources)+len(healthSources)) {
		t.Fatalf("recording source limit %d cannot hold %d object sources and %d health sources: %v",
			maxEntries, len(sources), len(healthSources), err)
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

func TestAbsentOptionalAPIRetryDoesNotAccumulateGaps(t *testing.T) {
	chaos := schema.GroupVersionResource{Group: "chaos-mesh.org", Version: "v1alpha1", Resource: "networkchaos"}
	c := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(
		runtime.NewScheme(), map[schema.GroupVersionResource]string{chaos: "NetworkChaosList"},
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
		Dynamic: c, Telemetry: &observation.NetworkTelemetry{ObjectMeta: metav1.ObjectMeta{Namespace: "lab"}},
		Sink: sink, states: map[string]observation.SourceStatus{}, pendingGaps: map[string]bool{},
	}
	needsGap := true
	resource := c.Resource(chaos).Namespace("lab")
	for range 3 {
		if _, err := r.snapshotAfterGap(t.Context(), resource, chaos.String(), &needsGap); !apierrors.IsNotFound(err) {
			t.Fatalf("missing API: %v", err)
		}
	}
	if r.states[chaos.String()].Gaps != 1 || len(sink.records) != 1 {
		t.Fatalf("retries inflated absent-API gaps: state=%+v records=%+v", r.states[chaos.String()], sink.records)
	}
	installed = true
	if _, err := r.snapshotAfterGap(t.Context(), resource, chaos.String(), &needsGap); err != nil {
		t.Fatal(err)
	}
	if !r.states[chaos.String()].Available || r.states[chaos.String()].Gaps != 1 {
		t.Fatalf("source recovery did not preserve the one boundary: %+v", r.states[chaos.String()])
	}
	needsGap = true // Expired watch history creates a new, genuine discontinuity.
	if _, err := r.snapshotAfterGap(t.Context(), resource, chaos.String(), &needsGap); err != nil {
		t.Fatal(err)
	}
	if r.states[chaos.String()].Gaps != 2 || len(sink.records) != 2 {
		t.Fatalf("history loss was not recorded: state=%+v records=%+v", r.states[chaos.String()], sink.records)
	}
}

func TestGeneratedChaosOwnershipResolvesBeforeParentWatch(t *testing.T) {
	gv := schema.GroupVersion{Group: telemetry.ChaosAPIGroup, Version: telemetry.ChaosAPIVersion}
	pods := schema.GroupVersionResource{Version: "v1", Resource: "pods"}
	resources := map[schema.GroupVersionResource]string{
		pods:                               "PodList",
		gv.WithResource("schedules"):       "ScheduleList",
		gv.WithResource("networkchaos"):    "NetworkChaosList",
		gv.WithResource("podnetworkchaos"): "PodNetworkChaosList",
		gv.WithResource("workflows"):       "WorkflowList",
		gv.WithResource("workflownodes"):   "WorkflowNodeList",
	}
	object := func(kind, name, uid, network string, owner *metav1.OwnerReference) *unstructured.Unstructured {
		o := &unstructured.Unstructured{Object: map[string]any{
			"apiVersion": gv.String(), "kind": kind,
			"metadata": map[string]any{"name": name, "namespace": "lab", "uid": uid},
		}}
		if network != "" {
			o.SetLabels(map[string]string{"network.stacks.org/network-uid": network})
		}
		if owner != nil {
			o.SetOwnerReferences([]metav1.OwnerReference{*owner})
		}
		return o
	}
	ref := func(o *unstructured.Unstructured) *metav1.OwnerReference {
		return &metav1.OwnerReference{APIVersion: gv.String(), Kind: o.GetKind(), Name: o.GetName(), UID: o.GetUID()}
	}
	schedule := object("Schedule", "schedule", "schedule-uid", "root", nil)
	child := object("NetworkChaos", "child", "child-uid", "", ref(schedule))
	pod := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1", "kind": "Pod", "metadata": map[string]any{
			"name": "actor", "namespace": "lab", "uid": "pod-uid",
			"labels": map[string]any{"network.stacks.org/network-uid": "root"},
		},
	}}
	perPod := object("PodHttpChaos", "per-pod", "per-pod-uid", "", &metav1.OwnerReference{
		APIVersion: "v1", Kind: "Pod", Name: pod.GetName(), UID: pod.GetUID(),
	})
	workflow := object("Workflow", "workflow", "workflow-uid", "root", nil)
	workflowNode := object("WorkflowNode", "node", "node-uid", "", ref(workflow))
	workflowChild := object("NetworkChaos", "workflow-child", "workflow-child-uid", "", ref(workflowNode))
	foreign := object("Schedule", "foreign", "foreign-uid", "other-root", nil)
	foreignPod := pod.DeepCopy()
	foreignPod.SetName("foreign-actor")
	foreignPod.SetUID("foreign-pod-uid")
	foreignPod.SetLabels(map[string]string{"network.stacks.org/network-uid": "other-root"})
	c := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), resources)
	for _, fixture := range []struct {
		resource schema.GroupVersionResource
		object   *unstructured.Unstructured
	}{
		{gv.WithResource("schedules"), schedule},
		{gv.WithResource("networkchaos"), child},
		{gv.WithResource("workflows"), workflow},
		{gv.WithResource("workflownodes"), workflowNode},
		{gv.WithResource("schedules"), foreign},
		{pods, pod},
		{pods, foreignPod},
	} {
		if err := c.Tracker().Create(fixture.resource, fixture.object, "lab"); err != nil {
			t.Fatal(err)
		}
	}
	sink := &failingSink{}
	r := &Recorder{
		Dynamic: c,
		Telemetry: &observation.NetworkTelemetry{
			ObjectMeta: metav1.ObjectMeta{Namespace: "lab"},
			Spec:       observation.NetworkTelemetrySpec{NetworkUID: "root"},
		},
		Sink: sink, known: map[types.UID]bool{"root": true},
		states: map[string]observation.SourceStatus{}, pendingGaps: map[string]bool{},
	}
	for _, current := range []*unstructured.Unstructured{perPod, child, workflowChild} {
		r.object(t.Context(), current, "chaos-mesh.org/v1alpha1", EventSnapshot)
	}
	if len(sink.records) != 3 || sink.records[0].ObjectUID != "per-pod-uid" ||
		sink.records[1].ObjectUID != "child-uid" || sink.records[2].ObjectUID != "workflow-child-uid" {
		t.Fatalf("generated children were lost before parent snapshots: records=%+v, actions=%+v",
			sink.records, c.Actions())
	}
	if err := c.Tracker().Delete(pods, "lab", pod.GetName()); err != nil {
		t.Fatal(err)
	}
	r.object(t.Context(), perPod, "podhttpchaos.chaos-mesh.org/v1alpha1", "DELETED")
	if len(sink.records) != 4 || sink.records[3].EventType != "DELETED" {
		t.Fatal("verified child lost attribution after its parent disappeared")
	}
	for _, current := range []*unstructured.Unstructured{
		object("NetworkChaos", "foreign-child", "foreign-child-uid", "", ref(foreign)),
		object("PodHttpChaos", "foreign-pod-child", "foreign-pod-child-uid", "", &metav1.OwnerReference{
			APIVersion: "v1", Kind: "Pod", Name: foreignPod.GetName(), UID: foreignPod.GetUID(),
		}),
		object("PodHttpChaos", "replaced-pod-child", "replaced-pod-child-uid", "", &metav1.OwnerReference{
			APIVersion: "v1", Kind: "Pod", Name: foreignPod.GetName(), UID: "replaced-pod-uid",
		}),
		object("NetworkChaos", "replaced", "replaced-uid", "", &metav1.OwnerReference{
			APIVersion: gv.String(), Kind: "Schedule", Name: schedule.GetName(), UID: "replaced-schedule-uid",
		}),
		object("NetworkChaos", "wrong-kind", "wrong-kind-uid", "", &metav1.OwnerReference{
			APIVersion: "v1", Kind: "Pod", Name: schedule.GetName(), UID: schedule.GetUID(),
		}),
	} {
		if r.belongs(t.Context(), current) {
			t.Fatalf("foreign or unverified ownership accepted: %s", current.GetName())
		}
	}
}

func TestPerPodChaosKindBoundary(t *testing.T) {
	for _, kind := range []string{"PodHttpChaos", "PodIOChaos", "PodNetworkChaos"} {
		if !isPerPodChaos(kind) {
			t.Fatalf("upstream per-Pod kind omitted: %s", kind)
		}
	}
	for _, kind := range []string{"PodChaos", "NetworkChaos", "HTTPChaos", "WorkflowNode"} {
		if isPerPodChaos(kind) {
			t.Fatalf("non-per-Pod kind accepted: %s", kind)
		}
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
