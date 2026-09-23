package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// backendRequest requires explicit table checks rather than claiming universal coverage.
type backendRequest struct {
	Endpoint      string         `json:"endpoint"`
	Authorization string         `json:"authorization"`
	Tables        []tableRequest `json:"tables"`
}

// tableRequest identifies the timestamp column of one expected evidence source.
type tableRequest struct {
	Name       string `json:"name"`
	TimeColumn string `json:"timeColumn"`
}

var tablePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{0,127}$`)

func (r backendRequest) validate() error {
	u, err := url.Parse(r.Endpoint)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" ||
		u.Fragment != "" ||
		(u.Path != "" && u.Path != "/") ||
		!strings.HasPrefix(r.Authorization, "Basic ") ||
		strings.ContainsAny(r.Authorization, "\r\n") ||
		len(r.Tables) < 1 ||
		len(r.Tables) > 8 {
		return errors.New("invalid backend inputs")
	}
	for _, t := range r.Tables {
		if !tablePattern.MatchString(t.Name) || (t.TimeColumn != "timestamp" && t.TimeColumn != "greptime_timestamp") {
			return errors.New("invalid evidence table")
		}
	}
	return nil
}

// checkBackend asks only whether each selected table has a row in the bounded recent window.
func checkBackend(ctx context.Context, r request, out *report) {
	client := &http.Client{
		Timeout:       10 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	for _, table := range r.Backend.Tables {
		now := time.Now().UTC()
		query := fmt.Sprintf(
			"SELECT %s FROM %s WHERE network_uid = '%s' AND %s >= '%s' AND %s <= '%s' ORDER BY %s DESC LIMIT 1",
			table.TimeColumn,
			table.Name,
			r.NetworkUID,
			table.TimeColumn,
			now.Add(-time.Duration(r.MaxAgeSeconds)*time.Second).Format(time.RFC3339Nano),
			table.TimeColumn,
			now.Add(clockSkewTolerance).Format(time.RFC3339Nano),
			table.TimeColumn,
		)
		err := queryRecent(ctx, client, r.Backend, query)
		if err != nil {
			out.add("backend/"+table.Name, checkUnknown, err.Error())
		} else {
			out.add(
				"backend/"+table.Name,
				checkPass,
				"at least one row in the requested window; completeness and per-actor coverage not asserted",
			)
		}
	}
}

// queryRecent bounds responses and never echoes backend diagnostics or authorization.
func queryRecent(ctx context.Context, c *http.Client, r backendRequest, query string) error {
	body := url.Values{"sql": {query}}.Encode()
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		strings.TrimSuffix(r.Endpoint, "/")+"/v1/sql",
		bytes.NewBufferString(body),
	)
	if err != nil {
		return errors.New("backend request unavailable")
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", r.Authorization)
	resp, err := c.Do(req) // #nosec G704 -- Endpoint is validated explicit administrator input.
	if err != nil {
		return errors.New("backend transport unavailable")
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 65537))
	if err != nil || len(data) > 65536 || resp.StatusCode != http.StatusOK {
		return errors.New("backend response unavailable or exceeds bound")
	}
	var result struct {
		Code   int `json:"code"`
		Output []struct {
			Records *struct {
				Rows [][]json.RawMessage `json:"rows"`
			} `json:"records"`
		} `json:"output"`
	}
	if json.Unmarshal(data, &result) != nil || result.Code != 0 || len(result.Output) != 1 ||
		result.Output[0].Records == nil {
		return errors.New("backend query result unavailable")
	}
	rows := result.Output[0].Records.Rows
	if len(rows) != 1 || len(rows[0]) != 1 || string(rows[0][0]) == "null" {
		return errors.New("no recent row established for requested table")
	}
	return nil
}
