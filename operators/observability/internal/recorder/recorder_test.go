package recorder

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	collectorlogs "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	"google.golang.org/protobuf/proto"

	observation "github.com/cylewitruk-stacks/stacks-k8s/operators/observability/api/v1alpha2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
)

func TestExporterUsesBoundedIdentityColumnsAndRejectsFalseAcknowledgements(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		code       int
		success    bool
	}{
		{"complete", ``, 200, true},
		{"partial", "\x0a\x02\x08\x01", 200, false},
		{"malformed", `<html>`, 200, false},
		{"redirect", ``, 302, false},
		{"unavailable", `PRIVATE`, 503, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			observed := time.Unix(1700000000, 123456789)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/v1/otlp/v1/logs" ||
					r.Header.Get("X-Greptime-Log-Table-Name") != "stacks_test_objects" ||
					r.Header.Get("X-Greptime-Hints") != "ttl=6h,append_mode=true" ||
					!strings.Contains(r.Header.Get("X-Greptime-Log-Extract-Keys"), "network_uid") {
					t.Errorf("incorrect request %v", r.Header)
				}
				payload, err := io.ReadAll(r.Body)
				var request collectorlogs.ExportLogsServiceRequest
				if err != nil || proto.Unmarshal(payload, &request) != nil {
					t.Fatal("invalid protobuf request")
				}
				record := request.GetResourceLogs()[0].GetScopeLogs()[0].GetLogRecords()[0]
				if record.TimeUnixNano != uint64(observed.UnixNano()) ||
					record.Body.GetStringValue() != `{"public":true}` {
					t.Fatal("wire timestamp or body changed")
				}
				w.WriteHeader(tc.code)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			exporter, err := NewExporter(server.URL, "Basic test", "stacks_test_objects", "6h")
			if err != nil {
				t.Fatal(err)
			}
			err = exporter.Write(t.Context(), Record{Time: observed, NetworkUID: "network", Body: `{"public":true}`})
			if (err == nil) != tc.success {
				t.Fatalf("export result %v", err)
			}
			if err != nil && strings.Contains(err.Error(), "PRIVATE") {
				t.Fatal("raw backend response leaked")
			}
		})
	}
}

func TestRedactionAndExactNetworkSelection(t *testing.T) {
	r := &Recorder{Telemetry: &observation.NetworkTelemetry{
		ObjectMeta: metav1.ObjectMeta{Namespace: "lab"},
		Spec:       observation.NetworkTelemetrySpec{NetworkUID: "network-a"},
	}, known: map[types.UID]bool{"network-a": true}}
	obj := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1", "kind": "Pod",
		"metadata": map[string]any{
			"namespace": "lab",
			"name":      "actor",
			"uid":       "pod-a",
			"labels": map[string]any{
				"network.stacks.org/network-uid":    "network-a",
				"actions.stacks.org/correlation-id": "experiment-hint",
			},
			"annotations": map[string]any{"private": "PRIVATE"},
		},
		"spec": map[string]any{"containers": []any{map[string]any{
			"name": "node", "image": "pinned:image",
			"env": []any{map[string]any{"name": "KEY", "value": "PRIVATE"}},
		}}},
		"status": map[string]any{"message": "password=PRIVATE", "imageID": "sha256:public"},
	}}
	if !r.belongs(t.Context(), obj) {
		t.Fatal("matching Pod rejected")
	}
	body := publicBody(obj)
	if strings.Contains(body, "PRIVATE") || !strings.Contains(body, "pinned:image") ||
		!strings.Contains(body, "sha256:public") || !strings.Contains(body, "experiment-hint") {
		t.Fatalf("unsafe or incomplete public projection: %s", body)
	}
	obj.SetLabels(map[string]string{"network.stacks.org/network-uid": "network-b"})
	if r.belongs(t.Context(), obj) {
		t.Fatal("same-namespace foreign network accepted")
	}
	obj.SetKind("Event")
	obj.Object["involvedObject"] = map[string]any{"uid": "network-a"}
	if !r.belongs(t.Context(), obj) {
		t.Fatal("bound Event rejected")
	}
	obj.Object["involvedObject"] = map[string]any{"uid": "network-b"}
	if r.belongs(t.Context(), obj) {
		t.Fatal("unbound Event accepted")
	}
}

