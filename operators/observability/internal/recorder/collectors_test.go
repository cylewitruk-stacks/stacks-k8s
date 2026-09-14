package recorder

import (
	"io"
	"net/http"
	"strings"
	"testing"

	observation "github.com/cylewitruk-stacks/stacks-k8s/operators/observability/api/v1alpha2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// metricsTransport provides bounded endpoint responses without a real Pod.
type metricsTransport struct {
	body string
	code int
}

func (m metricsTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: m.code, Body: io.NopCloser(strings.NewReader(m.body)), Request: r}, nil
}

func TestCollectorHealthRequiresCompleteRecognizableMetrics(t *testing.T) {
	prefix := "# TYPE otelcol_process_uptime_total counter\notelcol_process_uptime_total 10\n"
	for _, tc := range []struct {
		name, body string
		code       int
		valid      bool
		sum        float64
	}{
		{"healthy", prefix, 200, true, 0},
		{"failure counters", prefix + "# TYPE otelcol_exporter_send_failed_log_records_total counter\n" +
			"otelcol_exporter_send_failed_log_records_total 7\n", 200, true, 7},
		{"unavailable", prefix, 503, false, 0},
		{"missing", "other_metric 3\n", 200, false, 0},
		{"malformed", "not metric syntax?!", 200, false, 0},
		{"oversized", prefix + strings.Repeat("# padding\n", 500000), 200, false, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			value, err := collectorFailures(
				t.Context(),
				&http.Client{Transport: metricsTransport{tc.body, tc.code}},
				"127.0.0.1",
			)
			if (err == nil) != tc.valid || value != tc.sum {
				t.Fatalf("value=%v error=%v", value, err)
			}
		})
	}
}

func TestCollectorCoverageAndHeartbeatDoNotClaimContinuity(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	collector := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "collector",
			Namespace: "lab",
			UID:       "collector-uid",
			Labels:    map[string]string{"observation.stacks.org/telemetry-uid": "telemetry", "app": "collector"},
		},
		Spec: corev1.PodSpec{NodeName: "ordinary"},
		Status: corev1.PodStatus{
			Phase:      corev1.PodRunning,
			PodIP:      "127.0.0.1",
			Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}},
			ContainerStatuses: []corev1.ContainerStatus{
				{ContainerID: "session", State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}},
			},
		},
	}
	actor := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "actor",
			Namespace: "lab",
			Labels:    map[string]string{"network.stacks.org/network-uid": "root"},
		},
		Spec:   corev1.PodSpec{NodeName: "tainted"},
		Status: corev1.PodStatus{Phase: corev1.PodRunning},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(collector, actor).Build()
	sink := &failingSink{}
	r := &Recorder{
		Client: c,
		Telemetry: &observation.NetworkTelemetry{
			ObjectMeta: metav1.ObjectMeta{Namespace: "lab", UID: "telemetry"},
			Spec:       observation.NetworkTelemetrySpec{NetworkUID: "root", Sources: observation.Sources{Logs: true}},
		},
		Sink:              sink,
		states:            map[string]observation.SourceStatus{},
		pendingGaps:       map[string]bool{},
		collectorSessions: map[string]float64{},
		CollectorHTTPClient: &http.Client{
			Transport: metricsTransport{
				body: "# TYPE otelcol_process_uptime_total counter\notelcol_process_uptime_total 10\n",
				code: 200,
			},
		},
	}
	r.collectorHealth(t.Context())
	if r.states["collectors"].Available {
		t.Fatal("uncovered actor node reported healthy")
	}
	if len(sink.records) < 2 || sink.records[0].EventType != "CaptureGap" {
		t.Fatal("missing coverage gap")
	}
	for _, record := range sink.records {
		if record.EventType == EventHeartbeat && strings.Contains(record.Body, "coverage") {
			t.Fatal("heartbeat asserts continuity")
		}
	}
	actor.Spec.NodeName = "ordinary"
	if err := c.Update(t.Context(), actor); err != nil {
		t.Fatal(err)
	}
	r.collectorHealth(t.Context())
	if !r.states["collectors"].Available {
		t.Fatal("coverage did not recover")
	}
	if err := c.Delete(t.Context(), collector); err != nil {
		t.Fatal(err)
	}
	r.collectorHealth(t.Context())
	if r.states["collectors"].Available {
		t.Fatal("missing collector reported healthy")
	}
}
