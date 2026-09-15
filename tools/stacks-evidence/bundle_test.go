package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// bundleFixture returns deterministic equal-timestamp records behind a generated-query transport.
func bundleFixture(t *testing.T) (bundleRequest, *http.Client, *[]string) {
	t.Helper()
	from := time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC)
	request := bundleRequest{
		Endpoint:      "http://backend",
		Authorization: "Basic PRIVATE",
		NetworkUID:    "11111111-1111-1111-1111-111111111111",
		TelemetryUID:  "22222222-2222-2222-2222-222222222222",
		TablePrefix:   "stacks_test",
		From:          from,
		To:            from.Add(30 * time.Second),
		OutputDir:     filepath.Join(t.TempDir(), "bundle"),
		Metrics:       []string{"up"},
		PageSize:      2,
	}
	queries := []string{}
	c := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		data, err := io.ReadAll(req.Body)
		if err != nil {
			t.Fatal(err)
		}
		values, err := url.ParseQuery(string(data))
		if err != nil {
			t.Fatal(err)
		}
		query := values.Get("sql")
		queries = append(queries, query)
		if !strings.Contains(query, "network_uid = '"+request.NetworkUID+"'") ||
			!strings.Contains(query, "2026-09-15T00:00:00Z") ||
			!strings.Contains(query, "2026-09-15T00:00:30Z") {
			t.Error("query lost required bounds", query)
		}
		columns := []string{"timestamp", "network_uid", "body", "source", "event_type", "object_uid"}
		rows := [][]any{}
		if strings.Contains(query, "FROM stacks_test_objects ") {
			for _, kind := range []string{"StacksNetwork", "StacksGenesis", "StacksNetworkParticipant", "Pod"} {
				body := fmt.Sprintf(`{"kind":%q,"metadata":{"uid":%q},"spec":{"public":"value"}}`, kind, kind)
				rows = append(
					rows,
					[]any{from.Format(time.RFC3339Nano), request.NetworkUID, body, "source", "Snapshot", kind},
				)
			}
			rows = append(
				rows,
				[]any{from.Format(time.RFC3339Nano), request.NetworkUID, "gap", "pods", "CaptureGap", ""},
			)
		} else if strings.Contains(query, "FROM up ") {
			columns = []string{"greptime_timestamp", "network_uid", "telemetry_uid", "greptime_value"}
			rows = [][]any{{from.Format(time.RFC3339Nano), request.NetworkUID, request.TelemetryUID, 1}}
			if !strings.Contains(query, "telemetry_uid = '"+request.TelemetryUID+"'") {
				t.Error("metric recording unbound")
			}
		}
		if strings.HasSuffix(query, "LIMIT 0") {
			rows = nil
		} else {
			fields := strings.Fields(query)
			offset, _ := strconv.Atoi(fields[len(fields)-1])
			limit, _ := strconv.Atoi(fields[len(fields)-3])
			if !strings.Contains(query, "ORDER BY \""+columns[0]+"\"") {
				t.Error("missing timestamp order")
			}
			for _, name := range columns {
				if !strings.Contains(query, "\""+name+"\"") {
					t.Error("incomplete total order", name)
				}
			}
			if offset >= len(rows) {
				rows = nil
			} else {
				rows = rows[offset:min(len(rows), offset+limit)]
			}
		}
		schema := []map[string]string{}
		for _, name := range columns {
			schema = append(schema, map[string]string{"name": name, "data_type": "String"})
		}
		payload, _ := json.Marshal(
			map[string]any{
				"code": 0,
				"output": []any{
					map[string]any{
						"records": map[string]any{"schema": map[string]any{"column_schemas": schema}, "rows": rows},
					},
				},
			},
		)
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(payload))}, nil
	})}
	return request, c, &queries
}

