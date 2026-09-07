package bootstrap

import (
	"bytes"
	"context"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/environment"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/stacksrpc"
)

// poxFee is the explicit micro-STX fee for external enrollment and renewal.
const poxFee = "3000"

const pox4 = "ST000000000000000000002AMW42H.pox-4"

// poxInfo contains the public observations used to authorize one PoX-4 transaction.
type poxInfo struct {
	Contract    string `json:"contract_id"`
	BurnHeight  int64  `json:"current_burnchain_block_height"`
	RewardCycle int64  `json:"reward_cycle_id"`
	CycleLength int64  `json:"reward_cycle_length"`
	NextCycle   struct {
		MinThreshold       int64 `json:"min_threshold_ustx"`
		BlocksUntilPrepare int64 `json:"blocks_until_prepare_phase"`
	} `json:"next_cycle"`
}

// accountState contains native account balances and its current nonce.
type accountState struct {
	Balance, Locked     int64
	Nonce, UnlockHeight int64
}

// account rejects missing or malformed account observations.
func (s *session) account(ctx context.Context, address string) (accountState, error) {
	var wire struct {
		Balance      string `json:"balance"`
		Locked       string `json:"locked"`
		Nonce        *int64 `json:"nonce"`
		UnlockHeight *int64 `json:"unlock_height"`
	}
	if err := s.stacks(ctx, "/v2/accounts/"+address+"?proof=0", &wire); err != nil {
		return accountState{}, err
	}
	balance, err := strconv.ParseInt(strings.TrimPrefix(wire.Balance, "0x"), 16, 64)
	locked, lockErr := strconv.ParseInt(strings.TrimPrefix(wire.Locked, "0x"), 16, 64)
	if err != nil || lockErr != nil || balance < 0 || locked < 0 || wire.Nonce == nil || *wire.Nonce < 0 || wire.UnlockHeight == nil || *wire.UnlockHeight < 0 {
		return accountState{}, fmt.Errorf("invalid account observation")
	}
	return accountState{Balance: balance, Locked: locked, Nonce: *wire.Nonce, UnlockHeight: *wire.UnlockHeight}, nil
}

// signedTransaction is one exact SDK-signed transaction, independently hash-checked in Go.
type signedTransaction struct {
	TxID  string `json:"txid"`
	Bytes string `json:"bytes"`
}

// sign requests offline encoding, with explicit nonce and protocol observations.
func (s *session) sign(ctx context.Context, setup environment.Bootstrap, operation string, pox poxInfo, account accountState, amount, cycles, authID int64) (signedTransaction, error) {
	if pox.Contract != pox4 || pox.BurnHeight < 0 || pox.RewardCycle < 0 || pox.CycleLength <= 0 {
		return signedTransaction{}, fmt.Errorf("unsupported PoX observation")
	}
	input := map[string]any{"fee": poxFee, "account": setup.Signer, "operation": operation, "contract": pox.Contract, "nonce": strconv.FormatInt(account.Nonce, 10), "burnHeight": strconv.FormatInt(pox.BurnHeight, 10), "rewardCycle": strconv.FormatInt(pox.RewardCycle, 10), "amount": strconv.FormatInt(amount, 10), "authID": strconv.FormatInt(authID, 10), "cycles": strconv.FormatInt(cycles, 10)}
	data, err := json.Marshal(input)
	if err != nil {
		return signedTransaction{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "node", filepath.Join(s.options.SDKDirectory, "pox-sign.mjs"))
	cmd.Stdin = bytes.NewReader(data)
	out, err := cmd.Output()
	if err != nil {
		return signedTransaction{}, fmt.Errorf("offline PoX signing failed")
	}
	var tx signedTransaction
	if json.Unmarshal(out, &tx) != nil || !validTransaction(tx) {
		return tx, fmt.Errorf("invalid signed PoX transaction")
	}
	return tx, nil
}

// validTransaction verifies the exact wire identity without trusting adapter output.
func validTransaction(tx signedTransaction) bool {
	raw, err := hex.DecodeString(tx.Bytes)
	if err != nil || len(raw) < 100 || len(raw) > 4096 {
		return false
	}
	sum := sha512.Sum512_256(raw)
	return tx.TxID == hex.EncodeToString(sum[:])
}

// submit makes exactly one attempt and requires acknowledgement of the reserved transaction ID.
func (s *session) submit(ctx context.Context, tx signedTransaction) error {
	if !validTransaction(tx) {
		return fmt.Errorf("invalid transaction")
	}
	raw, _ := hex.DecodeString(tx.Bytes)
	data, err := s.request(ctx, "POST", "http://127.0.0.1:"+strconv.Itoa(s.options.StacksPort)+"/v2/transactions", raw, "", "", "application/octet-stream", 30)
	if err != nil {
		var response *httpResponseError
		if errors.As(err, &response) {
			if reason := stacksrpc.RejectionReason(response.status, response.body, tx.TxID); reason != "" {
				return fmt.Errorf("ingress rejected submission: %s; inspect recorded TxID before further work", reason)
			}
		}
		return fmt.Errorf("transaction submission is unconfirmed; inspect recorded TxID before further work: %w", err)
	}
	var receipt string
	if json.Unmarshal(data, &receipt) != nil || strings.TrimPrefix(receipt, "0x") != tx.TxID {
		return fmt.Errorf("transaction submission is ambiguous; inspect recorded TxID before further work")
	}
	return nil
}
