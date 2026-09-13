// Package rpc exposes bounded native node RPC without network policy or retries.
package rpc

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/cylewitruk-stacks/stacks-k8s/libs/stacks/clarity"
	"github.com/cylewitruk-stacks/stacks-k8s/libs/stacks/identity"
	"github.com/cylewitruk-stacks/stacks-k8s/libs/stacks/transaction"
)

// Config sets transport bounds explicitly. Redirects and implicit retries are disabled.
type Config struct {
	// Endpoint is an HTTP(S) node origin without credentials, query or fragment.
	Endpoint string
	// Timeout bounds the entire request, including body reads.
	Timeout time.Duration
	// MaxResponseBytes bounds every native response.
	MaxResponseBytes int64
}

// Client owns a scoped HTTP transport; callers choose network and authorization policy.
type Client struct {
	// endpoint is the validated origin.
	endpoint string
	// http enforces request and transport bounds.
	http *http.Client
	// maxBytes bounds each response.
	maxBytes int64
}

// New validates explicit bounds and disables proxy discovery and connection reuse.
func New(c Config) (*Client, error) {
	u, e := url.Parse(c.Endpoint)
	if e != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" && u.Path != "/" || c.Timeout <= 0 || c.MaxResponseBytes < 1 || c.MaxResponseBytes > 64<<20 {
		return nil, errors.New("invalid native RPC configuration")
	}
	return &Client{endpoint: strings.TrimSuffix(c.Endpoint, "/"), maxBytes: c.MaxResponseBytes, http: &http.Client{Timeout: c.Timeout, Transport: &http.Transport{Proxy: nil, DialContext: (&net.Dialer{Timeout: c.Timeout}).DialContext, DisableKeepAlives: true}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}

// request sends exactly once and does not expose raw response bodies in errors.
func (c *Client) request(ctx context.Context, method, path, contentType string, body []byte) (int, []byte, error) {
	var input io.Reader
	if body != nil {
		input = io.NopCloser(bytes.NewReader(body))
	}
	req, e := http.NewRequestWithContext(ctx, method, c.endpoint+path, input)
	if e != nil {
		return 0, nil, errors.New("invalid native RPC request")
	}
	if body != nil {
		req.ContentLength = int64(len(body))
	}
	req.Header.Set("Content-Type", contentType)
	response, e := c.http.Do(req)
	if e != nil {
		return 0, nil, errors.New("native RPC transport unavailable")
	}
	defer response.Body.Close()
	data, e := io.ReadAll(io.LimitReader(response.Body, c.maxBytes+1))
	if e != nil || int64(len(data)) > c.maxBytes {
		return response.StatusCode, nil, errors.New("native RPC response unreadable or oversized")
	}
	return response.StatusCode, data, nil
}

// get decodes one successful native observation.
func (c *Client) get(ctx context.Context, path string, result any) error {
	code, data, e := c.request(ctx, http.MethodGet, path, "application/json", nil)
	if e != nil {
		return e
	}
	if code != http.StatusOK || json.Unmarshal(data, result) != nil {
		return fmt.Errorf("native RPC observation unavailable (HTTP %d)", code)
	}
	return nil
}

// Info contains native node chain identity and processed heights.
type Info struct {
	// NetworkID is the node-reported chain identifier; callers enforce their expected value.
	NetworkID uint32
	// BurnHeight is the processed Bitcoin height.
	BurnHeight uint64
	// StacksHeight is the canonical Stacks height.
	StacksHeight uint64
	// Tip identifies the canonical Stacks tip.
	Tip string
}

// Info observes complete chain identity without imposing testnet policy.
func (c *Client) Info(ctx context.Context) (Info, error) {
	var wire struct {
		NetworkID *uint32 `json:"network_id"`
		Burn      *uint64 `json:"burn_block_height"`
		Stacks    *uint64 `json:"stacks_tip_height"`
		Tip       string  `json:"stacks_tip"`
	}
	if e := c.get(ctx, "/v2/info", &wire); e != nil {
		return Info{}, e
	}
	if wire.NetworkID == nil || wire.Burn == nil || wire.Stacks == nil || !validHash(wire.Tip) {
		return Info{}, errors.New("incomplete chain identity")
	}
	return Info{*wire.NetworkID, *wire.Burn, *wire.Stacks, wire.Tip}, nil
}

// Account contains canonical nonce and full-width uint128 balances.
type Account struct {
	// Nonce is the next canonical uint64 nonce.
	Nonce uint64
	// Balance is the native uint128 micro-STX balance.
	Balance clarity.Value
	// Locked is the uint128 micro-STX lock amount.
	Locked clarity.Value
	// UnlockHeight is the Bitcoin lock horizon.
	UnlockHeight uint64
}

// Account validates identity and all required account fields.
func (c *Client) Account(ctx context.Context, address string) (Account, error) {
	return c.account(ctx, address, "")
}

// AccountAt pins account state to one explicit canonical index block ID.
func (c *Client) AccountAt(ctx context.Context, address, indexBlockID string) (Account, error) {
	if !validHash(indexBlockID) {
		return Account{}, errors.New("invalid canonical tip")
	}
	return c.account(ctx, address, "&tip="+indexBlockID)
}

// account decodes the native state at the selected tip.
func (c *Client) account(ctx context.Context, address, tipQuery string) (Account, error) {
	if _, _, e := identity.DecodeAddress(address); e != nil {
		return Account{}, e
	}
	var wire struct {
		Nonce   *uint64 `json:"nonce"`
		Balance string  `json:"balance"`
		Locked  string  `json:"locked"`
		Unlock  *uint64 `json:"unlock_height"`
	}
	if e := c.get(ctx, "/v2/accounts/"+address+"?proof=0"+tipQuery, &wire); e != nil {
		return Account{}, e
	}
	balance, e := hexUint128(wire.Balance)
	locked, le := hexUint128(wire.Locked)
	if e != nil || le != nil || wire.Nonce == nil || wire.Unlock == nil {
		return Account{}, errors.New("incomplete account observation")
	}
	return Account{*wire.Nonce, balance, locked, *wire.Unlock}, nil
}

// PoX contains required enrollment timing and threshold observations.
type PoX struct {
	// Contract is the active boot contract principal.
	Contract string
	// BurnHeight is the processed Bitcoin height.
	BurnHeight uint64
	// RewardCycle is the current cycle identifier.
	RewardCycle uint64
	// CycleLength is the complete cycle length in Bitcoin blocks.
	CycleLength uint64
	// MinThreshold is the next-cycle uint128 micro-STX threshold.
	MinThreshold clarity.Value
	// BlocksUntilPrepare is the signed distance to the prepare boundary.
	BlocksUntilPrepare int64
}

// PoX rejects incomplete timing responses without choosing enrollment policy.
func (c *Client) PoX(ctx context.Context) (PoX, error) { return c.pox(ctx, "") }

// PoXAt pins the native PoX view to one explicit canonical index block ID.
func (c *Client) PoXAt(ctx context.Context, indexBlockID string) (PoX, error) {
	if !validHash(indexBlockID) {
		return PoX{}, errors.New("invalid canonical tip")
	}
	return c.pox(ctx, "?tip="+indexBlockID)
}

// pox decodes native timing without selecting enrollment policy.
func (c *Client) pox(ctx context.Context, tipQuery string) (PoX, error) {
	var wire struct {
		Contract string  `json:"contract_id"`
		Burn     *uint64 `json:"current_burnchain_block_height"`
		Cycle    *uint64 `json:"reward_cycle_id"`
		Length   *uint64 `json:"reward_cycle_length"`
		Next     struct {
			Threshold json.Number `json:"min_threshold_ustx"`
			Prepare   *int64      `json:"blocks_until_prepare_phase"`
		} `json:"next_cycle"`
	}
	if e := c.get(ctx, "/v2/pox"+tipQuery, &wire); e != nil {
		return PoX{}, e
	}
	contract, e := clarity.Principal(wire.Contract)
	threshold, te := clarity.Uint128(wire.Next.Threshold.String())
	if e != nil || contract.Type != clarity.ContractPrincipal || te != nil || wire.Burn == nil || wire.Cycle == nil || wire.Length == nil || *wire.Length == 0 || wire.Next.Prepare == nil {
		return PoX{}, errors.New("incomplete PoX observation")
	}
	return PoX{wire.Contract, *wire.Burn, *wire.Cycle, *wire.Length, threshold, *wire.Next.Prepare}, nil
}

// DataVariable decodes a complete serialized contract variable.
func (c *Client) DataVariable(ctx context.Context, address, contract, name string) (clarity.Value, error) {
	if e := validateContract(address, contract, name); e != nil {
		return clarity.Value{}, e
	}
	var wire struct {
		Data string `json:"data"`
	}
	if e := c.get(ctx, "/v2/data_var/"+address+"/"+contract+"/"+url.PathEscape(name)+"?proof=0", &wire); e != nil {
		return clarity.Value{}, e
	}
	return decodeCV(wire.Data)
}

// Source observes exact published source; 404 is distinct from a failed read.
func (c *Client) Source(ctx context.Context, address, contract string) (string, bool, error) {
	return c.source(ctx, address, contract, "")
}

// SourceAt pins exact source observation to one canonical index block ID.
func (c *Client) SourceAt(ctx context.Context, address, contract, indexBlockID string) (string, bool, error) {
	if !validHash(indexBlockID) {
		return "", false, errors.New("invalid canonical tip")
	}
	return c.source(ctx, address, contract, "&tip="+indexBlockID)
}

// source distinguishes absent contracts from failed reads at the selected tip.
func (c *Client) source(ctx context.Context, address, contract, tipQuery string) (string, bool, error) {
	if e := validateContract(address, contract, "source"); e != nil {
		return "", false, e
	}
	code, data, e := c.request(ctx, http.MethodGet, "/v2/contracts/source/"+address+"/"+contract+"?proof=0"+tipQuery, "application/json", nil)
	if e != nil {
		return "", false, e
	}
	if code == http.StatusNotFound {
		if tipQuery != "" && strings.TrimSpace(string(data)) != nativeSourceAbsent {
			return "", false, errors.New("pinned contract source observation unavailable")
		}
		return "", false, nil
	}
	var wire struct {
		Source *string `json:"source"`
	}
	if code != http.StatusOK || json.Unmarshal(data, &wire) != nil || wire.Source == nil || *wire.Source == "" {
		return "", false, errors.New("contract source observation unavailable")
	}
	return *wire.Source, true, nil
}

// ReadOnly sends explicitly encoded arguments once and decodes the returned value.
func (c *Client) ReadOnly(ctx context.Context, sender, address, contract, method string, args []clarity.Value) (clarity.Value, error) {
	return c.readOnly(ctx, sender, address, contract, method, args, "")
}

// ReadOnlyAt pins a native read-only contract call to one canonical index block ID.
func (c *Client) ReadOnlyAt(ctx context.Context, indexBlockID, sender, address, contract, method string, args []clarity.Value) (clarity.Value, error) {
	if !validHash(indexBlockID) {
		return clarity.Value{}, errors.New("invalid canonical tip")
	}
	return c.readOnly(ctx, sender, address, contract, method, args, "?tip="+indexBlockID)
}

// readOnly sends one bounded encoded call at the selected tip.
func (c *Client) readOnly(ctx context.Context, sender, address, contract, method string, args []clarity.Value, tipQuery string) (clarity.Value, error) {
	if _, e := clarity.Principal(sender); e != nil {
		return clarity.Value{}, e
	}
	if e := validateContract(address, contract, method); e != nil {
		return clarity.Value{}, e
	}
	if len(args) > 1024 {
		return clarity.Value{}, errors.New("too many arguments")
	}
	encoded := make([]string, len(args))
	total := 0
	for i, v := range args {
		raw, e := clarity.Encode(v)
		if e != nil {
			return clarity.Value{}, e
		}
		total += len(raw)
		if total > 1<<20 {
			return clarity.Value{}, errors.New("arguments too large")
		}
		encoded[i] = "0x" + hex.EncodeToString(raw)
	}
	body, _ := json.Marshal(map[string]any{"sender": sender, "arguments": encoded})
	code, data, e := c.request(ctx, http.MethodPost, "/v2/contracts/call-read/"+address+"/"+contract+"/"+url.PathEscape(method)+tipQuery, "application/json", body)
	if e != nil {
		return clarity.Value{}, e
	}
	var wire struct {
		Okay   bool   `json:"okay"`
		Result string `json:"result"`
	}
	if code != http.StatusOK || json.Unmarshal(data, &wire) != nil || !wire.Okay {
		return clarity.Value{}, errors.New("contract read-only result unavailable")
	}
	return decodeCV(wire.Result)
}

// Submit transmits once and requires acknowledgement of the exact precomputed TxID.
// A SubmissionRejection reports a matched native rejection; other errors leave delivery uncertain.
// Even a matched rejection does not establish permanent non-execution.
func (c *Client) Submit(ctx context.Context, tx transaction.Transaction) error {
	if !transaction.Valid(tx) {
		return errors.New("invalid transaction identity")
	}
	code, data, e := c.request(ctx, http.MethodPost, "/v2/transactions", "application/octet-stream", tx.Bytes)
	if e == nil {
		if rejection := ClassifySubmissionRejection(code, data, tx.TxID); rejection != nil {
			return rejection
		}
	}
	var id string
	if e != nil || code != http.StatusOK || json.Unmarshal(data, &id) != nil || strings.TrimPrefix(id, "0x") != tx.TxID {
		return errors.New("submission did not acknowledge the exact transaction")
	}
	return nil
}

// Inclusion retains exact canonical execution evidence, without finality claims.
type Inclusion struct {
	// Found indicates the exact transaction is observed in a canonical block.
	Found bool
	// Success reflects the native response result.
	Success bool
	// BlockID identifies that observed canonical block.
	BlockID string
}

// Inclusion verifies the transaction bytes, canonical flag and execution result.
func (c *Client) Inclusion(ctx context.Context, id string) (Inclusion, error) {
	if !validHash(id) {
		return Inclusion{}, errors.New("invalid transaction ID")
	}
	code, data, e := c.request(ctx, http.MethodGet, "/v3/transaction/"+id, "application/json", nil)
	if e != nil {
		return Inclusion{}, e
	}
	if code == http.StatusNotFound {
		return Inclusion{}, nil
	}
	var wire struct {
		Bytes     string `json:"tx"`
		Result    string `json:"result"`
		Block     string `json:"index_block_hash"`
		Canonical *bool  `json:"is_canonical"`
	}
	if code != http.StatusOK || json.Unmarshal(data, &wire) != nil {
		return Inclusion{}, errors.New("execution unavailable")
	}
	raw, e := hex.DecodeString(strings.TrimPrefix(wire.Bytes, "0x"))
	if e != nil || !transaction.Valid(transaction.Transaction{Bytes: raw, TxID: id}) || !validHash(wire.Block) || wire.Canonical == nil || (!strings.HasPrefix(wire.Result, "(ok ") && !strings.HasPrefix(wire.Result, "(err ")) || !strings.HasSuffix(wire.Result, ")") {
		return Inclusion{}, errors.New("exact execution identity unavailable")
	}
	if !*wire.Canonical {
		return Inclusion{}, nil
	}
	return Inclusion{true, strings.HasPrefix(wire.Result, "(ok "), wire.Block}, nil
}

// validateContract validates all interpolated native RPC path segments.
func validateContract(address, contract, name string) error {
	if _, _, e := identity.DecodeAddress(address); e != nil {
		return e
	}
	if !clarity.ValidContractName(contract) || !clarity.ValidName(name) {
		return errors.New("invalid contract path")
	}
	return nil
}

// validHash requires canonical unprefixed hex identity.
func validHash(s string) bool {
	raw, e := hex.DecodeString(s)
	return e == nil && len(raw) == 32 && s == strings.ToLower(s)
}

// decodeCV requires the native RPC hex prefix and a complete bounded consensus value.
func decodeCV(s string) (clarity.Value, error) {
	if !strings.HasPrefix(s, "0x") || len(s) > 2+(2<<20) {
		return clarity.Value{}, errors.New("invalid Clarity response")
	}
	raw, e := hex.DecodeString(s[2:])
	if e != nil {
		return clarity.Value{}, errors.New("invalid Clarity response")
	}
	return clarity.Decode(raw)
}

// hexUint128 parses the native fixed-width or minimal hexadecimal balance encoding.
func hexUint128(s string) (clarity.Value, error) {
	if !strings.HasPrefix(s, "0x") || len(s) < 3 || len(s) > 34 {
		return clarity.Value{}, errors.New("invalid uint128 balance")
	}
	for _, c := range s[2:] {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
			return clarity.Value{}, errors.New("invalid balance digit")
		}
	}
	raw := s[2:]
	if len(raw)%2 != 0 {
		raw = "0" + raw
	}
	decoded, _ := hex.DecodeString(raw)
	bytes := make([]byte, 17)
	bytes[0] = byte(clarity.UInt)
	copy(bytes[17-len(decoded):], decoded)
	return clarity.Decode(bytes)
}

// nativeSourceAbsent is the native tip-bound contract-source absence response.
const nativeSourceAbsent = "No contract source data found"
