// Command stacks-workload submits bounded transaction workloads.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/cylewitruk-stacks/stacks-k8s/libs/stacks/clarity"
	"github.com/cylewitruk-stacks/stacks-k8s/libs/stacks/identity"
	"github.com/cylewitruk-stacks/stacks-k8s/libs/stacks/rpc"
	"github.com/cylewitruk-stacks/stacks-k8s/libs/stacks/transaction"
)

func main() {
	flag.Parse()
	if err := run(context.Background(), flag.Args(), os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "stacks-workload:", err)
		os.Exit(1)
	}
}

// run accepts exactly one bounded submitRequest document.
func run(ctx context.Context, args []string, input io.Reader, output io.Writer) error {
	if len(args) != 1 || args[0] != "submit" {
		return errors.New("usage: stacks-workload submit < request.json")
	}
	var request submitRequest
	if err := decode(input, &request); err != nil {
		return err
	}
	return submit(ctx, request, json.NewEncoder(output))
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
