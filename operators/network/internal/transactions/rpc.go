// Package transactions maintains isolated, aggregate-owned STX transfer demand.
package transactions

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
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/stacksrpc"

	stacksv1alpha1 "github.com/cylewitruk-stacks/stacks-k8s/apis/network/stacks/v1alpha1"
)

// AccountProfile binds the mounted account to one network and approved ingress configuration.
type AccountProfile struct {
	// NetworkName is the only parent this worker may serve.
	NetworkName string `json:"networkName"`
	// Sender is the exclusive account address; actors and enrollment use different keys.
	Sender string `json:"sender"`
	// ConfigDigest identifies the administrator-approved ingress configuration.
	ConfigDigest string `json:"configDigest"`
}

// SignedTransfer contains public transaction identity and bounded wire bytes.
type SignedTransfer struct {
	// TxID hashes the signed bytes before submission.
	TxID string `json:"txid"`
	// Bytes is the hex-encoded signed transaction.
	Bytes string `json:"bytes"`
}

// Signer signs only explicit transfer parameters; it never sends transactions.
type Signer interface {
	Sign(context.Context, stacksv1alpha1.TransferPolicy, int64) (SignedTransfer, error)
}

// LocalSigner invokes the locked official SDK in the isolated transaction-worker image.
type LocalSigner struct {
	// AccountFile names the administrator-mounted immutable credential file.
	AccountFile string
	// Script names the packaged offline signer.
	Script string
}

// Sign builds one testnet transfer without fee discovery, nonce discovery, or network access.
func (s LocalSigner) Sign(ctx context.Context, policy stacksv1alpha1.TransferPolicy, nonce int64) (SignedTransfer, error) {
	var result SignedTransfer
	input, _ := json.Marshal(map[string]string{"sender": policy.Sender, "recipient": policy.Recipient, "nonce": strconv.FormatInt(nonce, 10), "amountMicroSTX": strconv.FormatInt(policy.AmountMicroSTX, 10), "feeMicroSTX": strconv.FormatInt(policy.FeeMicroSTX, 10)})
	command := exec.CommandContext(ctx, "node", s.Script, s.AccountFile)
	command.Stdin = bytes.NewReader(input)
	output, err := command.Output()
	if err != nil || len(output) > 4096 {
		return result, fmt.Errorf("offline transfer signing failed")
	}
	if json.Unmarshal(output, &result) != nil || !validTransaction(result) {
		return SignedTransfer{}, fmt.Errorf("invalid signed transfer identity")
	}
	return result, nil
}

// validTransaction verifies the bounded wire transaction's SHA512/256 identity.
func validTransaction(tx SignedTransfer) bool {
	raw, err := hex.DecodeString(tx.Bytes)
	if err != nil || len(raw) < 100 || len(raw) > 1024 || len(tx.TxID) != 64 {
		return false
	}
	sum := sha512.Sum512_256(raw)
	return hex.EncodeToString(sum[:]) == tx.TxID
}

// AccountState reports canonical nonce and spendable balance from the selected ingress.
type AccountState struct {
	// Nonce is the next canonical account nonce.
	Nonce int64
	// Balance is the available micro-STX balance.
	Balance uint64
}

// Inclusion is exact transaction evidence, not a permanent finality assertion.
type Inclusion struct {
	// Found reports observed canonical membership.
	Found bool
	// Success reports the exact transfer execution result.
	Success bool
	// BlockID identifies the inclusion block at observation.
	BlockID string
}

// RPC separates read-only admission/evidence from the single submission attempt.
type RPC interface {
	Account(context.Context, string, string) (AccountState, error)
	Submit(context.Context, string, SignedTransfer) error
	Inclusion(context.Context, string, string) (Inclusion, error)
}

// NodeRPC uses native Core endpoints without a sidecar indexer or transport replay.
type NodeRPC struct{ client *http.Client }

