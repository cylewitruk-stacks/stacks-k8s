// Package stackssdk invokes the repository's offline Stacks SDK adapters.
package stackssdk

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/stackstx"
)

// Key contains one role-specific private key and its verified public encodings.
type Key struct {
	// PrivateKey is the compressed signing seed; callers must never publish it.
	PrivateKey string `json:"privateKey"`
	// Address is the testnet spending principal.
	Address string `json:"address"`
	// PublicKey is the compressed secp256k1 key.
	PublicKey string `json:"publicKey"`
	// PoXAddress is the public Bitcoin reward address.
	PoXAddress string `json:"poxAddress"`
}

// Adapter binds the packaged offline encoders to a directory without owning any network operation.
type Adapter struct {
	// Directory contains the locked SDK package and its adapters.
	Directory string
}

// Sign encodes one explicit transaction offline.
func (a Adapter) Sign(ctx context.Context, script string, input any) (stackstx.Transaction, error) {
	return Sign(ctx, a.Directory, script, input)
}

// VerifyKey checks public encodings without granting submission authority.
func (a Adapter) VerifyKey(ctx context.Context, key Key) error {
	return VerifyKey(ctx, a.Directory, key)
}

// EncodeArguments serializes public query inputs without performing the query.
func (a Adapter) EncodeArguments(ctx context.Context, arguments []Argument) ([]string, error) {
	return EncodeArguments(ctx, a.Directory, arguments)
}

// Argument is an explicit input to the offline Clarity serializer.
type Argument struct {
	// Type selects the bounded supported Clarity encoding.
	Type string `json:"type"`
	// Value supplies a scalar value.
	Value string `json:"value,omitempty"`
	// Items contains list arguments.
	Items []Argument `json:"items,omitempty"`
	// Manager binds an offline signer-grant authorization.
	Manager string `json:"manager,omitempty"`
	// AuthID is the explicit signer-grant identifier.
	AuthID string `json:"authID,omitempty"`
	// PrivateKey is used only for an offline signer-grant signature.
	PrivateKey string `json:"privateKey,omitempty"`
}

// VerifyKey independently derives public encodings before granting a supplied key signing authority.
func VerifyKey(ctx context.Context, directory string, key Key) error {
	seed := key.PrivateKey
	if len(seed) == 66 && strings.HasSuffix(seed, "01") {
		seed = strings.TrimSuffix(seed, "01")
	}
	if len(seed) != 64 {
		return fmt.Errorf("invalid signing key encoding")
	}
	input, _ := json.Marshal([]string{seed})
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "node", filepath.Join(directory, "keys.mjs"))
	command.Stdin = bytes.NewReader(input)
	var output boundedOutput
	command.Stdout = &output
	if command.Run() != nil {
		return fmt.Errorf("offline key verification failed")
	}
	var encoded []struct {
		Address        string `json:"address"`
		PublicKey      string `json:"publicKey"`
		BitcoinAddress string `json:"bitcoinAddress"`
	}
	if json.Unmarshal(output.Bytes(), &encoded) != nil || len(encoded) != 1 || encoded[0].Address != key.Address || encoded[0].PublicKey != key.PublicKey || encoded[0].BitcoinAddress != key.PoXAddress {
		return fmt.Errorf("signing key does not match declared public identity")
	}
	return nil
}

// Sign runs a known offline adapter with explicit nonce, fee and signing inputs.
func Sign(ctx context.Context, directory, script string, input any) (stackstx.Transaction, error) {
	if script != "pox-sign.mjs" && script != "contract-sign.mjs" {
		return stackstx.Transaction{}, fmt.Errorf("unsupported offline signing adapter")
	}
	output, err := run(ctx, directory, script, input)
	if err != nil {
		return stackstx.Transaction{}, err
	}
	var tx stackstx.Transaction
	if json.Unmarshal(output, &tx) != nil || !stackstx.Valid(tx) {
		return stackstx.Transaction{}, fmt.Errorf("invalid signed transaction")
	}
	return tx, nil
}

// EncodeArguments serializes public Clarity values without observing or submitting anything.
func EncodeArguments(ctx context.Context, directory string, arguments []Argument) ([]string, error) {
	output, err := run(ctx, directory, "contract-sign.mjs", map[string]any{"operation": "encode-arguments", "arguments": arguments})
	if err != nil {
		return nil, err
	}
	var values []string
	if json.Unmarshal(output, &values) != nil || len(values) != len(arguments) {
		return nil, fmt.Errorf("invalid offline argument encodings")
	}
	return values, nil
}

// run isolates explicit SDK inputs from process arguments, logs and network discovery.
func run(ctx context.Context, directory, script string, input any) ([]byte, error) {
	data, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "node", filepath.Join(directory, script))
	command.Stdin = bytes.NewReader(data)
	var output boundedOutput
	command.Stdout = &output
	if command.Run() != nil {
		return nil, fmt.Errorf("offline SDK adapter failed")
	}
	return output.Bytes(), nil
}

// boundedOutput limits adapter output before it is decoded or retained in memory.
type boundedOutput struct{ bytes.Buffer }

// Write accepts at most one bounded serialized transaction response.
func (b *boundedOutput) Write(data []byte) (int, error) {
	if b.Len()+len(data) > 512*1024+4096 {
		return 0, fmt.Errorf("offline adapter response exceeds limit")
	}
	return b.Buffer.Write(data)
}