func TestBundlePaginationContextAndIntegrity(t *testing.T) {
	request, c, queries := bundleFixture(t)
	var output bytes.Buffer
	if err := bundle(t.Context(), request, c, &output); err != nil {
		t.Fatal(err)
	}
	var manifest evidenceManifest
	if err := json.Unmarshal(output.Bytes(), &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.State != "Exported" || manifest.Coverage != "Unknown" || manifest.Consistency != "LiveReadNoSnapshot" ||
		len(manifest.ContextMissing) != 0 ||
		manifest.CaptureGaps["pods"] != 1 ||
		manifest.Tables[0].Rows != 5 ||
		len(manifest.Tables[0].Files) != 3 {
		t.Fatalf("manifest differs: %s", output.String())
	}
	var encoded map[string]json.RawMessage
	if err := json.Unmarshal(output.Bytes(), &encoded); err != nil {
		t.Fatal(err)
	}
	if string(encoded["contextMissing"]) != "[]" {
		t.Fatal("empty contextMissing must encode as an array")
	}
	if !strings.Contains(strings.Join(*queries, "\n"), "OFFSET 4") {
		t.Fatal("pagination did not reach equal-timestamp tail")
	}
	for _, table := range manifest.Tables {
		if !table.Through.Equal(request.To) {
			t.Fatal("completed window not reflected in through")
		}
	}
	// Additional experimenter-owned files are outside the manifest's integrity contract.
	if err := os.WriteFile(
		filepath.Join(request.OutputDir, "receipts.jsonl"),
		[]byte("external receipt\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	if err := verifyBundle(request.OutputDir); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"manifest.json", "context.json"} {
		// #nosec G304 -- Test-owned private temporary fixture path.
		data, err := os.ReadFile(filepath.Join(request.OutputDir, name))
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(data, []byte("PRIVATE")) {
			t.Fatal("credential leaked")
		}
	}
	path := filepath.Join(request.OutputDir, manifest.Tables[0].Files[0].Name)
	if err := os.WriteFile(path, []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	if verifyBundle(request.OutputDir) == nil {
		t.Fatal("tampering undetected")
	}
	before := len(*queries)
	if bundle(t.Context(), request, c, io.Discard) == nil || len(*queries) != before {
		t.Fatal("existing evidence overwritten or queried")
	}
}

func TestBundleBudgetsAndUnavailableSourcesAreExplicit(t *testing.T) {
	for _, mode := range []string{"rows", "bytes", "queries", "backend", "malformed", "schema", "foreign"} {
		t.Run(mode, func(t *testing.T) {
			request, c, _ := bundleFixture(t)
			switch mode {
			case "rows":
				request.MaxRows = 1
			case "bytes":
				request.MaxBytes = 1
			case "queries":
				request.MaxQueries = 1
			default:
				original := c.Transport
				c.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
					response, err := original.RoundTrip(r)
					if err != nil {
						return nil, err
					}
					data, _ := io.ReadAll(response.Body)
					_ = response.Body.Close()
					switch mode {
					case "backend":
						data = []byte(`{"code":1000,"error":"PRIVATE"}`)
					case "malformed":
						data = []byte(`{"code":1000,"error":"PRIVATE",`)
					case "schema":
						data = bytes.ReplaceAll(data, []byte(`"network_uid"`), []byte(`"foreign"`))
					case "foreign":
						data = bytes.ReplaceAll(data, []byte(request.NetworkUID), []byte(request.TelemetryUID))
					}
					response.Body = io.NopCloser(bytes.NewReader(data))
					return response, nil
				})
			}
			var output bytes.Buffer
			if bundle(t.Context(), request, c, &output) == nil {
				t.Fatal("partial export reported success")
			}
			var manifest evidenceManifest
			_ = json.Unmarshal(output.Bytes(), &manifest)
			if manifest.State == "Exported" || len(manifest.Tables) != 3 || len(manifest.ContextMissing) == 0 ||
				bytes.Contains(output.Bytes(), []byte("PRIVATE")) {
				t.Fatalf("partial evidence hidden: %s", output.String())
			}
			if mode == "rows" &&
				(!manifest.Tables[0].Through.Equal(request.From) || len(manifest.Tables[0].Files) != 1) {
				t.Fatal("partial window advanced through or lost saved page")
			}
			if mode == "backend" || mode == "malformed" {
				expected := "BackendRejected"
				if mode == "malformed" {
					expected = "InvalidRecords"
				}
				for _, table := range manifest.Tables {
					if table.State != "Unavailable" || string(table.Reason) != expected {
						t.Fatalf("backend failure misclassified: %+v", table)
					}
				}
			}
			if err := verifyBundle(request.OutputDir); err != nil {
				t.Fatal("partial retained files unverifiable", err)
			}
		})
	}
}

func TestBundleCancellationAndInvalidBounds(t *testing.T) {
	request, c, queries := bundleFixture(t)
	request.To = request.From.Add(25 * time.Hour)
	if bundle(t.Context(), request, c, io.Discard) == nil || len(*queries) != 0 {
		t.Fatal("invalid window queried")
	}
	if _, err := os.Stat(request.OutputDir); !os.IsNotExist(err) {
		t.Fatal("invalid request claimed directory")
	}
	request.To = request.From.Add(30 * time.Second)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if bundle(ctx, request, c, io.Discard) == nil || len(*queries) != 0 {
		t.Fatal("cancelled export queried")
	}
	if err := verifyBundle(request.OutputDir); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyRejectsManifestTraversal(t *testing.T) {
	request, c, _ := bundleFixture(t)
	if err := bundle(t.Context(), request, c, io.Discard); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(request.OutputDir, "manifest.json")
	// #nosec G304 -- Test-owned private temporary fixture path.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	data = bytes.ReplaceAll(data, []byte(`"context.json"`), []byte(`"../context.json"`))
	// #nosec G703 -- Test-owned private temporary fixture path.
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if verifyBundle(request.OutputDir) == nil {
		t.Fatal("manifest traversal accepted")
	}
}

// TestUnretainedPagesCannotChangeSummaries covers rejection after a valid row and disk failure.
func TestUnretainedPagesCannotChangeSummaries(t *testing.T) {
	for _, mode := range []string{"foreign-tail", "write"} {
		t.Run(mode, func(t *testing.T) {
			request, c, _ := bundleFixture(t)
			original := c.Transport
			c.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
				response, err := original.RoundTrip(r)
				if err != nil {
					return nil, err
				}
				data, _ := io.ReadAll(response.Body)
				_ = response.Body.Close()
				if bytes.Contains(data, []byte(`Snapshot`)) {
					if mode == "foreign-tail" {
						pos := bytes.LastIndex(data, []byte(request.NetworkUID))
						data = append(
							append(append([]byte{}, data[:pos]...), []byte(request.TelemetryUID)...),
							data[pos+len(request.NetworkUID):]...)
					} else {
						if err := os.Mkdir(
							filepath.Join(request.OutputDir, "table-00-page-000000.json"),
							0o700,
						); err != nil {
							t.Fatal(err)
						}
					}
				}
				response.Body = io.NopCloser(bytes.NewReader(data))
				return response, nil
			})
			var output bytes.Buffer
			if bundle(t.Context(), request, c, &output) == nil {
				t.Fatal("unretained page accepted")
			}
			var m evidenceManifest
			if err := json.Unmarshal(output.Bytes(), &m); err != nil {
				t.Fatal(err)
			}
			expected := "RecordIdentityMismatch"
			if mode == "write" {
				expected = "WriteFailed"
			}
			if string(m.Tables[0].Reason) != expected || m.Tables[0].Rows != 0 || len(m.Tables[0].Files) != 0 ||
				len(m.ContextMissing) != 4 ||
				len(m.CaptureGaps) != 0 {
				t.Fatalf("unretained page influenced evidence: %s", output.String())
			}
		})
	}
}