// NewNodeRPC disables redirects, proxy discovery, and connection reuse.
func NewNodeRPC() *NodeRPC {
	return &NodeRPC{client: &http.Client{Transport: &http.Transport{Proxy: nil, DialContext: (&net.Dialer{Timeout: 3 * time.Second}).DialContext, DisableKeepAlives: true}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

// request performs one bounded-memory request; callers bound its duration.
func (r *NodeRPC) request(ctx context.Context, method, url string, body []byte) (int, []byte, error) {
	var payload io.Reader
	if body != nil {
		payload = io.NopCloser(bytes.NewReader(body))
	}
	req, err := http.NewRequestWithContext(ctx, method, url, payload)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	if body != nil {
		req.ContentLength = int64(len(body))
	}
	response, err := r.client.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("Stacks RPC transport unavailable")
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 65537))
	if err != nil || len(data) > 65536 {
		return response.StatusCode, nil, fmt.Errorf("invalid Stacks RPC response")
	}
	return response.StatusCode, data, nil
}

// Account verifies testnet chain identity and reads its canonical account state.
func (r *NodeRPC) Account(ctx context.Context, endpoint, sender string) (AccountState, error) {
	var result AccountState
	code, data, err := r.request(ctx, "GET", endpoint+"/v2/info", nil)
	var info struct {
		NetworkID uint32 `json:"network_id"`
	}
	if err != nil || code != 200 || json.Unmarshal(data, &info) != nil || info.NetworkID != 0x80000000 {
		return result, fmt.Errorf("ingress does not report the approved testnet identity")
	}
	code, _, err = r.request(ctx, "GET", endpoint+"/v3/transaction/"+strings.Repeat("0", 64), nil)
	if err != nil || code != http.StatusNotFound {
		return result, fmt.Errorf("native transaction indexing must be enabled before production")
	}
	code, data, err = r.request(ctx, "GET", endpoint+"/v2/accounts/"+sender+"?proof=0", nil)
	var account struct {
		Nonce   *int64 `json:"nonce"`
		Balance string `json:"balance"`
		Locked  string `json:"locked"`
	}
	if err != nil || code != 200 || json.Unmarshal(data, &account) != nil || account.Nonce == nil || *account.Nonce < 0 {
		return result, fmt.Errorf("account state is unavailable")
	}
	balance, err := strconv.ParseUint(strings.TrimPrefix(account.Balance, "0x"), 16, 64)
	if err != nil {
		return result, fmt.Errorf("account balance exceeds the initial profile")
	}
	result.Nonce, result.Balance = *account.Nonce, balance
	return result, nil
}

// submissionRejection contains only a bounded class from a matched native rejection response.
type submissionRejection struct {
	// reason is FeeTooLow, BadNonce, or Other; raw server detail is discarded.
	reason string
}

// Error describes this ingress response without claiming permanent non-execution.
func (r *submissionRejection) Error() string { return "ingress rejected submission: " + r.reason }

// Submit makes a single non-replaying POST and requires the exact locally computed TxID.
func (r *NodeRPC) Submit(ctx context.Context, endpoint string, tx SignedTransfer) error {
	raw, _ := hex.DecodeString(tx.Bytes)
	code, data, err := r.request(ctx, "POST", endpoint+"/v2/transactions", raw)
	if err == nil {
		if reason := stacksrpc.RejectionReason(code, data, tx.TxID); reason != "" {
			return &submissionRejection{reason: reason}
		}
	}
	var id string
	if err != nil || code != 200 || json.Unmarshal(data, &id) != nil || strings.TrimPrefix(id, "0x") != tx.TxID {
		return fmt.Errorf("submission did not acknowledge the exact transaction; observe before proceeding")
	}
	return nil
}

// Inclusion requires matching signed bytes, canonical membership and an exact successful result.
func (r *NodeRPC) Inclusion(ctx context.Context, endpoint, id string) (Inclusion, error) {
	code, data, err := r.request(ctx, "GET", endpoint+"/v3/transaction/"+id, nil)
	if err != nil {
		return Inclusion{}, err
	}
	if code == 404 {
		return Inclusion{}, nil
	}
	var tx struct {
		Bytes     string `json:"tx"`
		Result    string `json:"result"`
		BlockID   string `json:"index_block_hash"`
		Canonical bool   `json:"is_canonical"`
	}
	if code != 200 || json.Unmarshal(data, &tx) != nil || !validTransaction(SignedTransfer{TxID: id, Bytes: strings.TrimPrefix(tx.Bytes, "0x")}) || len(tx.BlockID) != 64 {
		return Inclusion{}, fmt.Errorf("exact transaction inclusion is unavailable")
	}
	if !tx.Canonical {
		return Inclusion{}, nil
	}
	return Inclusion{Found: true, Success: tx.Result == "(ok true)", BlockID: tx.BlockID}, nil
}

// Height observes a complete testnet burn height before delayed transfer admission.
func (r *NodeRPC) Height(ctx context.Context, endpoint string) (int64, error) {
	code, data, err := r.request(ctx, "GET", endpoint+"/v2/info", nil)
	var info struct {
		NetworkID uint32 `json:"network_id"`
		Height    *int64 `json:"burn_block_height"`
	}
	if err != nil || code != 200 || json.Unmarshal(data, &info) != nil || info.NetworkID != 0x80000000 || info.Height == nil || *info.Height < 0 {
		return 0, fmt.Errorf("testnet burn height unavailable")
	}
	return *info.Height, nil
}
