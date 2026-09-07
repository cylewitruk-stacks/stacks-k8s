package bootstrap

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"reflect"
	"strconv"

	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha1"
	stacks "github.com/cylewitruk-stacks/stacks-k8s/apis/network/stacks/v1alpha1"
	network "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha1"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/environment"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/naming"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/profiles"
)

// manifest reads only the network and private bootstrap binding from the provisioning document.
type manifest struct {
	Items     []json.RawMessage     `json:"items"`
	Bootstrap environment.Bootstrap `json:"bootstrap"`
}

// Load validates the private provisioning document and its immutable genesis binding.
func Load(path string) (environment.Bootstrap, *network.StacksNetwork, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return environment.Bootstrap{}, nil, err
	}
	var doc manifest
	if json.Unmarshal(data, &doc) != nil {
		return doc.Bootstrap, nil, fmt.Errorf("invalid provisioning document")
	}
	var parent *network.StacksNetwork
	for _, raw := range doc.Items {
		var header struct {
			Kind string `json:"kind"`
		}
		if json.Unmarshal(raw, &header) != nil {
			return doc.Bootstrap, nil, fmt.Errorf("invalid resource")
		}
		if header.Kind == "StacksNetwork" {
			if parent != nil {
				return doc.Bootstrap, nil, fmt.Errorf("expected one network")
			}
			parent = &network.StacksNetwork{}
			if err = json.Unmarshal(raw, parent); err != nil {
				return doc.Bootstrap, nil, fmt.Errorf("invalid network")
			}
		}
	}
	if parent == nil || parent.Name != doc.Bootstrap.NetworkName || parent.Namespace != doc.Bootstrap.Namespace || parent.Spec.Genesis == nil {
		return doc.Bootstrap, nil, fmt.Errorf("bootstrap/network identity mismatch")
	}
	if err = ValidateGenesis(doc.Bootstrap, parent); err != nil {
		return doc.Bootstrap, nil, err
	}
	return doc.Bootstrap, parent, nil
}

// ValidateGenesis verifies the shared snapshot and named bootstrap account before any mutation.
func ValidateGenesis(setup environment.Bootstrap, parent *network.StacksNetwork) error {
	digest, err := environment.GenesisDigest(parent.Spec.Genesis)
	if err != nil {
		return err
	}
	if digest != setup.GenesisDigest {
		return fmt.Errorf("bootstrap genesis digest mismatch")
	}
	found := false
	for _, a := range parent.Spec.Genesis.Balances {
		if a.Name == setup.SignerAccount && a.Address == setup.Signer.Address {
			found = true
		}
	}
	if !found {
		return fmt.Errorf("bootstrap account is absent from network genesis")
	}
	expected := profiles.DefaultGenesis()
	resolved, err := profiles.ResolveGenesis(parent.Spec.Genesis)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(resolved.UseTestGenesisChainstate, expected.UseTestGenesisChainstate) || !reflect.DeepEqual(resolved.Epochs, expected.Epochs) || !reflect.DeepEqual(resolved.PoX, expected.PoX) {
		return fmt.Errorf("external bootstrap currently supports only the qualified PoX-4 regtest schedule")
	}
	return nil
}

