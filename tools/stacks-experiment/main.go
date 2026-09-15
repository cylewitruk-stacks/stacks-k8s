// Command stacks-experiment provides bounded external-agent experiment primitives.
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

	"github.com/cylewitruk-stacks/stacks-k8s/libs/stacks/clarity"
	"github.com/cylewitruk-stacks/stacks-k8s/libs/stacks/identity"
	"github.com/cylewitruk-stacks/stacks-k8s/libs/stacks/rpc"
	"github.com/cylewitruk-stacks/stacks-k8s/libs/stacks/transaction"
)

const (
	commandSubmit = "submit"
	commandEvent  = "event"
	commandExport = "export"
)

var defaultExportHTTPClient = &http.Client{
	Timeout:       60 * time.Second,
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

func main() {
	flag.Parse()
	if err := run(context.Background(), flag.Args(), os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "stacks-experiment:", err)
		os.Exit(1)
	}
}

// run dispatches one strict JSON request to a bounded experiment primitive.
func run(ctx context.Context, args []string, input io.Reader, output io.Writer) error {
	if len(args) != 1 {
		return errors.New("usage: stacks-experiment <submit|event|export> < request.json")
	}
	switch args[0] {
	case commandSubmit:
		var request submitRequest
		if err := decode(input, &request); err != nil {
			return err
		}
		return submit(ctx, request, json.NewEncoder(output))
	case commandEvent:
		var request eventRequest
		if err := decode(input, &request); err != nil {
			return err
		}
		return writeEvent(request, json.NewEncoder(output))
	case commandExport:
		var request exportRequest
		if err := decode(input, &request); err != nil {
			return err
		}
		return export(ctx, request, output)
	default:
		return errors.New("unknown command")
	}
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

// submitRequest describes either one deployment or a finite contract-call batch.
type submitRequest struct {
	Endpoint       string `json:"endpoint"`
	PrivateKey     string `json:"privateKey"`
	Sender         string `json:"sender"`
	FeeMicroSTX    uint64 `json:"feeMicroSTX"`
	Contract       string `json:"contract"`
	ContractSource string `json:"contractSource,omitempty"`
	ClarityVersion byte   `json:"clarityVersion,omitempty"`
	Function       string `json:"function,omitempty"`
	Count          int    `json:"count,omitempty"`
	Writes         int    `json:"writes,omitempty"`
	Reads          int    `json:"reads,omitempty"`
	PayloadBytes   int    `json:"payloadBytes,omitempty"`
	KeyBase        uint64 `json:"keyBase,omitempty"`
	TimeoutSeconds int    `json:"timeoutSeconds"`
}

// experimentEvent records one transaction authorization or observation boundary.
type experimentEvent struct {
	Type       string    `json:"type"`
	ObservedAt time.Time `json:"observedAt"`
	TxID       string    `json:"txid"`
	Nonce      uint64    `json:"nonce"`
	Bytes      int       `json:"bytes,omitempty"`
	BlockID    string    `json:"blockID,omitempty"`
	Success    *bool     `json:"success,omitempty"`
}

// submit authorizes, sends once and observes exact transaction identities.
func submit(ctx context.Context, request submitRequest, output *json.Encoder) error {
	if err := validateSubmitRequest(request); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(request.TimeoutSeconds)*time.Second)
	defer cancel()
	public, err := identity.FromPrivate(request.PrivateKey)
	if err != nil || public.Address != request.Sender {
		return errors.New("private key does not match sender")
	}
	client, err := rpc.New(rpc.Config{
		Endpoint: request.Endpoint, Timeout: 30 * time.Second, MaxResponseBytes: 16 << 20,
	})
	if err != nil {
		return err
	}
	account, err := client.Account(ctx, request.Sender)
	if err != nil {
		return err
	}
	transactions, err := buildTransactions(request, account.Nonce)
	if err != nil {
		return err
	}
	for index, tx := range transactions {
		event := experimentEvent{
			Type: "Authorized", ObservedAt: time.Now().UTC(), TxID: tx.TxID,
			Nonce: account.Nonce + uint64(index), Bytes: len(tx.Bytes),
		}
		if err := output.Encode(event); err != nil {
			return errors.New("write authorization evidence")
		}
		if err := client.Submit(ctx, tx); err != nil {
			return fmt.Errorf("transaction %s submission outcome: %w", tx.TxID, err)
		}
		event.Type = "Submitted"
		event.ObservedAt = time.Now().UTC()
		if err := output.Encode(event); err != nil {
			return errors.New("write submission evidence")
		}
	}
	return observeTransactions(ctx, client, account.Nonce, transactions, output)
}

// validateSubmitRequest requires one unambiguous bounded transaction mode.
func validateSubmitRequest(request submitRequest) error {
	if request.FeeMicroSTX == 0 || request.TimeoutSeconds < 1 || request.TimeoutSeconds > 3600 ||
		request.Count < 0 || request.Count > 100 || request.Writes < 0 || request.Writes > 64 ||
		request.Reads < 0 || request.Reads > 1024 || request.PayloadBytes < 0 || request.PayloadBytes > 4096 {
		return errors.New("submission bounds are invalid")
	}
	if request.ContractSource != "" {
		if request.Function != "" || request.Count != 0 || request.Writes != 0 || request.Reads != 0 ||
			request.PayloadBytes != 0 || request.KeyBase != 0 {
			return errors.New("contract deployment and load-call fields are mutually exclusive")
		}
		return nil
	}
	if request.Function == "" || request.Count == 0 || request.ClarityVersion != 0 {
		return errors.New("contract call fields are incomplete")
	}
	return nil
}