func TestAdditionalChaosKindUsesExactIdentityAndRedactedBody(t *testing.T) {
	r := &Recorder{Telemetry: &observation.NetworkTelemetry{
		ObjectMeta: metav1.ObjectMeta{Namespace: "lab"},
		Spec:       observation.NetworkTelemetrySpec{NetworkUID: "network-a"},
	}}
	object := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "chaos-mesh.org/v1alpha1", "kind": "HTTPChaos",
		"metadata": map[string]any{
			"name": "fault", "namespace": "lab", "uid": "fault-uid",
			"labels": map[string]any{
				"network.stacks.org/network-uid":    "network-a",
				"actions.stacks.org/correlation-id": "probe-1",
			},
		},
		"spec": map[string]any{
			"mode": "one", "selector": map[string]any{"labelSelectors": map[string]any{
				"network.stacks.org/role": "actor",
			}},
			"request_headers": map[string]any{
				"Authorization": "PRIVATE", "X-Experiment": "visible", "X-API-Key": "PRIVATE-KEY",
			},
			"response_headers": map[string]any{"Set-Cookie": "PRIVATE-COOKIE"},
			"replace": map[string]any{
				"queries": map[string]any{"token": "PRIVATE-QUERY"},
				"headers": map[string]any{"Set-Cookie": "PRIVATE-COOKIE"},
				"body":    "PRIVATE-BODY",
			},
		},
	}}
	if !r.belongs(t.Context(), object) {
		t.Fatal("matching HTTPChaos omitted")
	}
	body := publicBody(object)
	if strings.Contains(body, "PRIVATE") || strings.Contains(body, "Authorization") ||
		strings.Contains(body, "X-Experiment") || !strings.Contains(body, "probe-1") ||
		!strings.Contains(body, "\"mode\":\"one\"") {
		t.Fatalf("unsafe or incomplete Chaos projection: %s", body)
	}
	object.SetLabels(map[string]string{"network.stacks.org/network-uid": "network-b"})
	if r.belongs(t.Context(), object) {
		t.Fatal("foreign fault attributed to recording")
	}
}

func TestJVMChaosOpaqueRuleDataIsNotExported(t *testing.T) {
	object := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "chaos-mesh.org/v1alpha1", "kind": "JVMChaos",
		"metadata": map[string]any{"name": "fault", "namespace": "lab"},
		"spec":     map[string]any{"action": "ruleData", "ruleData": "PRIVATE-RULE"},
	}}
	if body := publicBody(object); strings.Contains(body, "PRIVATE-RULE") ||
		!strings.Contains(body, `"action":"ruleData"`) {
		t.Fatalf("JVMChaos projection retained opaque rule data: %s", body)
	}
}

// failingSink models a backend outage without exposing mutation or retry authority.
type failingSink struct {
	fail    bool
	records []Record
}

func (s *failingSink) Write(_ context.Context, r Record) error {
	if s.fail {
		return fmt.Errorf("offline")
	}
	s.records = append(s.records, r)
	return nil
}

func TestExportOutagePrecedesRecoveredFactsWithGap(t *testing.T) {
	sink := &failingSink{fail: true}
	r := &Recorder{Sink: sink, states: map[string]observation.SourceStatus{}, pendingGaps: map[string]bool{}}
	record := Record{Time: time.Now(), Source: "pods", EventType: "MODIFIED", Body: "first"}
	r.emit(t.Context(), record)
	sink.fail = false
	record.Body = "later"
	r.emit(t.Context(), record)
	if len(sink.records) != 2 || sink.records[0].EventType != "CaptureGap" || sink.records[1].Body != "later" {
		t.Fatalf("gap not preserved: %+v", sink.records)
	}
	if r.states["pods"].Gaps != 1 || !r.backendReady {
		t.Fatalf("incorrect health: %+v", r.states)
	}
}