// Run initializes a fresh suspended network, enrolls its seeded stacker and verifies first production.
func Run(ctx context.Context, options Options) error {
	setup, declared, err := Load(options.Manifest)
	if err != nil {
		return err
	}
	if options.Kubeconfig == "" || options.Context == "" || options.BitcoinPort < 1 || options.BitcoinPort > 65535 || options.StacksPort < 1 || options.StacksPort > 65535 || options.StacksPort == options.BitcoinPort {
		return fmt.Errorf("explicit kubeconfig/context and distinct valid loopback ports are required")
	}
	s := &session{options: options, namespace: setup.Namespace, http: &http.Client{Transport: &http.Transport{DisableKeepAlives: true}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
	if err = s.claimEvidence("bootstrap"); err != nil {
		return err
	}
	defer s.close()
	ready := func(kind, name string) bool {
		var actor struct {
			Status struct {
				Ready bool `json:"ready"`
			} `json:"status"`
		}
		return s.get(ctx, kind, name, &actor) == nil && actor.Status.Ready
	}
	if err = wait(ctx, "Bitcoin readiness", 180, func() bool { return ready("bitcoinnode", naming.Child(setup.NetworkName, "bitcoin")) }); err != nil {
		return err
	}
	if err = s.forward(ctx, naming.Child(setup.NetworkName, "bitcoin"), options.BitcoinPort, 18443); err != nil {
		return err
	}
	var parent network.StacksNetwork
	if err = s.get(ctx, "stacksnetwork", setup.NetworkName, &parent); err != nil {
		return err
	}
	if err = ValidateGenesis(setup, &parent); err != nil {
		return err
	}
	if !reflect.DeepEqual(parent.Spec.BitcoinBlockProduction, declared.Spec.BitcoinBlockProduction) || !reflect.DeepEqual(parent.Spec.StacksNodes, declared.Spec.StacksNodes) || !reflect.DeepEqual(parent.Spec.Signers, declared.Spec.Signers) || !reflect.DeepEqual(parent.Spec.BitcoinNodes, declared.Spec.BitcoinNodes) || !reflect.DeepEqual(parent.Spec.StacksTransactionProduction, declared.Spec.StacksTransactionProduction) {
		return fmt.Errorf("bootstrap actor declarations changed")
	}
	var ledger bitcoin.BitcoinProductionTarget
	if err = s.get(ctx, "bitcoinproductiontarget", naming.Child(setup.NetworkName, "bitcoin"), &ledger); err != nil {
		return err
	}
	var height int64
	if err = s.bitcoin(ctx, setup, "getblockcount", nil, "", &height); err != nil {
		return err
	}
	if err = validateFreshBitcoin(&parent, &ledger, height); err != nil {
		return err
	}

	if parent.Spec.StacksTransactionProduction == nil || !parent.Spec.StacksTransactionProduction.Paused {
		return fmt.Errorf("bootstrap requires paused transfer production")
	}
	for _, node := range parent.Spec.StacksNodes {
		if !node.Suspended {
			return fmt.Errorf("bootstrap requires initially suspended Stacks nodes")
		}
	}
	for _, signer := range parent.Spec.Signers {
		if !signer.Suspended {
			return fmt.Errorf("bootstrap requires initially suspended signers")
		}
	}
	// Persist the experiment identity before mutation; failure is not automatically replayed.
	if err = s.record("BootstrapStarted", map[string]any{"network": parent.Name, "networkUID": string(parent.UID), "genesisDigest": setup.GenesisDigest}); err != nil {
		return err
	}
	var wallet struct {
		Name string `json:"name"`
	}
	if err = s.bitcoin(ctx, setup, "createwallet", []any{"stacks-miner", true, true, "", false, true}, "", &wallet); err != nil {
		return err
	}
	if wallet.Name == "" {
		return fmt.Errorf("wallet creation did not confirm")
	}
	var descriptor struct {
		Descriptor string `json:"descriptor"`
	}
	if err = s.bitcoin(ctx, setup, "getdescriptorinfo", []any{"addr(" + setup.Bitcoin.Address + ")"}, "", &descriptor); err != nil {
		return err
	}
	var imported []struct {
		Success bool `json:"success"`
	}
	if err = s.bitcoin(ctx, setup, "importdescriptors", []any{[]any{map[string]any{"desc": descriptor.Descriptor, "timestamp": 0}}}, "stacks-miner", &imported); err != nil {
		return err
	}
	if len(imported) != 1 || !imported[0].Success {
		return fmt.Errorf("descriptor import did not confirm")
	}
	var hashes []string
	if err = s.bitcoin(ctx, setup, "generatetoaddress", []any{201, setup.Bitcoin.Address, 1000000}, "", &hashes); err != nil {
		return err
	}
	if len(hashes) != 201 {
		return fmt.Errorf("bootstrap generation is incomplete")
	}
	if err = s.record("BitcoinBootstrap", map[string]any{"blocks": len(hashes), "lastBlockHash": hashes[len(hashes)-1]}); err != nil {
		return err
	}
	for i := range parent.Spec.StacksNodes {
		parent.Spec.StacksNodes[i].Suspended = false
	}
	for i := range parent.Spec.Signers {
		parent.Spec.Signers[i].Suspended = false
	}
	if err = s.patch(ctx, parent.Name, map[string]any{"stacksNodes": parent.Spec.StacksNodes, "signers": parent.Spec.Signers}); err != nil {
		return err
	}
	if err = wait(ctx, "Stacks ingress readiness", 240, func() bool { return ready("stacksnode", naming.Child(parent.Name, "signer-node")) }); err != nil {
		return err
	}
	if err = s.forward(ctx, naming.Child(parent.Name, "signer-node"), options.StacksPort, 20443); err != nil {
		return err
	}
	var info nodeInfo
	if err = wait(ctx, "Stacks burnchain catchup", 180, func() bool { return s.stacks(ctx, "/v2/info", &info) == nil && info.BurnHeight >= 201 }); err != nil {
		return err
	}
	if err = s.patch(ctx, parent.Name, map[string]any{"bitcoinBlockProduction": map[string]any{"paused": false}}); err != nil {
		return err
	}
	var pox poxInfo
	if err = wait(ctx, "PoX-4 activation", 180, func() bool { return s.stacks(ctx, "/v2/pox", &pox) == nil && pox.Contract == pox4 }); err != nil {
		return err
	}
	account, err := s.account(ctx, setup.Signer.Address)
	if err != nil {
		return err
	}
	amount := pox.NextCycle.MinThreshold + pox.NextCycle.MinThreshold/2
	if amount < 1 || account.Balance < amount {
		return fmt.Errorf("signer genesis balance is insufficient")
	}
	tx, err := s.sign(ctx, setup, "enroll", pox, account, amount, 12, 1)
	if err != nil {
		return err
	}
	if err = s.record("SignerEnrollmentAuthorized", map[string]any{"txid": tx.TxID, "nonce": account.Nonce}); err != nil {
		return err
	}
	if err = s.submit(ctx, tx); err != nil {
		return err
	}
	if err = s.record("SignerEnrollmentSubmitted", map[string]any{"txid": tx.TxID}); err != nil {
		return err
	}
	if err = wait(ctx, "signer lock confirmation", 180, func() bool {
		var readErr error
		account, readErr = s.account(ctx, setup.Signer.Address)
		return readErr == nil && account.Locked > 0
	}); err != nil {
		return err
	}
	if err = s.record("SignerEnrollmentLocked", map[string]any{"unlockHeight": account.UnlockHeight}); err != nil {
		return err
	}
	if err = wait(ctx, "Nakamoto activation", 240, func() bool {
		return s.stacks(ctx, "/v2/info", &info) == nil && info.BurnHeight >= 230 && info.StacksHeight > 0
	}); err != nil {
		return err
	}
	if err = s.patch(ctx, parent.Name, map[string]any{"stacksTransactionProduction": map[string]any{"paused": false}}); err != nil {
		return err
	}
	var production stacks.StacksTransactionProduction
	if err = wait(ctx, "first exact transfer inclusion", 240, func() bool {
		return s.get(ctx, "stackstransactionproduction", parent.Name, &production) == nil && production.Status.Confirmed > 0
	}); err != nil {
		return err
	}
	if err = s.stacks(ctx, "/v2/info", &info); err != nil {
		return err
	}
	return s.record("BootstrapComplete", map[string]any{"burnHeight": info.BurnHeight, "stacksHeight": info.StacksHeight, "serverVersion": info.ServerVersion, "genesisHash": info.GenesisHash, "confirmedTransfers": production.Status.Confirmed})
}

// bitcoin performs one RPC attempt and decodes only acknowledged success.
func (s *session) bitcoin(ctx context.Context, setup environment.Bootstrap, method string, params any, wallet string, out any) error {
	if params == nil {
		params = []any{}
	}
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": "bootstrap", "method": method, "params": params})
	url := "http://127.0.0.1:" + strconv.Itoa(s.options.BitcoinPort)
	if wallet != "" {
		url += "/wallet/" + wallet
	}
	seconds := 30
	if method == "generatetoaddress" {
		seconds = 120
	}
	data, err := s.request(ctx, "POST", url, body, setup.Bitcoin.Username, setup.Bitcoin.Password, "application/json", seconds)
	if err != nil {
		return err
	}
	var result struct {
		Result json.RawMessage `json:"result"`
		Error  json.RawMessage `json:"error"`
	}
	if json.Unmarshal(data, &result) != nil || (len(result.Error) > 0 && string(result.Error) != "null") || len(result.Result) == 0 || string(result.Result) == "null" {
		return fmt.Errorf("Bitcoin %s did not acknowledge success", method)
	}
	if json.Unmarshal(result.Result, out) != nil {
		return fmt.Errorf("invalid Bitcoin %s receipt", method)
	}
	return nil
}

// nodeInfo retains only readiness and identity evidence needed by bootstrap.
type nodeInfo struct {
	BurnHeight    int64  `json:"burn_block_height"`
	StacksHeight  int64  `json:"stacks_tip_height"`
	ServerVersion string `json:"server_version"`
	GenesisHash   string `json:"genesis_chainstate_hash"`
}

// stacks reads one bounded native Stacks endpoint.
func (s *session) stacks(ctx context.Context, path string, out any) error {
	data, err := s.request(ctx, "GET", "http://127.0.0.1:"+strconv.Itoa(s.options.StacksPort)+path, nil, "", "", "", 5)
	if err != nil {
		return err
	}
	if json.Unmarshal(data, out) != nil {
		return fmt.Errorf("invalid Stacks read response")
	}
	return nil
}

// validateFreshBitcoin excludes reused, foreign, unpaused or action-reserved ledgers.
func validateFreshBitcoin(parent *network.StacksNetwork, ledger *bitcoin.BitcoinProductionTarget, height int64) error {
	if parent.UID == "" || !parent.DeletionTimestamp.IsZero() || !ledger.DeletionTimestamp.IsZero() || parent.Spec.BitcoinBlockProduction == nil || !parent.Spec.BitcoinBlockProduction.Paused || ledger.Spec.NetworkUID != string(parent.UID) || ledger.Spec.NetworkName != parent.Name || ledger.Spec.ProductionUID != parent.Status.BitcoinProductionUID || ledger.Spec.Policy.Target != "bitcoin" || !ledger.Spec.Policy.Paused || ledger.Status.DispatchID != "" || ledger.Status.DispatchState != "" || ledger.Status.BlocksProduced != 0 || ledger.Status.Action != nil || ledger.Status.Reorganization != nil || height != 0 {
		return fmt.Errorf("bootstrap requires a fresh height-zero network and its unused, unreserved paused Bitcoin ledger")
	}
	return nil
}
