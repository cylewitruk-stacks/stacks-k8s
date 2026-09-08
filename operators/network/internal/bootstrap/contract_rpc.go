package bootstrap

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/environment"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/stackssdk"
)

// clarityArgument specifies a value to the offline serializer; it contains no observation logic.
type clarityArgument = stackssdk.Argument

// cv supplies a scalar explicitly typed Clarity value.
func cv(kind, value string) clarityArgument { return clarityArgument{Type: kind, Value: value} }

// offline signs a fully specified transaction through the isolated SDK adapter.
func (s *session) offline(ctx context.Context, script string, input any) (signedTransaction, error) {
	return stackssdk.Sign(ctx, s.options.SDKDirectory, script, input)
}

// contractCall signs an explicit call with a current, exclusively owned sender nonce.
func (s *session) contractCall(ctx context.Context, sender environment.AccountKey, contract, method string, args []clarityArgument, allowLock bool) (signedTransaction, error) {
	parts := strings.Split(contract, ".")
	if len(parts) != 2 {
		return signedTransaction{}, fmt.Errorf("invalid contract principal")
	}
	account, err := s.account(ctx, sender.Address)
	if err != nil {
		return signedTransaction{}, err
	}
	return s.offline(ctx, "contract-sign.mjs", map[string]any{"operation": "call", "account": sender, "fee": poxFee, "nonce": strconv.FormatInt(account.Nonce, 10), "contractAddress": parts[0], "contractName": parts[1], "functionName": method, "arguments": args, "allowSTXLock": allowLock})
}

// execute records authorization before the sole send and then requires exact canonical successful execution.
func (s *session) execute(ctx context.Context, tx signedTransaction, operation string) error {
	if err := s.record("TransactionAuthorized", map[string]any{"operation": operation, "txid": tx.TxID}); err != nil {
		return err
	}
	if err := s.submit(ctx, tx); err != nil {
		return err
	}
	if err := s.record("TransactionSubmitted", map[string]any{"operation": operation, "txid": tx.TxID}); err != nil {
		return err
	}
	var receipt executionReceipt
	err := wait(ctx, "exact transaction execution", 180, func() bool {
		var err error
		receipt, err = s.execution(ctx, tx.TxID)
		return err == nil && receipt.Found
	})
	if err != nil {
		return fmt.Errorf("execution unconfirmed for recorded TxID: %w", err)
	}
	if err = s.record("TransactionExecuted", map[string]any{"operation": operation, "txid": tx.TxID, "blockID": receipt.BlockID, "success": receipt.Success}); err != nil {
		return err
	}
	if !receipt.Success {
		return fmt.Errorf("recorded transaction executed unsuccessfully; inspect TxID %s", tx.TxID)
	}
	return nil
}

// executionReceipt retains exact canonical execution evidence, without promising permanent finality.
type executionReceipt struct {
	Found, Success bool
	BlockID        string
}

// execution validates canonical membership against the signed transaction's byte identity.
func (s *session) execution(ctx context.Context, id string) (executionReceipt, error) {
	var wire struct {
		Bytes     string `json:"tx"`
		Result    string `json:"result"`
		BlockID   string `json:"index_block_hash"`
		Canonical bool   `json:"is_canonical"`
	}
	if err := s.stacks(ctx, "/v3/transaction/"+id, &wire); err != nil {
		return executionReceipt{}, err
	}
	if !validTransaction(signedTransaction{TxID: id, Bytes: strings.TrimPrefix(wire.Bytes, "0x")}) {
		return executionReceipt{}, fmt.Errorf("execution transaction identity mismatch")
	}
	block, err := hex.DecodeString(wire.BlockID)
	if err != nil || len(block) != 32 {
		return executionReceipt{}, fmt.Errorf("execution block identity unavailable")
	}
	if (!strings.HasPrefix(wire.Result, "(ok ") && !strings.HasPrefix(wire.Result, "(err ")) || !strings.HasSuffix(wire.Result, ")") {
		return executionReceipt{}, fmt.Errorf("execution result unavailable")
	}
	if !wire.Canonical {
		return executionReceipt{}, nil
	}
	return executionReceipt{Found: true, Success: strings.HasPrefix(wire.Result, "(ok "), BlockID: wire.BlockID}, nil
}

// publish confirms both exact successful execution and the expected source bytes.
func (s *session) publish(ctx context.Context, sender environment.AccountKey, contract contractSource) error {
	account, err := s.account(ctx, sender.Address)
	if err != nil {
		return err
	}
	tx, err := s.offline(ctx, "contract-sign.mjs", map[string]any{"operation": "publish", "account": sender, "nonce": strconv.FormatInt(account.Nonce, 10), "fee": strconv.Itoa(len(contract.Source)*10 + 3000), "contractName": contract.Name, "source": contract.Source, "clarityVersion": contract.ClarityVersion})
	if err != nil {
		return err
	}
	if err = s.record("ContractPublication", map[string]any{"contract": sender.Address + "." + contract.Name, "sourceDigest": environment.Digest(contract.Source), "clarityVersion": contract.ClarityVersion}); err != nil {
		return err
	}
	if err = s.execute(ctx, tx, "publish "+sender.Address+"."+contract.Name); err != nil {
		return err
	}
	var observed struct {
		Source string `json:"source"`
	}
	if err = s.stacks(ctx, "/v2/contracts/source/"+sender.Address+"/"+contract.Name+"?proof=0", &observed); err != nil {
		return err
	}
	if observed.Source != contract.Source {
		return fmt.Errorf("deployed contract source mismatch")
	}
	return nil
}

// readOnly obtains a bounded serialized Clarity result without exposing raw response errors.
func (s *session) readOnly(ctx context.Context, sender, contract, method string, args []string) (string, error) {
	parts := strings.Split(contract, ".")
	if len(parts) != 2 {
		return "", fmt.Errorf("invalid read-only contract")
	}
	if args == nil {
		args = []string{}
	}
	body, _ := json.Marshal(map[string]any{"sender": sender, "arguments": args})
	data, err := s.request(ctx, "POST", "http://127.0.0.1:"+strconv.Itoa(s.options.StacksPort)+"/v2/contracts/call-read/"+parts[0]+"/"+parts[1]+"/"+method, body, "", "", "application/json", 5)
	if err != nil {
		return "", err
	}
	var result struct {
		Okay   bool   `json:"okay"`
		Result string `json:"result"`
	}
	if json.Unmarshal(data, &result) != nil || !result.Okay || !strings.HasPrefix(result.Result, "0x") {
		return "", fmt.Errorf("invalid read-only result")
	}
	return result.Result, nil
}
