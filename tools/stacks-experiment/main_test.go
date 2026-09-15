package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/cylewitruk-stacks/stacks-k8s/libs/stacks/transaction"
)

func TestEventIsIdentityBoundAndContainsNoArbitraryMetadata(t *testing.T) {
	request := eventRequest{
		Namespace: "experiment", NetworkName: "network",
		NetworkUID: "11111111-1111-1111-1111-111111111111", Phase: "load-start",
		Values: map[string]any{"width": float64(16)},
	}
	var output bytes.Buffer
	if err := writeEvent(request, json.NewEncoder(&output)); err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(output.Bytes(), &object); err != nil {
		t.Fatal(err)
	}
	metadata := object["metadata"].(map[string]any)
	involved := object["involvedObject"].(map[string]any)
	if metadata["namespace"] != "experiment" || involved["uid"] != request.NetworkUID ||
		object["reason"] != "ExperimentPhase" || !strings.Contains(object["message"].(string), "load-start") {
		t.Fatalf("event identity differs: %s", output.String())
	}
	request.NetworkUID = "not-a-uid"
	if err := writeEvent(request, json.NewEncoder(&bytes.Buffer{})); err == nil {
		t.Fatal("accepted unbound event")
	}
}

func TestDecodeRejectsTrailingAndUnknownInput(t *testing.T) {
	for _, input := range []string{`{"phase":"x","unknown":true}`, `{}` + `{}`} {
		var request eventRequest
		if decode(strings.NewReader(input), &request) == nil {
			t.Fatalf("accepted %s", input)
		}
	}
}

func TestLoadCallBoundsAndDeterminism(t *testing.T) {
	request := submitRequest{
		Contract: "ST000000000000000000002AMW42H.load-driver", Function: "run16",
		Writes: 16, Reads: 128, PayloadBytes: 1024, KeyBase: 1000,
	}
	options := transactionOptions(t)
	first, err := loadCall(options, request, 0)
	if err != nil {
		t.Fatal(err)
	}
	second, err := loadCall(options, request, 0)
	if err != nil || first.TxID != second.TxID || !bytes.Equal(first.Bytes, second.Bytes) {
		t.Fatal("load transaction is not deterministic")
	}
}

func TestSubmitRequestModesAreExclusiveAndBounded(t *testing.T) {
	base := submitRequest{FeeMicroSTX: 1000, TimeoutSeconds: 60, Contract: "load-driver"}
	deployment := base
	deployment.ContractSource = "(define-public (ok) (ok true))"
	if err := validateSubmitRequest(deployment); err != nil {
		t.Fatal(err)
	}
	call := base
	call.Contract = "ST000000000000000000002AMW42H.load-driver"
	call.Function, call.Count = "run16", 1
	if err := validateSubmitRequest(call); err != nil {
		t.Fatal(err)
	}
	deployment.Count = 1
	if err := validateSubmitRequest(deployment); err == nil {
		t.Fatal("accepted deployment with call fields")
	}
	call.ClarityVersion = 4
	if err := validateSubmitRequest(call); err == nil {
		t.Fatal("accepted call with deployment fields")
	}
}

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

func transactionOptions(t *testing.T) transaction.Options {
	t.Helper()
	return transaction.Options{
		Version: transaction.Testnet, ChainID: 0x80000000, Fee: 1000,
		PostConditionMode: transaction.Allow,
		PrivateKey:        strings.Repeat("0", 63) + "101",
	}
}
