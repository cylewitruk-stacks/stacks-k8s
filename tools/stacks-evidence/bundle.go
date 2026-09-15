package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// bundleRequest selects an exact recording and finite export budget. Secrets are input-only.
type bundleRequest struct {
	Endpoint       string    `json:"endpoint"`
	Authorization  string    `json:"authorization"`
	NetworkUID     string    `json:"networkUID"`
	TelemetryUID   string    `json:"telemetryUID"`
	TablePrefix    string    `json:"tablePrefix"`
	From           time.Time `json:"from"`
	To             time.Time `json:"to"`
	OutputDir      string    `json:"outputDir"`
	Metrics        []string  `json:"metrics,omitempty"`
	PageSize       int       `json:"pageSize,omitempty"`
	WindowSeconds  int       `json:"windowSeconds,omitempty"`
	MaxRows        int       `json:"maxRows,omitempty"`
	MaxBytes       int64     `json:"maxBytes,omitempty"`
	MaxQueries     int       `json:"maxQueries,omitempty"`
	TimeoutSeconds int       `json:"timeoutSeconds,omitempty"`
}

// exportState describes traversal, never evidence completeness or actor health.
type exportState string

const (
	stateExported    exportState = "Exported"
	stateUnavailable exportState = "Unavailable"
	stateLimited     exportState = "Limited"
	stateInterrupted exportState = "Interrupted"
)

// exportReason identifies a bounded export failure without leaking backend details.
type exportReason string

const (
	reasonCancelledOrDeadline       exportReason = "CancelledOrDeadline"
	reasonExportBudget              exportReason = "ExportBudget"
	reasonQueryUnavailable          exportReason = "QueryUnavailable"
	reasonResponseByteLimit         exportReason = "ResponseByteLimit"
	reasonInvalidRecords            exportReason = "InvalidRecords"
	reasonIdentitySchemaUnavailable exportReason = "IdentitySchemaUnavailable"
	reasonSchemaOrLimitChanged      exportReason = "SchemaOrLimitChanged"
	reasonRecordIdentityMismatch    exportReason = "RecordIdentityMismatch"
	reasonWriteFailed               exportReason = "WriteFailed"
)

// evidenceFile binds a generated relative filename to exact exported bytes.
type evidenceFile struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
	Bytes  int64  `json:"bytes"`
	Rows   int    `json:"rows,omitempty"`
}

// tableExport records what was actually traversed for one source table.
type tableExport struct {
	Table   string         `json:"table"`
	State   exportState    `json:"state"`
	Reason  exportReason   `json:"reason,omitempty"`
	Rows    int            `json:"rows"`
	Through time.Time      `json:"through"`
	Files   []evidenceFile `json:"files"`
}

// evidenceManifest distinguishes retained observations, missing context and export limits.
type evidenceManifest struct {
	Format          string         `json:"format"`
	NetworkUID      string         `json:"networkUID"`
	TelemetryUID    string         `json:"telemetryUID"`
	From            time.Time      `json:"from"`
	To              time.Time      `json:"to"`
	StartedAt       time.Time      `json:"startedAt"`
	FinishedAt      time.Time      `json:"finishedAt"`
	Consistency     string         `json:"consistency"`
	Coverage        string         `json:"coverage"`
	State           exportState    `json:"state"`
	Tables          []tableExport  `json:"tables"`
	CaptureGaps     map[string]int `json:"captureGaps"`
	OmittedPayloads int            `json:"omittedPayloads"`
	ContextMissing  []string       `json:"contextMissing"`
	ContextLimited  bool           `json:"contextLimited"`
	Context         evidenceFile   `json:"context"`
	Queries         int            `json:"queries"`
	Limits          bundleLimits   `json:"limits"`
}

// bundleLimits records the selected traversal budgets without backend credentials.
type bundleLimits struct {
	PageSize       int   `json:"pageSize"`
	WindowSeconds  int   `json:"windowSeconds"`
	MaxRows        int   `json:"maxRows"`
	MaxPageBytes   int64 `json:"maxPageBytes"`
	MaxQueries     int   `json:"maxQueries"`
	TimeoutSeconds int   `json:"timeoutSeconds"`
}

