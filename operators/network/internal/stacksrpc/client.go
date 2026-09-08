package stacksrpc

import (
	"bytes"
	"context"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/stackstx"
)

// Client observes native Stacks state and submits a transaction only when explicitly called.
type Client struct{ http *http.Client }

// NewClient disables redirects, proxy discovery and connection reuse.
func NewClient() *Client {
	return &Client{http: &http.Client{Transport: &http.Transport{Proxy: nil, DialContext: (&net.Dialer{Timeout: 3 * time.Second}).DialContext, DisableKeepAlives: true},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

// DataVariable observes a serialized contract variable without modifying it.
func (c *Client) DataVariable(ctx context.Context, endpoint, address, contract, name string) (string, error) {
	var wire struct {
		Data string `json:"data"`
	}
	if err := c.get(ctx, endpoint, "/v2/data_var/"+address+"/"+contract+"/"+name+"?proof=0", &wire); err != nil {
		return "", err
	}
	if !strings.HasPrefix(wire.Data, "0x") {
		return "", fmt.Errorf("contract variable observation unavailable")
	}
	if _, err := hex.DecodeString(strings.TrimPrefix(wire.Data, "0x")); err != nil {
		return "", fmt.Errorf("invalid contract variable serialization")
	}
	return wire.Data, nil
}

// Info is the node's processed-chain observation, not a block's tenure attribution.
type Info struct {
	// BurnHeight is the processed Bitcoin chain height.
	BurnHeight int64
	// StacksHeight is the observed canonical Stacks tip height.
	StacksHeight int64
	// Tip identifies the observed canonical Stacks tip.
	Tip string
}

// Account contains the canonical nonce, unlocked balance and current stacking lock.
type Account struct {
	// Nonce is the next canonical account nonce.
	Nonce int64
	// Balance is the account balance in micro-STX.
	Balance int64
	// Locked is the current locked amount in micro-STX.
	Locked int64
	// UnlockHeight is the current Bitcoin lock horizon.
	UnlockHeight int64
}

// PoX is the protocol observation used for direct enrollment and renewal.
type PoX struct {
	// Contract is the active PoX boot contract principal.
	Contract string
	// BurnHeight is the processed burn height for this observation.
	BurnHeight int64
	// RewardCycle is the current reward cycle identifier.
	RewardCycle int64
	// CycleLength counts Bitcoin blocks per complete reward cycle.
	CycleLength int64
	// MinThreshold is the observed next-cycle threshold in micro-STX.
	MinThreshold int64
	// BlocksUntilPrepare is the node's distance to the next prepare boundary.
	BlocksUntilPrepare int64
}

// request bounds response memory and leaves ambiguous delivery to the account ledger.
func (c *Client) request(ctx context.Context, method, url, contentType string, body []byte) (int, []byte, error) {
	var input io.Reader
	if body != nil {
		input = io.NopCloser(bytes.NewReader(body))
	}
	req, err := http.NewRequestWithContext(ctx, method, url, input)
	if err != nil {
		return 0, nil, fmt.Errorf("invalid native RPC request")
	}
	if body != nil {
		req.ContentLength = int64(len(body))
	}
	req.Header.Set("Content-Type", contentType)
	response, err := c.http.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("native RPC transport unavailable")
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 1024*1024+1))
	if err != nil || len(data) > 1024*1024 {
		return response.StatusCode, nil, fmt.Errorf("native RPC response unreadable or oversized")
	}
	return response.StatusCode, data, nil
}

// get decodes one successful native response without including raw response text in errors.
func (c *Client) get(ctx context.Context, endpoint, path string, result any) error {
	code, data, err := c.request(ctx, "GET", endpoint+path, "application/json", nil)
	if err != nil {
		return err
	}
	if code != http.StatusOK || json.Unmarshal(data, result) != nil {
		return fmt.Errorf("native RPC observation unavailable (HTTP %d)", code)
	}
	return nil
}

// Info verifies testnet identity and complete height observations.
func (c *Client) Info(ctx context.Context, endpoint string) (Info, error) {
	var wire struct {
		NetworkID uint32 `json:"network_id"`
		Burn      *int64 `json:"burn_block_height"`
		Stacks    *int64 `json:"stacks_tip_height"`
		Tip       string `json:"stacks_tip"`
	}
	err := c.get(ctx, endpoint, "/v2/info", &wire)
	if err != nil {
		return Info{}, err
	}
	if wire.NetworkID != 0x80000000 || wire.Burn == nil || *wire.Burn < 0 || wire.Stacks == nil || *wire.Stacks < 0 {
		return Info{}, fmt.Errorf("ingress does not report complete testnet chain identity")
	}
	return Info{BurnHeight: *wire.Burn, StacksHeight: *wire.Stacks, Tip: wire.Tip}, nil
}

// Account rejects incomplete or out-of-range account observations.
func (c *Client) Account(ctx context.Context, endpoint, address string) (Account, error) {
	var wire struct {
		Nonce        *int64 `json:"nonce"`
		Balance      string `json:"balance"`
		Locked       string `json:"locked"`
		UnlockHeight *int64 `json:"unlock_height"`
	}
	if err := c.get(ctx, endpoint, "/v2/accounts/"+address+"?proof=0", &wire); err != nil {
		return Account{}, err
	}
	balance, err := strconv.ParseInt(strings.TrimPrefix(wire.Balance, "0x"), 16, 64)
	locked, lockErr := strconv.ParseInt(strings.TrimPrefix(wire.Locked, "0x"), 16, 64)
	if err != nil || lockErr != nil || balance < 0 || locked < 0 || wire.Nonce == nil || *wire.Nonce < 0 || wire.UnlockHeight == nil || *wire.UnlockHeight < 0 {
		return Account{}, fmt.Errorf("incomplete account observation")
	}
	return Account{Nonce: *wire.Nonce, Balance: balance, Locked: locked, UnlockHeight: *wire.UnlockHeight}, nil
}

// Nonce verifies chain identity and returns a canonical account nonce for authorization.
func (c *Client) Nonce(ctx context.Context, endpoint, address string) (int64, error) {
	if _, err := c.Info(ctx, endpoint); err != nil {
		return 0, err
	}
	account, err := c.Account(ctx, endpoint, address)
	return account.Nonce, err
}

// PoX rejects missing timing fields before a controller selects a transaction.
func (c *Client) PoX(ctx context.Context, endpoint string) (PoX, error) {
	var wire struct {
		Contract string `json:"contract_id"`
		Burn     *int64 `json:"current_burnchain_block_height"`
		Cycle    *int64 `json:"reward_cycle_id"`
		Length   *int64 `json:"reward_cycle_length"`
		Next     struct {
			Threshold *int64 `json:"min_threshold_ustx"`
			Prepare   *int64 `json:"blocks_until_prepare_phase"`
		} `json:"next_cycle"`
	}
	if err := c.get(ctx, endpoint, "/v2/pox", &wire); err != nil {
		return PoX{}, err
	}
	if wire.Contract == "" || wire.Burn == nil || *wire.Burn < 0 || wire.Cycle == nil || *wire.Cycle < 0 || wire.Length == nil || *wire.Length < 1 || wire.Next.Threshold == nil || *wire.Next.Threshold < 0 || wire.Next.Prepare == nil {
		return PoX{}, fmt.Errorf("incomplete PoX observation")
	}
	return PoX{Contract: wire.Contract, BurnHeight: *wire.Burn, RewardCycle: *wire.Cycle, CycleLength: *wire.Length, MinThreshold: *wire.Next.Threshold, BlocksUntilPrepare: *wire.Next.Prepare}, nil
}

// SubmissionRejection is a bounded matched native rejection envelope.
type SubmissionRejection struct{ reason string }

// Error reports classification without raw response data.
func (r *SubmissionRejection) Error() string { return "ingress rejected submission: " + r.reason }

// Reason exposes the bounded classification to the account ledger.
func (r *SubmissionRejection) Reason() string { return r.reason }

// Submit sends once and requires the exact precomputed TxID acknowledgement.
func (c *Client) Submit(ctx context.Context, endpoint string, tx stackstx.Transaction) error {
	if !stackstx.Valid(tx) {
		return fmt.Errorf("invalid transaction identity")
	}
	raw, _ := hex.DecodeString(tx.Bytes)
	code, data, err := c.request(ctx, "POST", endpoint+"/v2/transactions", "application/octet-stream", raw)
	if err == nil {
		if reason := RejectionReason(code, data, tx.TxID); reason != "" {
			return &SubmissionRejection{reason: reason}
		}
	}
	var id string
	if err != nil || code != http.StatusOK || json.Unmarshal(data, &id) != nil || strings.TrimPrefix(id, "0x") != tx.TxID {
		return fmt.Errorf("submission did not acknowledge the exact transaction")
	}
	return nil
}

// Inclusion verifies consensus bytes, canonical membership and a response result.
func (c *Client) Inclusion(ctx context.Context, endpoint, id string) (stackstx.Inclusion, error) {
	code, data, err := c.request(ctx, "GET", endpoint+"/v3/transaction/"+id, "application/json", nil)
	if err != nil {
		return stackstx.Inclusion{}, err
	}
	if code == http.StatusNotFound {
		return stackstx.Inclusion{}, nil
	}
	var wire struct {
		Bytes     string `json:"tx"`
		Result    string `json:"result"`
		BlockID   string `json:"index_block_hash"`
		Canonical bool   `json:"is_canonical"`
	}
	if code != http.StatusOK || json.Unmarshal(data, &wire) != nil || !stackstx.Valid(stackstx.Transaction{TxID: id, Bytes: strings.TrimPrefix(wire.Bytes, "0x")}) {
		return stackstx.Inclusion{}, fmt.Errorf("exact execution identity unavailable")
	}
	block, err := hex.DecodeString(wire.BlockID)
	if err != nil || len(block) != 32 || (!strings.HasPrefix(wire.Result, "(ok ") && !strings.HasPrefix(wire.Result, "(err ")) || !strings.HasSuffix(wire.Result, ")") {
		return stackstx.Inclusion{}, fmt.Errorf("exact execution result unavailable")
	}
	if !wire.Canonical {
		return stackstx.Inclusion{}, nil
	}
	return stackstx.Inclusion{Found: true, Success: strings.HasPrefix(wire.Result, "(ok "), BlockID: wire.BlockID}, nil
}

// Source observes published contract bytes; absence is distinct from a failed read.
func (c *Client) Source(ctx context.Context, endpoint, address, name string) (string, bool, error) {
	code, data, err := c.request(ctx, "GET", endpoint+"/v2/contracts/source/"+address+"/"+name+"?proof=0", "application/json", nil)
	if err != nil {
		return "", false, err
	}
	if code == http.StatusNotFound {
		return "", false, nil
	}
	var wire struct {
		Source *string `json:"source"`
	}
	if code != http.StatusOK || json.Unmarshal(data, &wire) != nil || wire.Source == nil || *wire.Source == "" {
		return "", false, fmt.Errorf("contract source observation unavailable")
	}
	return *wire.Source, true, nil
}

// ReadOnly returns a bounded serialized Clarity result from an explicitly named function.
func (c *Client) ReadOnly(ctx context.Context, endpoint, sender, address, contract, method string, args []string) (string, error) {
	if args == nil {
		args = []string{}
	}
	body, err := json.Marshal(map[string]any{"sender": sender, "arguments": args})
	if err != nil {
		return "", err
	}
	code, data, err := c.request(ctx, "POST", endpoint+"/v2/contracts/call-read/"+address+"/"+contract+"/"+method, "application/json", body)
	if err != nil {
		return "", err
	}
	var wire struct {
		Okay   bool   `json:"okay"`
		Result string `json:"result"`
	}
	if code != http.StatusOK || json.Unmarshal(data, &wire) != nil || !wire.Okay || !strings.HasPrefix(wire.Result, "0x") {
		return "", fmt.Errorf("contract read-only result unavailable")
	}
	if _, err := hex.DecodeString(strings.TrimPrefix(wire.Result, "0x")); err != nil {
		return "", fmt.Errorf("invalid serialized Clarity result")
	}
	return wire.Result, nil
}

// LegacyCanonical verifies a recent legacy block against the canonical header response.
// The bounded window covers initial PoX-4 enrollment; an evidence gap never authorizes a resend.
func (c *Client) LegacyCanonical(ctx context.Context, endpoint, blockID string) (bool, error) {
	var headers []struct {
		Consensus string `json:"consensus_hash"`
		Header    string `json:"header"`
		Parent    string `json:"parent_block_id"`
	}
	if err := c.get(ctx, endpoint, "/v2/headers/64", &headers); err != nil {
		return false, err
	}
	if len(headers) == 0 || len(headers) > 64 {
		return false, fmt.Errorf("legacy header window unavailable")
	}
	previous := ""
	for _, header := range headers {
		raw, err := hex.DecodeString(header.Header)
		consensus, ce := hex.DecodeString(header.Consensus)
		if err != nil || ce != nil || len(raw) < 100 || len(raw) > 4096 || len(consensus) != 20 {
			return false, fmt.Errorf("malformed legacy header")
		}
		hash := sha512.Sum512_256(raw)
		id := sha512.Sum512_256(append(hash[:], consensus...))
		current := hex.EncodeToString(id[:])
		if previous != "" && previous != current {
			return false, fmt.Errorf("legacy header ancestry differs")
		}
		if current == blockID {
			return true, nil
		}
		previous = header.Parent
	}
	return false, nil
}
