// Command stacks-evidence queries bounded recorded evidence.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"syscall"
	"time"
)

func main() {
	flag.Parse()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := run(ctx, flag.Args(), os.Stdin, os.Stdout)
	cancel()
	if err != nil {
		fmt.Fprintln(os.Stderr, "stacks-evidence:", err)
		os.Exit(1)
	}
}

// run accepts exactly one bounded exportRequest document.
func run(ctx context.Context, args []string, input io.Reader, output io.Writer) error {
	if len(args) == 2 && args[0] == "verify" {
		return verifyBundle(args[1])
	}
	if len(args) != 1 {
		return errors.New("usage: stacks-evidence <export|bundle> < request.json; stacks-evidence verify DIRECTORY")
	}
	switch args[0] {
	case "export":
		var request exportRequest
		if err := decode(input, &request); err != nil {
			return err
		}
		return export(ctx, request, output)
	case "bundle":
		var request bundleRequest
		if err := decode(input, &request); err != nil {
			return err
		}
		return bundle(ctx, request, defaultExportHTTPClient, output)
	default:
		return errors.New("unknown command")
	}
}

var defaultExportHTTPClient = &http.Client{
	Timeout:       60 * time.Second,
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

// decode accepts one bounded JSON document and rejects unknown fields.
func decode(input io.Reader, target any) error {
	decoder := json.NewDecoder(io.LimitReader(input, 2<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return errors.New("invalid request")
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return errors.New("request must contain one JSON value")
	}
	return nil
}

// exportRequest describes one bounded read-only Greptime query.
type exportRequest struct {
	Endpoint       string    `json:"endpoint"`
	Authorization  string    `json:"authorization"`
	Table          string    `json:"table"`
	TimeColumn     string    `json:"timeColumn"`
	NetworkUID     string    `json:"networkUID"`
	ParticipantUID string    `json:"participantUID,omitempty"`
	From           time.Time `json:"from"`
	To             time.Time `json:"to"`
	Limit          int       `json:"limit"`
}

// export validates a bounded query and executes it with the default transport.
func export(ctx context.Context, request exportRequest, output io.Writer) error {
	return exportWithClient(ctx, defaultExportHTTPClient, request, output)
}

// exportWithClient executes a validated bounded query through the supplied transport.
func exportWithClient(ctx context.Context, httpClient *http.Client, request exportRequest, output io.Writer) error {
	if err := validateExport(request); err != nil {
		return err
	}
	query := fmt.Sprintf(
		"SELECT * FROM %s WHERE network_uid = '%s' AND %s >= '%s' AND %s < '%s'",
		request.Table, request.NetworkUID, request.TimeColumn, request.From.UTC().Format(time.RFC3339Nano),
		request.TimeColumn, request.To.UTC().Format(time.RFC3339Nano),
	)
	if request.ParticipantUID != "" {
		query += " AND participant_uid = '" + request.ParticipantUID + "'"
	}
	query += fmt.Sprintf(" ORDER BY %s LIMIT %d", request.TimeColumn, request.Limit)
	data, err := executeSQL(ctx, httpClient, request.Endpoint, request.Authorization, query, 64<<20)
	if err != nil {
		return err
	}
	_, err = output.Write(data)
	return err
}

// validateExport enforces identity, time and query bounds independently of transport.
func validateExport(request exportRequest) error {
	parsed, err := url.Parse(request.Endpoint)
	validEndpoint := err == nil && parsed.Host != "" && (parsed.Scheme == "http" || parsed.Scheme == "https") &&
		parsed.User == nil && parsed.RawQuery == "" && parsed.Fragment == "" && (parsed.Path == "" || parsed.Path == "/")
	validIdentity := uid.MatchString(request.NetworkUID) &&
		(request.ParticipantUID == "" || uid.MatchString(request.ParticipantUID))
	if !validEndpoint || !validIdentity || !sqlIdentifier.MatchString(request.Table) ||
		(request.TimeColumn != "timestamp" && request.TimeColumn != "greptime_timestamp") ||
		request.Limit < 1 || request.Limit > 10000 || !request.To.After(request.From) ||
		request.To.Sub(request.From) > 24*time.Hour || !strings.HasPrefix(request.Authorization, "Basic ") ||
		strings.ContainsAny(request.Authorization, "\r\n") {
		return errors.New("export bounds are invalid")
	}
	return nil
}

// errResponseLimit distinguishes local response bounds from backend failure.
var errResponseLimit = errors.New("response byte limit exceeded")

// executeSQL issues one read-only generated query and bounds response memory.
func executeSQL(
	ctx context.Context,
	httpClient *http.Client,
	endpoint, authorization, query string,
	maximum int64,
) ([]byte, error) {
	body := url.Values{"sql": {query}}.Encode()
	req, err := http.NewRequestWithContext(
		ctx, http.MethodPost, strings.TrimSuffix(endpoint, "/")+"/v1/sql", bytes.NewBufferString(body),
	)
	if err != nil {
		return nil, errors.New("construct export request")
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", authorization)
	response, err := httpClient.Do(req) // #nosec G704 -- Endpoint is explicitly validated administrator input.
	if err != nil {
		return nil, errors.New("export transport unavailable")
	}
	defer func() { _ = response.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(response.Body, maximum+1))
	if int64(len(data)) > maximum {
		return nil, errResponseLimit
	}
	if err != nil || response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("export response unavailable (HTTP %d)", response.StatusCode)
	}
	return data, nil
}

var (
	uid           = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	sqlIdentifier = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{0,127}$`)
)
