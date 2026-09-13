package bitcoincontrol

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"

	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// chain reads a complete regtest tip without mutation credentials leaving the worker.
func (w *Worker) chain(ctx context.Context, endpoint string) (int64, string, error) {
	var result struct {
		Chain  string `json:"chain"`
		Blocks *int64 `json:"blocks"`
		Best   string `json:"bestblockhash"`
	}
	if e := w.RPC.Call(ctx, endpoint, "observe-chain", "getblockchaininfo", nil, &result); e != nil {
		return 0, "", e
	}
	if result.Chain != "regtest" || result.Blocks == nil || *result.Blocks < 0 || !hashValid(result.Best) {
		return 0, "", fmt.Errorf("complete regtest tip unavailable")
	}
	return *result.Blocks, result.Best, nil
}

// wallets resolves only public identity required by current admitted wallet attachments.
func (w *Worker) wallets(ctx context.Context, a admitted) ([]bitcoin.FrozenBitcoinWallet, error) {
	result := []bitcoin.FrozenBitcoinWallet{}
	node := a.participant.Status.Admission.Configuration.BitcoinNode
	if node == nil {
		return nil, fmt.Errorf("Bitcoin configuration unavailable")
	}
	for _, ref := range ptr.Deref(node.WalletRefs, nil) {
		wallet := &bitcoin.BitcoinWallet{}
		if e := w.Reader.Get(ctx, client.ObjectKey{Namespace: a.participant.Namespace, Name: ref.Name}, wallet); e != nil {
			return nil, e
		}
		pinned := false
		for _, binding := range a.participant.Status.Admission.Dependencies {
			pinned = pinned || binding.Kind == "BitcoinWallet" && binding.Name == wallet.Name && binding.UID == wallet.UID && binding.Fingerprint == wallet.Status.Digest
		}
		if !pinned || wallet.DeletionTimestamp != nil {
			return nil, fmt.Errorf("attached wallet identity unavailable")
		}
		public, e := publicWallet(wallet)
		if e != nil {
			return nil, e
		}
		result = append(result, public)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result, nil
}

// observe returns successful native facts and at most one needed wallet mutation.
func (w *Worker) observe(ctx context.Context, a admitted, record *bitcoin.BitcoinExecution) (*bitcoin.BitcoinObservation, *bitcoin.BitcoinArmedRPC, error) {
	height, tip, e := w.chain(ctx, a.target.Endpoint)
	if e != nil {
		return nil, nil, e
	}
	wallets, e := w.wallets(ctx, a)
	if e != nil {
		return nil, nil, e
	}
	observation := &bitcoin.BitcoinObservation{Target: a.target, Height: height, Tip: tip, ObservedAt: metav1.NewTime(w.Now().UTC())}
	var pending *bitcoin.BitcoinArmedRPC
	for _, wallet := range wallets {
		if removal := record.Status.PendingWalletRemoval; removal != nil && (removal.Wallet.UID == wallet.Wallet.UID || removal.Name == wallet.Name) {
			record.Status.PendingWalletRemoval = nil
		}
	}
	if removal := record.Status.PendingWalletRemoval; removal != nil {
		var loaded []string
		if e := w.RPC.Call(ctx, a.target.Endpoint, "detached-wallet-list", "listwallets", nil, &loaded); e != nil {
			return nil, nil, e
		}
		for _, name := range loaded {
			if name == removal.Name {
				pending = &bitcoin.BitcoinArmedRPC{Method: bitcoin.RPCUnloadWallet, Wallet: removal.DeepCopy()}
			}
		}
		if pending == nil {
			record.Status.PendingWalletRemoval = nil
		}
	}
	for _, wallet := range wallets {
		state, operation, e := w.observeWallet(ctx, a.target.Endpoint, wallet)
		if e != nil {
			return nil, nil, e
		}
		observation.Wallets = append(observation.Wallets, state)
		if pending == nil && operation != nil {
			pending = operation
		}
	}
	// A removed attachment is unloaded only after its frozen bootstrap obligations end.
	if record.Status.Observation != nil {
		var loaded []string
		listed := false
		for _, old := range record.Status.Observation.Wallets {
			present := false
			for _, wallet := range wallets {
				present = present || wallet.Wallet.UID == old.Wallet.UID || wallet.Name == old.Name
			}
			if present {
				continue
			}
			required := old.Wallet.UID == a.initialization.Spec.PayoutWallet.Wallet.UID
			for _, wallet := range a.initialization.Spec.MinerWallets {
				required = required || wallet.Wallet.UID == old.Wallet.UID
			}
			if required {
				return observation, nil, fmt.Errorf("removed wallet retains frozen initialization obligations")
			}
			if !listed {
				if e := w.RPC.Call(ctx, a.target.Endpoint, "removed-wallet-list", "listwallets", nil, &loaded); e != nil {
					return nil, nil, e
				}
				listed = true
			}
			stillLoaded := false
			for _, name := range loaded {
				stillLoaded = stillLoaded || name == old.Name
			}
			if !stillLoaded {
				continue
			}
			// Keep every detached identity while the shared slot settles one unload at a time.
			observation.Wallets = append(observation.Wallets, old)
			if pending == nil {
				pending = &bitcoin.BitcoinArmedRPC{Method: bitcoin.RPCUnloadWallet, Wallet: &bitcoin.BitcoinWalletOperation{Wallet: old.Wallet, Name: old.Name}}
			}
		}
	}
	return observation, pending, nil
}

// observeWallet plans named watch-only wallet convergence without issuing mutations.
func (w *Worker) observeWallet(ctx context.Context, endpoint string, wallet bitcoin.FrozenBitcoinWallet) (bitcoin.BitcoinWalletObservation, *bitcoin.BitcoinArmedRPC, error) {
	state := bitcoin.BitcoinWalletObservation{Wallet: wallet.Wallet, Name: wallet.Name, Address: wallet.Address}
	operation := func(method bitcoin.RPCMethod, descriptor string) *bitcoin.BitcoinArmedRPC {
		return &bitcoin.BitcoinArmedRPC{Method: method, Wallet: &bitcoin.BitcoinWalletOperation{Wallet: wallet.Wallet, Name: wallet.Name, Descriptor: descriptor}}
	}
	var loaded []string
	if e := w.RPC.Call(ctx, endpoint, "wallet-list", "listwallets", nil, &loaded); e != nil {
		return state, nil, e
	}
	found := false
	for _, name := range loaded {
		found = found || name == wallet.Name
	}
	if !found {
		var directory struct {
			Wallets []struct {
				Name string `json:"name"`
			} `json:"wallets"`
		}
		if e := w.RPC.Call(ctx, endpoint, "wallet-directory", "listwalletdir", nil, &directory); e != nil {
			return state, nil, e
		}
		for _, item := range directory.Wallets {
			if item.Name == wallet.Name {
				return state, operation(bitcoin.RPCLoadWallet, ""), nil
			}
		}
		return state, operation(bitcoin.RPCCreateWallet, ""), nil
	}
	local := endpoint + "/wallet/" + url.PathEscape(wallet.Name)
	var info struct {
		Name        string `json:"walletname"`
		Private     *bool  `json:"private_keys_enabled"`
		Descriptors *bool  `json:"descriptors"`
	}
	if e := w.RPC.Call(ctx, local, "wallet-info", "getwalletinfo", nil, &info); e != nil {
		return state, nil, e
	}
	if info.Name != wallet.Name || info.Private == nil || *info.Private || info.Descriptors == nil || !*info.Descriptors {
		return state, nil, fmt.Errorf("wallet differs from watch-only descriptor profile")
	}
	var descriptor struct {
		Descriptor string `json:"descriptor"`
		Private    *bool  `json:"hasprivatekeys"`
	}
	if e := w.RPC.Call(ctx, endpoint, "wallet-descriptor", "getdescriptorinfo", []any{wallet.Descriptor}, &descriptor); e != nil {
		return state, nil, e
	}
	if descriptor.Private == nil || *descriptor.Private || !strings.HasPrefix(descriptor.Descriptor, wallet.Descriptor+"#") {
		return state, nil, fmt.Errorf("public descriptor acknowledgement differs")
	}
	var listing struct {
		Descriptors []struct {
			Desc string `json:"desc"`
		} `json:"descriptors"`
	}
	if e := w.RPC.Call(ctx, local, "wallet-descriptors", "listdescriptors", []any{false}, &listing); e != nil {
		return state, nil, e
	}
	found = false
	for _, item := range listing.Descriptors {
		found = found || item.Desc == descriptor.Descriptor
	}
	if !found {
		return state, operation(bitcoin.RPCImportDescriptor, descriptor.Descriptor), nil
	}
	var outputs []struct {
		Address       string `json:"address"`
		Confirmations int64  `json:"confirmations"`
		TxID          string `json:"txid"`
		Vout          uint32 `json:"vout"`
	}
	if e := w.RPC.Call(ctx, local, "wallet-maturity", "listunspent", []any{101, 9999999, []string{wallet.Address}, false, map[string]any{"maximumCount": 100}}, &outputs); e != nil {
		return state, nil, e
	}
	if len(outputs) > 100 {
		return state, nil, fmt.Errorf("wallet output observation exceeds bound")
	}
	for _, output := range outputs {
		if output.Address != wallet.Address || output.Confirmations < 101 || !hashValid(output.TxID) {
			return state, nil, fmt.Errorf("mature output identity incomplete")
		}
		var coin struct {
			Coinbase      *bool `json:"coinbase"`
			Confirmations int64 `json:"confirmations"`
		}
		if e := w.RPC.Call(ctx, endpoint, "wallet-coinbase", "gettxout", []any{output.TxID, output.Vout, true}, &coin); e != nil {
			return state, nil, e
		}
		if coin.Coinbase == nil || coin.Confirmations < 101 {
			return state, nil, fmt.Errorf("mature coinbase observation unavailable")
		}
		if *coin.Coinbase {
			state.MatureOutputs++
		}
	}
	state.Ready = true
	return state, nil, nil
}

// mutate performs exactly one supported native mutation; any error retains Armed.
func (w *Worker) mutate(ctx context.Context, request bitcoin.BitcoinArmedRPC) (string, error) {
	if request.Method == bitcoin.RPCInvalidateBlock || request.Method == bitcoin.RPCReconsiderBlock {
		if request.Action == nil || !hashValid(request.BlockHash) {
			return "", fmt.Errorf("invalid finite marker inputs")
		}
		method := "invalidateblock"
		if request.Method == bitcoin.RPCReconsiderBlock {
			method = "reconsiderblock"
		}
		var result json.RawMessage
		if err := w.RPC.Call(ctx, request.Target.Endpoint, request.ID, method, []any{request.BlockHash}, &result); err != nil {
			return "", err
		}
		if string(result) != "null" {
			return "", fmt.Errorf("invalid marker receipt")
		}
		return "", nil
	}
	if request.Method == bitcoin.RPCGenerate {
		if request.Action != nil {
			return w.RPC.Generate(ctx, request.Target.Endpoint, request.Address, request.ID)
		}
		if request.Offer == nil {
			return "", fmt.Errorf("missing generation inputs")
		}
		return w.RPC.Generate(ctx, request.Target.Endpoint, request.Offer.Address, request.ID)
	}
	if request.Wallet == nil {
		return "", fmt.Errorf("missing wallet inputs")
	}
	wallet := request.Wallet
	endpoint := request.Target.Endpoint
	switch request.Method {
	case bitcoin.RPCCreateWallet, bitcoin.RPCLoadWallet:
		method, args := "loadwallet", []any{wallet.Name, true}
		if request.Method == bitcoin.RPCCreateWallet {
			method, args = "createwallet", []any{wallet.Name, true, true, "", false, true, true}
		}
		var result struct {
			Name string `json:"name"`
		}
		if e := w.RPC.Call(ctx, endpoint, request.ID, method, args, &result); e != nil {
			return "", e
		}
		if result.Name != wallet.Name {
			return "", fmt.Errorf("wallet receipt name differs")
		}
	case bitcoin.RPCImportDescriptor:
		var imported []struct {
			Success bool `json:"success"`
		}
		if e := w.RPC.Call(ctx, endpoint+"/wallet/"+url.PathEscape(wallet.Name), request.ID, "importdescriptors", []any{[]any{map[string]any{"desc": wallet.Descriptor, "timestamp": 0}}}, &imported); e != nil {
			return "", e
		}
		if len(imported) != 1 || !imported[0].Success {
			return "", fmt.Errorf("descriptor import receipt incomplete")
		}
	case bitcoin.RPCUnloadWallet:
		var result map[string]any
		if e := w.RPC.Call(ctx, endpoint, request.ID, "unloadwallet", []any{wallet.Name, false}, &result); e != nil {
			return "", e
		}
	default:
		return "", fmt.Errorf("unsupported managed Bitcoin mutation")
	}
	return "", nil
}

// hashValid checks the node's canonical unprefixed hash representation.
func hashValid(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, c := range value {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
