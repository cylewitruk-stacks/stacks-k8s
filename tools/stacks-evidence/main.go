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
	"regexp"
	"strings"
	"time"
)

func main() {
	flag.Parse()
	if err := run(context.Background(), flag.Args(), os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "stacks-evidence:", err)
		os.Exit(1)
	}
}

// run accepts exactly one bounded exportRequest document.
func run(ctx context.Context, args []string, input io.Reader, output io.Writer) error {
	if len(args) != 1 || args[0] != "export" {
		return errors.New("usage: stacks-evidence export < request.json")
	}
	var request exportRequest
	if err := decode(input, &request); err != nil {
		return err
	}
	return export(ctx, request, output)
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
	query := fmt.Sprintf(
		"SELECT * FROM %s WHERE network_uid = '%s' AND %s >= '%s' AND %s < '%s'",
		request.Table, request.NetworkUID, request.TimeColumn, request.From.UTC().Format(time.RFC3339Nano),
		request.TimeColumn, request.To.UTC().Format(time.RFC3339Nano),
	)
	if request.ParticipantUID != "" {
		query += " AND participant_uid = '" + request.ParticipantUID + "'"
	}
	query += fmt.Sprintf(" ORDER BY %s LIMIT %d", request.TimeColumn, request.Limit)
	body := url.Values{"sql": {query}}.Encode()
	req, err := http.NewRequestWithContext(
		ctx, http.MethodPost, strings.TrimSuffix(request.Endpoint, "/")+"/v1/sql", bytes.NewBufferString(body),
	)
	if err != nil {
		return errors.New("construct export request")
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", request.Authorization)
	response, err := httpClient.Do(req) // #nosec G704 -- Endpoint is explicitly validated administrator input.
	if err != nil {
		return errors.New("export transport unavailable")
	}
	defer func() { _ = response.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(response.Body, (64<<20)+1))
	if err != nil || len(data) > 64<<20 || response.StatusCode != http.StatusOK {
		return fmt.Errorf("export response unavailable (HTTP %d)", response.StatusCode)
	}
	_, err = output.Write(data)
	return err
}

var (
	uid           = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	sqlIdentifier = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{0,127}$`)
)