// queryRecords preserves server schema and exact JSON values without float conversion.
type queryRecords struct {
	Schema struct {
		Columns []struct {
			Name     string `json:"name"`
			DataType string `json:"data_type"`
		} `json:"column_schemas"`
	} `json:"schema"`
	Rows [][]json.RawMessage `json:"rows"`
}

// parseRecords rejects HTTP-success SQL errors, malformed schemas and truncated row shapes.
func parseRecords(data []byte) (*queryRecords, error) {
	var envelope struct {
		Code   int `json:"code"`
		Output []struct {
			Records *queryRecords `json:"records"`
		} `json:"output"`
	}
	if json.Unmarshal(data, &envelope) != nil || envelope.Code != 0 || len(envelope.Output) != 1 ||
		envelope.Output[0].Records == nil {
		return nil, errors.New("backend did not return records")
	}
	records := envelope.Output[0].Records
	if len(records.Schema.Columns) == 0 || len(records.Schema.Columns) > 256 {
		return nil, errors.New("backend schema unavailable")
	}
	seen := map[string]bool{}
	for _, c := range records.Schema.Columns {
		if !sqlIdentifier.MatchString(c.Name) || seen[c.Name] {
			return nil, errors.New("backend schema invalid")
		}
		seen[c.Name] = true
	}
	for _, row := range records.Rows {
		if len(row) != len(records.Schema.Columns) {
			return nil, errors.New("backend row incomplete")
		}
	}
	return records, nil
}

// bundleExporter owns one newly created output directory and bounded backend reads.
type bundleExporter struct {
	request      bundleRequest
	client       *http.Client
	manifest     evidenceManifest
	rows         int
	bytes        int64
	contextBytes int
	context      map[string]json.RawMessage
	kinds        map[string]bool
}

// normalizeBundle validates before claiming the output directory or querying a backend.
func normalizeBundle(r *bundleRequest) error {
	if r.PageSize == 0 {
		r.PageSize = 1000
	}
	if r.WindowSeconds == 0 {
		r.WindowSeconds = 45
	}
	if r.MaxRows == 0 {
		r.MaxRows = 100000
	}
	if r.MaxBytes == 0 {
		r.MaxBytes = 256 << 20
	}
	if r.MaxQueries == 0 {
		r.MaxQueries = 4000
	}
	if r.TimeoutSeconds == 0 {
		r.TimeoutSeconds = 300
	}
	probe := exportRequest{
		Endpoint:      r.Endpoint,
		Authorization: r.Authorization,
		Table:         r.TablePrefix + "_objects",
		TimeColumn:    "timestamp",
		NetworkUID:    r.NetworkUID,
		From:          r.From,
		To:            r.To,
		Limit:         r.PageSize,
	}
	if validateExport(probe) != nil || !uid.MatchString(r.TelemetryUID) || r.OutputDir == "" ||
		r.PageSize > 10000 || r.WindowSeconds < 1 || r.WindowSeconds > 300 || r.MaxRows < 1 || r.MaxRows > 1000000 ||
		r.MaxBytes < 1 || r.MaxBytes > 1<<30 || r.MaxQueries < 1 || r.MaxQueries > 10000 ||
		r.TimeoutSeconds < 1 || r.TimeoutSeconds > 3600 ||
		len(r.Metrics) > 32 || r.To.After(time.Now()) {
		return errors.New("bundle bounds are invalid")
	}
	seen := map[string]bool{r.TablePrefix + "_objects": true, r.TablePrefix + "_logs": true}
	for _, name := range r.Metrics {
		if !sqlIdentifier.MatchString(name) || seen[name] {
			return errors.New("metric selection invalid")
		}
		seen[name] = true
	}
	return nil
}

