package recorder

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	observation "github.com/cylewitruk-stacks/stacks-k8s/operators/observability/api/v1alpha2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/metadata"
	"k8s.io/client-go/rest"
)

// TestResourceInventoryUsesMetadataNegotiation exercises the real HTTP client boundary.
func TestResourceInventoryUsesMetadataNegotiation(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/namespaces/lab/pods":
			if !strings.Contains(r.Header.Get("Accept"), "as=PartialObjectMetadataList") {
				t.Error("full Pod requested")
			}
			if r.URL.Query().
				Get("labelSelector") !=
				"network.stacks.org/network-uid=root,network.stacks.org/role=actor" {
				t.Errorf("wrong scope %s", r.URL.RawQuery)
			}
			_, _ = w.Write(
				[]byte(
					`{"apiVersion":"meta.k8s.io/v1","kind":"PartialObjectMetadataList","items":[
{"apiVersion":"meta.k8s.io/v1","kind":"PartialObjectMetadata",
"metadata":{"name":"actor","namespace":"lab","uid":"pod",
"labels":{"network.stacks.org/network-uid":"root",
"network.stacks.org/role":"actor","network.stacks.org/participant-uid":"participant"}}}]}`,
				),
			)
		case "/apis/metrics.k8s.io/v1beta1/namespaces/lab/pods":
			_, _ = w.Write(
				[]byte(
					`{"apiVersion":"metrics.k8s.io/v1beta1","kind":"PodMetricsList","items":[
{
"metadata":{"name":"actor"},
"timestamp":"2026-09-15T00:00:00Z","window":"15s",
"containers":[{"name":"node","usage":{"cpu":"1m","memory":"1Mi"}}]}]}`,
				),
			)
		default:
			t.Errorf("unexpected endpoint %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	config := &rest.Config{Host: server.URL}
	inventory, err := metadata.NewForConfig(config)
	if err != nil {
		t.Fatal(err)
	}
	metrics, err := dynamic.NewForConfig(config)
	if err != nil {
		t.Fatal(err)
	}
	sink := &failingSink{}
	r := &Recorder{
		Metadata: inventory,
		Dynamic:  metrics,
		Telemetry: &observation.NetworkTelemetry{
			ObjectMeta: metav1.ObjectMeta{Namespace: "lab"},
			Spec:       observation.NetworkTelemetrySpec{NetworkUID: "root"},
		},
		Sink:            sink,
		resourceSamples: map[string]string{},
		states:          map[string]observation.SourceStatus{},
		pendingGaps:     map[string]bool{},
	}
	if err := r.collectContainerResources(t.Context()); err != nil {
		t.Fatal(err)
	}
	if requests != 2 || len(sink.records) != 1 || sink.records[0].PodUID != "pod" {
		t.Fatalf("metadata join failed: %d %+v", requests, sink.records)
	}
}