func TestNewGapSourcesAlwaysHavePublishableObservationTimes(t *testing.T) {
	sink := &failingSink{}
	r := &Recorder{
		Telemetry: &observation.NetworkTelemetry{}, Sink: sink,
		states: map[string]observation.SourceStatus{}, pendingGaps: map[string]bool{},
	}
	r.gap(t.Context(), "fresh-source", "startup")
	if r.states["fresh-source"].ObservedAt.Time.IsZero() {
		t.Fatal("initial source timestamp would fail API validation")
	}
	sink.fail = true
	r.emit(t.Context(), Record{Time: time.Now(), Source: "new-failed-source"})
	if r.states["new-failed-source"].ObservedAt.Time.IsZero() {
		t.Fatal("export failure source timestamp would fail API validation")
	}
}

func TestCredentialBearingDiagnosticsAreEntirelyRedacted(t *testing.T) {
	for _, text := range []string{
		`Authorization: Basic PRIVATE`, `{"private_key":"PRIVATE","public":"keep"}`,
		"password=PRIVATE trailing details", "seed = PRIVATE",
	} {
		if result := sensitiveAssignment.ReplaceAllString(text, "[REDACTED]"); strings.Contains(result, "PRIVATE") {
			t.Fatalf("credential leaked: %s", result)
		}
	}
}

func TestCredentialVocabularyAndStableSourceEncoding(t *testing.T) {
	for _, term := range []string{
		"password", "privatekey", "private_key", "private-key", "authorization",
		"authtoken", "auth_token", "auth-token", "seed", "AUTH-TOKEN",
	} {
		object := &unstructured.Unstructured{
			Object: map[string]any{
				"kind":     "Test",
				"metadata": map[string]any{},
				"status":   map[string]any{term: "PRIVATE", "message": term + `="PRIVATE"`},
			},
		}
		if body := publicBody(object); strings.Contains(body, "PRIVATE") || !strings.Contains(body, "[REDACTED]") {
			t.Fatalf("redaction missed %s: %s", term, body)
		}
	}
	for _, tc := range []struct {
		gvr  schema.GroupVersionResource
		want string
	}{
		{schema.GroupVersionResource{Version: "v1", Resource: "pods"}, "pods.core/v1"},
		{schema.GroupVersionResource{
			Group: "actions.stacks.org", Version: "v1alpha2", Resource: "bitcoinblockgenerations",
		}, "bitcoinblockgenerations.actions.stacks.org/v1alpha2"},
	} {
		if got := sourceID(tc.gvr); got != tc.want {
			t.Fatalf("source: %s", got)
		}
	}
}

func TestEventAttributionCapacityRecordsGapWithoutDroppingObject(t *testing.T) {
	sink := &failingSink{}
	r := &Recorder{
		Telemetry: &observation.NetworkTelemetry{
			ObjectMeta: metav1.ObjectMeta{Namespace: "lab"},
			Spec:       observation.NetworkTelemetrySpec{NetworkUID: "root"},
		},
		Sink:        sink,
		known:       map[types.UID]bool{},
		states:      map[string]observation.SourceStatus{},
		pendingGaps: map[string]bool{},
	}
	for i := 0; i < 4096; i++ {
		r.known[types.UID(fmt.Sprint(i))] = true
	}
	for _, uid := range []string{"new-a", "new-b"} {
		object := &unstructured.Unstructured{
			Object: map[string]any{
				"kind": "Pod",
				"metadata": map[string]any{
					"namespace": "lab",
					"uid":       uid,
					"labels":    map[string]any{"network.stacks.org/network-uid": "root"},
				},
			},
		}
		r.object(t.Context(), object, "pods.core/v1", "ADDED")
	}
	if len(sink.records) != 3 || sink.records[0].EventType != "CaptureGap" || sink.records[1].ObjectUID != "new-a" ||
		sink.records[2].ObjectUID != "new-b" ||
		len(r.known) != 4096 {
		t.Fatalf("capacity handling: %+v", sink.records)
	}
}