// bundle exports existing redacted records; all sources are attempted within the shared budget.
func bundle(ctx context.Context, r bundleRequest, c *http.Client, out io.Writer) (result error) {
	if err := normalizeBundle(&r); err != nil {
		return err
	}
	if err := os.Mkdir(r.OutputDir, 0o700); err != nil {
		return fmt.Errorf("output directory must be new: %s", r.OutputDir)
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(r.TimeoutSeconds)*time.Second)
	defer cancel()
	b := &bundleExporter{
		request: r,
		client:  c,
		context: map[string]json.RawMessage{},
		kinds:   map[string]bool{},
		manifest: evidenceManifest{
			Format:       "stacks-evidence/v1",
			NetworkUID:   r.NetworkUID,
			TelemetryUID: r.TelemetryUID,
			From:         r.From,
			To:           r.To,
			StartedAt:    time.Now().UTC(),
			Consistency:  "LiveReadNoSnapshot",
			Coverage:     "Unknown",
			State:        stateExported,
			CaptureGaps:  map[string]int{},
			Limits: bundleLimits{
				r.PageSize,
				r.WindowSeconds,
				r.MaxRows,
				r.MaxBytes,
				r.MaxQueries,
				r.TimeoutSeconds,
			},
		},
	}
	// An interrupted process may lack a manifest; such a directory is never a completed bundle.
	defer func() {
		for _, kind := range []string{"StacksNetwork", "StacksGenesis", "StacksNetworkParticipant", "Pod"} {
			if !b.kinds[kind] {
				b.manifest.ContextMissing = append(b.manifest.ContextMissing, kind)
			}
		}
		content, err := json.Marshal(b.context)
		if err == nil {
			b.manifest.Context, err = b.write("context.json", content, 0)
		}
		if err != nil {
			result = errors.Join(result, errors.New("write public context"))
			b.manifest.State = stateInterrupted
		}
		if b.manifest.ContextLimited {
			b.manifest.State = stateLimited
			result = errors.Join(result, errors.New("public context limit"))
		}
		b.manifest.FinishedAt = time.Now().UTC()
		content, err = json.MarshalIndent(b.manifest, "", "  ")
		if err == nil {
			_, err = b.write("manifest.json", content, 0)
		}
		if err != nil {
			result = errors.Join(result, errors.New("write evidence manifest"))
		}
		if err := json.NewEncoder(out).Encode(b.manifest); err != nil {
			result = errors.Join(result, err)
		}
	}()
	tables := append([]string{r.TablePrefix + "_objects", r.TablePrefix + "_logs"}, r.Metrics...)
	for index, table := range tables {
		state := b.exportTable(ctx, index, table)
		b.manifest.Tables = append(b.manifest.Tables, state)
		if state.State != stateExported {
			b.manifest.State = stateLimited
			result = errors.Join(result, fmt.Errorf("table %s: %s", table, state.Reason))
		}
	}
	return result
}

// write creates only generated relative names inside the newly claimed directory.
func (b *bundleExporter) write(name string, data []byte, rows int) (evidenceFile, error) {
	path := filepath.Join(b.request.OutputDir, name)
	// #nosec G304 G703 -- Caller-selected new output directory; filename is generated internally.
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return evidenceFile{}, err
	}
	_, writeErr := file.Write(data)
	closeErr := file.Close()
	if err = errors.Join(writeErr, closeErr); err != nil {
		return evidenceFile{}, err
	}
	sum := sha256.Sum256(data)
	return evidenceFile{Name: name, SHA256: hex.EncodeToString(sum[:]), Bytes: int64(len(data)), Rows: rows}, nil
}

