package main

import (
	"bytes"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestExportAppliesIdentityTimeAndRowBounds(t *testing.T) {
	var query string
	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.Header.Get("Authorization") != "Basic dGVzdDp0ZXN0" {
			t.Error("authorization missing")
		}
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Error(err)
		}
		query = string(body)
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"output":[]}`)),
			Header:     make(http.Header),
		}, nil
	})}
	request := exportRequest{
		Endpoint: "http://greptime.example", Authorization: "Basic dGVzdDp0ZXN0", Table: "stacks_logs",
		TimeColumn: "timestamp", NetworkUID: "11111111-1111-1111-1111-111111111111",
		ParticipantUID: "22222222-2222-2222-2222-222222222222",
		From:           time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC),
		To:             time.Date(2026, 9, 15, 1, 0, 0, 0, time.UTC), Limit: 500,
	}
	var output bytes.Buffer
	if err := exportWithClient(t.Context(), httpClient, request, &output); err != nil {
		t.Fatal(err)
	}
	decoded, err := url.QueryUnescape(strings.TrimPrefix(query, "sql="))
	if err != nil || !strings.Contains(decoded, "network_uid = '11111111-1111-1111-1111-111111111111'") ||
		!strings.Contains(decoded, "participant_uid = '22222222-2222-2222-2222-222222222222'") ||
		!strings.Contains(decoded, "timestamp >= '2026-09-15T00:00:00Z'") ||
		!strings.HasSuffix(decoded, "ORDER BY timestamp LIMIT 500") {
		t.Fatalf("query is not bounded: %s", decoded)
	}
	request.To = request.From.Add(25 * time.Hour)
	if err := exportWithClient(t.Context(), httpClient, request, &bytes.Buffer{}); err == nil {
		t.Fatal("accepted oversized export window")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}
