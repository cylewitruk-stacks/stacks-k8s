// Package recorder continuously retains bounded public observations without mutating network resources.
package recorder

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/cylewitruk-stacks/stacks-k8s/operators/observability/internal/telemetry"
	collectorlogs "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	logspb "go.opentelemetry.io/proto/otlp/logs/v1"
	"google.golang.org/protobuf/proto"
)

// Record separates collector-established identity from the redacted source payload.
type Record struct {
	// Time is the collector observation time, not an inferred source event time.
	Time time.Time
	// NetworkUID binds the experiment incarnation.
	NetworkUID string
	// ParticipantUID identifies a generated participant when known.
	ParticipantUID string
	// PodUID identifies a runtime Pod when known.
	PodUID string
	// ObjectUID identifies the source object.
	ObjectUID string
	// EventType distinguishes watch events, snapshots and capture gaps.
	EventType string
	// Source identifies the Kubernetes GVR or recorder source.
	Source string
	// Body is a bounded redacted JSON or diagnostic payload.
	Body string
}

// Sink retains observations. Errors never mean that an attempted record was not stored.
type Sink interface {
	Write(context.Context, Record) error
}

// HTTPExporter writes OTLP protobuf to the pinned GreptimeDB HTTP endpoint.
type HTTPExporter struct {
	// Client bounds each request and refuses credential-bearing redirects.
	Client *http.Client
	// Endpoint is the validated base HTTP URL.
	Endpoint string
	// Authorization is the administrator-provisioned ingestion credential.
	Authorization string
	// Table selects the recording's append-only table.
	Table string
	// Retention sets creation-time TTL for this independently named table.
	Retention string
}

// NewExporter validates the configured backend without exposing credentials in errors.
func NewExporter(endpoint, authorization, table, retention string) (*HTTPExporter, error) {
	u, err := url.Parse(endpoint)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" ||
		u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return nil, fmt.Errorf("invalid backend HTTP base endpoint")
	}
	if !strings.HasPrefix(authorization, "Basic ") || strings.ContainsAny(authorization, "\r\n") {
		return nil, fmt.Errorf("invalid backend authorization header")
	}
	if retention != "1h" && retention != "6h" && retention != "24h" {
		return nil, fmt.Errorf("unsupported retention window")
	}
	return &HTTPExporter{
		Client: &http.Client{
			Timeout:       5 * time.Second,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
		},
		Endpoint: strings.TrimSuffix(endpoint, "/"), Authorization: authorization, Table: table, Retention: retention,
	}, nil
}

// Write accepts only an acknowledged complete OTLP export; partial success is a capture failure.
func (e *HTTPExporter) Write(ctx context.Context, r Record) error {
	attrs := []*commonpb.KeyValue{}
	for _, pair := range [][2]string{
		{"network_uid", r.NetworkUID},
		{"participant_uid", r.ParticipantUID},
		{"pod_uid", r.PodUID},
		{"object_uid", r.ObjectUID},
		{"event_type", r.EventType},
		{"source", r.Source},
	} {
		attrs = append(
			attrs,
			&commonpb.KeyValue{
				Key:   pair[0],
				Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: pair[1]}},
			},
		)
	}
	if r.Time.Before(time.Unix(0, 0)) {
		return fmt.Errorf("invalid observation timestamp")
	}
	payload := &collectorlogs.ExportLogsServiceRequest{
		ResourceLogs: []*logspb.ResourceLogs{{ScopeLogs: []*logspb.ScopeLogs{{
			Scope: &commonpb.InstrumentationScope{Name: "stacks-k8s-recorder-v1"}, LogRecords: []*logspb.LogRecord{
				{
					TimeUnixNano: uint64(
						r.Time.UnixNano(),
					),
					ObservedTimeUnixNano: uint64(r.Time.UnixNano()),
					Attributes:           attrs,
					Body: &commonpb.AnyValue{
						Value: &commonpb.AnyValue_StringValue{StringValue: r.Body},
					},
				},
			},
		}}}},
	}
	body, err := proto.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode observation: %w", err)
	}
	// #nosec G704 -- Validated administrator-selected backend URL, never derived from actor telemetry.
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.Endpoint+"/v1/otlp/v1/logs", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("construct observation request")
	}
	req.Header.Set("Content-Type", "application/x-protobuf")
	req.Header.Set("Authorization", e.Authorization)
	req.Header.Set("X-Greptime-DB-Name", "public")
	req.Header.Set("X-Greptime-Log-Table-Name", e.Table)
	req.Header.Set("X-Greptime-Log-Extract-Keys", telemetry.ExtractKeys)
	req.Header.Set("X-Greptime-Hints", "ttl="+e.Retention+",append_mode=true")
	response, err := e.Client.Do(req) // #nosec G704 -- Trusted backend endpoint; redirects disabled.
	if err != nil {
		return fmt.Errorf("backend export unavailable")
	}
	defer func() { _ = response.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(response.Body, 65537))
	if err != nil || len(data) > 65536 {
		return fmt.Errorf("backend response unreadable")
	}
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("backend export HTTP %d", response.StatusCode)
	}
	var result collectorlogs.ExportLogsServiceResponse
	if err := proto.Unmarshal(data, &result); err != nil {
		return fmt.Errorf("backend acknowledgement malformed")
	}
	if result.GetPartialSuccess().GetRejectedLogRecords() != 0 {
		return fmt.Errorf("backend export partial acknowledgement")
	}
	return nil
}
