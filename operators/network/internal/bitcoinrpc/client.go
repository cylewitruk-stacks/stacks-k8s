// Package bitcoinrpc implements non-retrying native Bitcoin Core RPC transport.
package bitcoinrpc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"
)

// Credentials contains the static producer principal and administrator-approved server configuration digest.
type Credentials struct {
	// Username identifies the method-restricted producer principal.
	Username string `json:"username"`
	// Password authenticates only the producer; it must never be logged.
	Password string `json:"password"`
	// ConfigDigest identifies the exact approved Bitcoin configuration bytes.
	ConfigDigest string `json:"configDigest"`
}

// RPC exposes only the preflight and single-block operations used by this controller.
type RPC interface {
	Check(context.Context, string, string) error
	Generate(context.Context, string, string, string) (string, error)
}

// Client is a non-retrying, non-redirecting typed Bitcoin Core client.
type Client struct {
	credentials Credentials
	client      *http.Client
}

// New creates a client with bounded connection establishment and no connection reuse.
func New(credentials Credentials) *Client {
	return &Client{credentials: credentials, client: &http.Client{
		Transport:     &http.Transport{Proxy: nil, DialContext: (&net.Dialer{Timeout: 3 * time.Second}).DialContext, DisableKeepAlives: true},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
}

// ErrInvalidPreflight identifies a definite negative result from a successful preflight read.
var ErrInvalidPreflight = errors.New("invalid production preflight")

// Check verifies regtest and destination validity without creating wallet state.
func (r *Client) Check(ctx context.Context, endpoint, address string) error {
	var chain struct {
		Chain string `json:"chain"`
	}
	if err := r.Call(ctx, endpoint, "preflight-chain", MethodGetBlockchainInfo, []any{}, &chain); err != nil {
		return err
	}
	if chain.Chain == "" {
		return fmt.Errorf("RPC preflight omitted chain identity")
	}
	if chain.Chain != ChainRegtest {
		return fmt.Errorf("%w: target is not regtest", ErrInvalidPreflight)
	}
	var validation struct {
		Valid *bool `json:"isvalid"`
	}
	if err := r.Call(ctx, endpoint, "preflight-address", MethodValidateAddress, []any{address}, &validation); err != nil {
		return err
	}
	if validation.Valid == nil {
		return fmt.Errorf("RPC preflight omitted destination validity")
	}
	if !*validation.Valid {
		return fmt.Errorf("%w: invalid regtest destination", ErrInvalidPreflight)
	}
	return nil
}

// Generate requests exactly one block and requires an attributable success receipt.
func (r *Client) Generate(ctx context.Context, endpoint, address, id string) (string, error) {
	var hashes []string
	if err := r.Call(ctx, endpoint, id, MethodGenerateToAddress, []any{1, address, 1000000}, &hashes); err != nil {
		return "", err
	}
	if len(hashes) != 1 || len(hashes[0]) != 64 {
		return "", fmt.Errorf("incomplete generation receipt")
	}
	for _, c := range hashes[0] {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return "", fmt.Errorf("invalid block hash")
		}
	}
	return hashes[0], nil
}

// Call avoids replayable request bodies and bounds response memory; errors omit server response bodies.
func (r *Client) Call(ctx context.Context, endpoint, id, method string, params []any, result any) error {
	if params == nil {
		params = []any{}
	}
	body, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, io.NopCloser(bytes.NewReader(body)))
	if err != nil {
		return fmt.Errorf("create RPC request")
	}
	request.Header.Set("Content-Type", "application/json")
	request.SetBasicAuth(r.credentials.Username, r.credentials.Password)
	response, err := r.client.Do(request)
	if err != nil {
		return fmt.Errorf("RPC transport did not yield a receipt")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("RPC HTTP status %d", response.StatusCode)
	}
	var envelope struct {
		ID     string          `json:"id"`
		Result json.RawMessage `json:"result"`
		Error  json.RawMessage `json:"error"`
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 65537))
	if err != nil || len(data) > 65536 {
		return fmt.Errorf("RPC receipt unreadable or oversized")
	}
	if err := json.Unmarshal(data, &envelope); err != nil || envelope.ID != id || (len(envelope.Error) != 0 && string(envelope.Error) != "null") {
		return fmt.Errorf("RPC did not return a matching success receipt")
	}
	if err := json.Unmarshal(envelope.Result, result); err != nil {
		return fmt.Errorf("RPC result could not be decoded")
	}
	return nil
}