// exportTable pages finite time windows using a total order over returned scalar columns.
func (b *bundleExporter) exportTable(ctx context.Context, index int, table string) tableExport {
	state := tableExport{Table: table, State: stateExported, Through: b.request.From, Files: []evidenceFile{}}
	column := "timestamp"
	if index >= 2 {
		column = "greptime_timestamp"
	}
	order := ""
	var schema []byte
	page := 0
	for start := b.request.From; start.Before(b.request.To); {
		end := start.Add(time.Duration(b.request.WindowSeconds) * time.Second)
		if end.After(b.request.To) {
			end = b.request.To
		}
		offset := 0
		for {
			if ctx.Err() != nil {
				state.State = stateInterrupted
				state.Reason = reasonCancelledOrDeadline
				return state
			}
			if b.rows >= b.request.MaxRows || b.bytes >= b.request.MaxBytes ||
				b.manifest.Queries >= b.request.MaxQueries {
				state.State = stateLimited
				state.Reason = reasonExportBudget
				return state
			}
			where := fmt.Sprintf(
				"network_uid = '%s' AND \"%s\" >= '%s' AND \"%s\" < '%s'",
				b.request.NetworkUID,
				column,
				start.UTC().Format(time.RFC3339Nano),
				column,
				end.UTC().Format(time.RFC3339Nano),
			)
			if index >= 2 {
				where += " AND telemetry_uid = '" + b.request.TelemetryUID + "'"
			}
			limit := min(b.request.PageSize, b.request.MaxRows-b.rows)
			query := "SELECT * FROM " + table + " WHERE " + where
			if order == "" {
				query += " LIMIT 0"
			} else {
				query += fmt.Sprintf(" ORDER BY %s LIMIT %d OFFSET %d", order, limit, offset)
			}
			b.manifest.Queries++
			data, err := executeSQL(
				ctx,
				b.client,
				b.request.Endpoint,
				b.request.Authorization,
				query,
				min(int64(64<<20), b.request.MaxBytes-b.bytes),
			)
			if err != nil {
				state.State = stateUnavailable
				state.Reason = reasonQueryUnavailable
				if ctx.Err() != nil {
					state.State = stateInterrupted
					state.Reason = reasonCancelledOrDeadline
				} else if errors.Is(err, errResponseLimit) {
					state.State = stateLimited
					state.Reason = reasonResponseByteLimit
				}
				return state
			}
			records, err := parseRecords(data)
			if err != nil {
				state.State = stateUnavailable
				state.Reason = reasonInvalidRecords
				return state
			}
			currentSchema, _ := json.Marshal(records.Schema)
			if order == "" {
				names := []string{}
				hasTime, hasNetwork, hasTelemetry := false, false, false
				for _, c := range records.Schema.Columns {
					names = append(names, "\""+c.Name+"\"")
					hasTime = hasTime || c.Name == column
					hasNetwork = hasNetwork || c.Name == "network_uid"
					hasTelemetry = hasTelemetry || c.Name == "telemetry_uid"
				}
				if !hasTime || !hasNetwork || (index >= 2 && !hasTelemetry) {
					state.State = stateUnavailable
					state.Reason = reasonIdentitySchemaUnavailable
					return state
				}
				sort.Strings(names)
				order = "\"" + column + "\""
				for _, name := range names {
					if name != "\""+column+"\"" {
						order += ", " + name
					}
				}
				schema = currentSchema
				continue
			}
			if string(currentSchema) != string(schema) || len(records.Rows) > limit {
				state.State = stateUnavailable
				state.Reason = reasonSchemaOrLimitChanged
				return state
			}
			if err := b.inspect(records); err != nil {
				state.State = stateUnavailable
				state.Reason = reasonRecordIdentityMismatch
				return state
			}
			if len(records.Rows) > 0 {
				file, err := b.write(fmt.Sprintf("table-%02d-page-%06d.json", index, page), data, len(records.Rows))
				if err != nil {
					state.State = stateInterrupted
					state.Reason = reasonWriteFailed
					return state
				}
				state.Files = append(state.Files, file)
				if index == 0 {
					b.summarize(records)
				}
				state.Rows += len(records.Rows)
				b.rows += len(records.Rows)
				b.bytes += int64(len(data))
				page++
			}
			if len(records.Rows) < limit {
				state.Through = end
				break
			}
			offset += len(records.Rows)
		}
		start = end
	}
	return state
}

// inspect validates every row before a page can be retained.
func (b *bundleExporter) inspect(records *queryRecords) error {
	for _, row := range records.Rows {
		fields, text := recordFields(records, row)
		if text("network_uid") != b.request.NetworkUID {
			return errors.New("foreign network row")
		}
		if value, ok := fields["telemetry_uid"]; ok {
			var id string
			if json.Unmarshal(value, &id) != nil || id != b.request.TelemetryUID {
				return errors.New("foreign recording row")
			}
		}
	}
	return nil
}