// TestQueryFailureClassification distinguishes resource limits and cancellation from outages.
func TestQueryFailureClassification(t *testing.T) {
	for _, mode := range []string{"bytes", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			request, c, _ := bundleFixture(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			expectedState, expectedReason := "Limited", "ResponseByteLimit"
			if mode == "bytes" {
				request.MaxBytes = 1
			} else {
				expectedState, expectedReason = "Interrupted", "CancelledOrDeadline"
				c.Transport = roundTripFunc(
					func(*http.Request) (*http.Response, error) { cancel(); return nil, context.Canceled },
				)
			}
			var output bytes.Buffer
			if bundle(ctx, request, c, &output) == nil {
				t.Fatal("query failure hidden")
			}
			var m evidenceManifest
			if err := json.Unmarshal(output.Bytes(), &m); err != nil {
				t.Fatal(err)
			}
			if string(m.Tables[0].State) != expectedState || string(m.Tables[0].Reason) != expectedReason {
				t.Fatalf("misclassified: %+v", m.Tables[0])
			}
		})
	}
}

// TestContextSerializationIsBounded includes escaped keys and nested body expansion.
func TestContextSerializationIsBounded(t *testing.T) {
	request, _, _ := bundleFixture(t)
	records := &queryRecords{}
	for _, name := range []string{"timestamp", "network_uid", "body", "source", "event_type", "object_uid"} {
		column := struct {
			Name     string `json:"name"`
			DataType string `json:"data_type"`
		}{Name: name, DataType: "String"}
		records.Schema.Columns = append(records.Schema.Columns, column)
	}
	b := bundleExporter{
		context:  map[string]json.RawMessage{},
		kinds:    map[string]bool{},
		manifest: evidenceManifest{CaptureGaps: map[string]int{}},
	}
	body := `{"kind":"Pod","nested":{"v":"` + strings.Repeat("a", 60000) + `"}}`
	for i := 0; i < 400; i++ {
		row := []string{
			"",
			request.NetworkUID,
			body,
			"pods",
			"Snapshot",
			fmt.Sprintf("%d%s", i, strings.Repeat("<", 200)),
		}
		records.Rows = [][]json.RawMessage{{}}
		for _, v := range row {
			encoded, _ := json.Marshal(v)
			records.Rows[0] = append(records.Rows[0], encoded)
		}
		b.summarize(records)
	}
	encoded, err := json.Marshal(b.context)
	if err != nil {
		t.Fatal(err)
	}
	if !b.manifest.ContextLimited || len(encoded) > 16<<20 || b.contextBytes+2 < len(encoded) {
		t.Fatalf("serialized context escaped bound: %d / %d", len(encoded), b.contextBytes)
	}
}
