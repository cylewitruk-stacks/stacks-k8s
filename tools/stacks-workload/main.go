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
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/cylewitruk-stacks/stacks-k8s/libs/stacks/clarity"
	"github.com/cylewitruk-stacks/stacks-k8s/libs/stacks/identity"
	"github.com/cylewitruk-stacks/stacks-k8s/libs/stacks/rpc"
	"github.com/cylewitruk-stacks/stacks-k8s/libs/stacks/transaction"
)

// defaultClarityVersion applies when a deployment omits clarityVersion.
const defaultClarityVersion byte = 4

func main() {
	flag.Parse()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := run(ctx, flag.Args(), os.Stdin, os.Stdout)
	stop()
	if err != nil {
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
	// IntervalMilliseconds is the minimum spacing between submission starts; zero is unpaced.
	IntervalMilliseconds int `json:"intervalMilliseconds,omitempty"`
	// MaxOutstanding caps submissions not yet observed canonically included; zero defaults to 100.
	MaxOutstanding int `json:"maxOutstanding,omitempty"`
	// ObservationConcurrency bounds simultaneous read-only inclusion requests; zero defaults to one.
	ObservationConcurrency int `json:"observationConcurrency,omitempty"`
	// DurationSeconds bounds the submission phase after nonce discovery; observation uses the total timeout.
	DurationSeconds int `json:"durationSeconds,omitempty"`
	// Variation selects reproducible call shapes, independently of runtime scheduling.
	Variation *variation `json:"variation,omitempty"`
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
	return execute(ctx, client, request, account.Nonce, output)
}

// validateSubmitRequest requires one unambiguous bounded transaction mode.
func validateSubmitRequest(request submitRequest) error {
	if request.FeeMicroSTX == 0 || request.TimeoutSeconds < 1 || request.TimeoutSeconds > 3600 ||
		request.Count < 0 || request.Count > 10000 || request.Writes < 0 || request.Writes > 64 ||
		request.Reads < 0 || request.Reads > 1024 || request.PayloadBytes < 0 || request.PayloadBytes > 4096 {
		return errors.New("submission bounds are invalid")
	}
	if err := validateControls(request); err != nil {
		return err
	}
	if request.ContractSource != "" {
		if request.Variation != nil {
			return errors.New("variation is only supported for calls")
		}
		if request.Function != "" || request.Count != 0 || request.Writes != 0 || request.Reads != 0 ||
			request.PayloadBytes != 0 || request.KeyBase != 0 {
			return errors.New("contract deployment and load-call fields are mutually exclusive")
		}
		return nil
	}
	if request.Function == "" || request.Count == 0 || request.ClarityVersion != 0 {
		return errors.New("contract call fields are incomplete")
	}
	return validateVariation(request)
}

// buildTransaction signs one ordinal lazily, keeping memory independent of total count.
func buildTransaction(request submitRequest, nonce uint64, ordinal int) (transaction.Transaction, error) {
	key, err := identity.CompressedPrivateKey(request.PrivateKey)
	if err != nil {
		return transaction.Transaction{}, err
	}
	options := transaction.Options{
		Version: transaction.Testnet, ChainID: 0x80000000, Nonce: nonce,
		Fee: request.FeeMicroSTX, PostConditionMode: transaction.Allow, PrivateKey: key,
	}
	if request.ContractSource != "" {
		return transaction.Deploy(options, request.Contract, request.ContractSource, request.deploymentVersion())
	}
	return loadCall(options, request, ordinal)
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

// deploymentVersion resolves the supported default for deployment construction and evidence.
func (r submitRequest) deploymentVersion() byte {
	if r.ClarityVersion == 0 {
		return defaultClarityVersion
	}
	return r.ClarityVersion
}