// recordFields decodes string fields without converting numeric JSON values.
func recordFields(records *queryRecords, row []json.RawMessage) (map[string]json.RawMessage, func(string) string) {
	fields := map[string]json.RawMessage{}
	for i, c := range records.Schema.Columns {
		fields[c.Name] = row[i]
	}
	return fields, func(name string) string { var value string; _ = json.Unmarshal(fields[name], &value); return value }
}

// summarize derives context only from a validated, successfully written page.
func (b *bundleExporter) summarize(records *queryRecords) {
	for _, row := range records.Rows {
		_, text := recordFields(records, row)
		if text("event_type") == "CaptureGap" {
			b.manifest.CaptureGaps[text("source")]++
			continue
		}
		var body map[string]json.RawMessage
		if json.Unmarshal([]byte(text("body")), &body) != nil {
			continue
		}
		if _, ok := body["payloadOmitted"]; ok {
			b.manifest.OmittedPayloads++
			continue
		}
		var kind string
		_ = json.Unmarshal(body["kind"], &kind)
		switch kind {
		case "StacksNetwork", "StacksGenesis", "StacksNetworkParticipant", "Pod":
		default:
			continue
		}
		id := text("object_uid")
		if id == "" {
			continue
		}
		raw := text("body")
		if len(raw) > 64<<10 || len(id) > 256 {
			b.manifest.ContextLimited = true
			continue
		}
		data, err := json.Marshal(json.RawMessage(raw))
		if err != nil {
			continue
		}
		key, _ := json.Marshal(id)
		overhead := 0
		if b.context[id] == nil {
			overhead = len(key) + 2
		}
		newSize := b.contextBytes - len(b.context[id]) + len(data) + overhead
		if len(data) > 64<<10 || newSize+2 > 16<<20 || (b.context[id] == nil && len(b.context) >= 4096) {
			b.manifest.ContextLimited = true
			continue
		}
		b.contextBytes = newSize
		b.kinds[kind] = true
		b.context[id] = data
	}
}

// verifyBundle checks local file integrity, not authenticity or evidence completeness.
func verifyBundle(directory string) error {
	// Root confines manifest-derived filenames to the selected evidence directory.
	root, err := os.OpenRoot(directory)
	if err != nil {
		return errors.New("evidence directory unavailable")
	}
	defer func() { _ = root.Close() }()
	file, err := root.Open("manifest.json")
	if err != nil {
		return errors.New("manifest unavailable")
	}
	data, readErr := io.ReadAll(io.LimitReader(file, (8<<20)+1))
	closeErr := file.Close()
	if readErr != nil || closeErr != nil || len(data) > 8<<20 {
		return errors.New("manifest unreadable or oversized")
	}

	var manifest evidenceManifest
	if json.Unmarshal(data, &manifest) != nil || manifest.Format != "stacks-evidence/v1" {
		return errors.New("invalid manifest")
	}
	files := []evidenceFile{manifest.Context}
	for _, table := range manifest.Tables {
		files = append(files, table.Files...)
	}
	seen := map[string]bool{}
	for _, file := range files {
		if file.Name == "" || filepath.Base(file.Name) != file.Name || strings.ContainsAny(file.Name, "/\\") ||
			seen[file.Name] {
			return errors.New("invalid manifest file")
		}
		seen[file.Name] = true
		content, err := root.Open(file.Name)
		if err != nil {
			return errors.New("evidence file missing")
		}
		info, statErr := content.Stat()
		if statErr != nil || !info.Mode().IsRegular() || info.Size() != file.Bytes || file.Bytes < 0 ||
			file.Bytes > 1<<30 {
			_ = content.Close()
			return errors.New("evidence file missing or resized")
		}
		hash := sha256.New()
		_, readErr := io.Copy(hash, content)
		closeErr := content.Close()
		if readErr != nil || closeErr != nil || hex.EncodeToString(hash.Sum(nil)) != file.SHA256 {
			return errors.New("evidence checksum mismatch")
		}

	}
	return nil
}