// buildTransactions signs the finite request against one freshly read nonce sequence.
func buildTransactions(request submitRequest, nonce uint64) ([]transaction.Transaction, error) {
	count := request.Count
	if request.ContractSource != "" {
		count = 1
		if request.ClarityVersion == 0 {
			request.ClarityVersion = 4
		}
	} else if count == 0 {
		return nil, errors.New("contract call count is required")
	}
	key, err := identity.CompressedPrivateKey(request.PrivateKey)
	if err != nil {
		return nil, err
	}
	result := make([]transaction.Transaction, 0, count)
	for index := 0; index < count; index++ {
		options := transaction.Options{
			Version: transaction.Testnet, ChainID: 0x80000000, Nonce: nonce + uint64(index),
			Fee: request.FeeMicroSTX, PostConditionMode: transaction.Allow, PrivateKey: key,
		}
		var tx transaction.Transaction
		if request.ContractSource != "" {
			tx, err = transaction.Deploy(options, request.Contract, request.ContractSource, request.ClarityVersion)
		} else {
			tx, err = loadCall(options, request, index)
		}
		if err != nil {
			return nil, err
		}
		result = append(result, tx)
	}
	return result, nil
}

// loadCall builds one deterministic two-list load-driver invocation.
func loadCall(options transaction.Options, request submitRequest, ordinal int) (transaction.Transaction, error) {
	writes := clarity.Value{Type: clarity.List}
	reads := clarity.Value{Type: clarity.List}
	for index := 0; index < request.Writes; index++ {
		payload := make([]byte, request.PayloadBytes)
		for offset := range payload {
			// #nosec G115 -- Modulo 251 bounds the value before conversion.
			payload[offset] = byte((offset + ordinal + index) % 251)
		}
		// #nosec G115 -- Nonnegative ordinal/index are bounded by validated request limits.
		key := request.KeyBase + uint64(ordinal*64+index)
		writes.Items = append(writes.Items, clarity.Value{Type: clarity.Tuple, Fields: map[string]clarity.Value{
			"key":     clarity.Uint(key),
			"payload": {Type: clarity.Buffer, Bytes: payload},
		}})
	}
	// #nosec G115 -- Nonnegative ordinal is bounded by the validated transaction count.
	baseKey := request.KeyBase + uint64(ordinal*64)
	for index := 0; index < request.Reads; index++ {
		key := baseKey
		if request.Writes > 0 {
			key += uint64(index % request.Writes)
		}
		reads.Items = append(reads.Items, clarity.Uint(key))
	}
	parts := strings.Split(request.Contract, ".")
	if len(parts) != 2 {
		return transaction.Transaction{}, errors.New("contract call requires address.name")
	}
	return transaction.Call(options, parts[0], parts[1], request.Function, []clarity.Value{writes, reads})
}

// observeTransactions waits for exact canonical transaction inclusion without resubmitting.
func observeTransactions(
	ctx context.Context,
	client *rpc.Client,
	nonce uint64,
	transactions []transaction.Transaction,
	output *json.Encoder,
) error {
	pending := make(map[int]bool, len(transactions))
	for index := range transactions {
		pending[index] = true
	}
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for len(pending) > 0 {
		for index := range pending {
			included, err := client.Inclusion(ctx, transactions[index].TxID)
			if err != nil {
				return err
			}
			if !included.Found {
				continue
			}
			// #nosec G115 -- index is bounded by the validated transaction count.
			includedNonce := nonce + uint64(index)
			event := experimentEvent{
				Type: "Included", ObservedAt: time.Now().UTC(), TxID: transactions[index].TxID,
				Nonce: includedNonce, BlockID: included.BlockID, Success: &included.Success,
			}
			if err := output.Encode(event); err != nil {
				return errors.New("write inclusion evidence")
			}
			delete(pending, index)
		}
		if len(pending) == 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("%d transactions remain unobserved: %w", len(pending), ctx.Err())
		case <-ticker.C:
		}
	}
	return nil
}

// eventRequest describes one exact-network experiment phase marker.
type eventRequest struct {
	Namespace   string         `json:"namespace"`
	NetworkName string         `json:"networkName"`
	NetworkUID  string         `json:"networkUID"`
	Phase       string         `json:"phase"`
	Values      map[string]any `json:"values,omitempty"`
}

// writeEvent renders one core Kubernetes Event for explicit submission by the caller.
func writeEvent(request eventRequest, output *json.Encoder) error {
	if !dnsName.MatchString(request.Namespace) || !dnsName.MatchString(request.NetworkName) ||
		!uid.MatchString(request.NetworkUID) || request.Phase == "" || len(request.Phase) > 128 {
		return errors.New("event identity is invalid")
	}
	message, err := json.Marshal(map[string]any{"phase": request.Phase, "values": request.Values})
	if err != nil || len(message) > 2048 {
		return errors.New("event payload is invalid")
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	return output.Encode(map[string]any{
		"apiVersion": "v1", "kind": "Event",
		"metadata": map[string]any{
			"generateName": "stacks-experiment-", "namespace": request.Namespace,
			"labels": map[string]string{"network.stacks.org/network-uid": request.NetworkUID},
		},
		"involvedObject": map[string]any{
			"apiVersion": "network.stacks.org/v1alpha2", "kind": "StacksNetwork",
			"namespace": request.Namespace, "name": request.NetworkName, "uid": request.NetworkUID,
		},
		"reason": "ExperimentPhase", "message": string(message), "type": "Normal",
		"firstTimestamp": now, "lastTimestamp": now, "count": 1,
		"source": map[string]string{"component": "stacks-experiment"},
	})
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
	dnsName       = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)
	uid           = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	sqlIdentifier = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{0,127}$`)
)
