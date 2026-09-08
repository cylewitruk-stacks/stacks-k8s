package production

import (
	"context"
	"fmt"
)

// InitializationRPC exposes idempotent wallet preparation and chain observation, not another miner.
type InitializationRPC interface {
	Height(context.Context, string) (int64, error)
	PrepareWallet(context.Context, string, string, string) error
}

// Height observes a complete regtest chain identity and height.
func (r *BitcoinRPC) Height(ctx context.Context, endpoint string) (int64, error) {
	var chain struct {
		Chain  string `json:"chain"`
		Blocks *int64 `json:"blocks"`
	}
	if err := r.call(ctx, endpoint, "initialization-height", "getblockchaininfo", nil, &chain); err != nil {
		return 0, err
	}
	if chain.Chain != "regtest" || chain.Blocks == nil || *chain.Blocks < 0 {
		return 0, fmt.Errorf("regtest height is unavailable")
	}
	return *chain.Blocks, nil
}

// PrepareWallet reconciles one named watch-only wallet and public address descriptor.
// Repeated attempts observe state before idempotent named creation/loading/import; no blocks are generated.
func (r *BitcoinRPC) PrepareWallet(ctx context.Context, endpoint, wallet, address string) error {
	var loaded []string
	if err := r.call(ctx, endpoint, "wallet-list", "listwallets", nil, &loaded); err != nil {
		return err
	}
	found := false
	for _, name := range loaded {
		found = found || name == wallet
	}
	if !found {
		var directory struct {
			Wallets []struct {
				Name string `json:"name"`
			} `json:"wallets"`
		}
		if err := r.call(ctx, endpoint, "wallet-directory", "listwalletdir", nil, &directory); err != nil {
			return err
		}
		exists := false
		for _, entry := range directory.Wallets {
			exists = exists || entry.Name == wallet
		}
		var result struct {
			Name string `json:"name"`
		}
		method, args := "loadwallet", []any{wallet, true}
		if !exists {
			method, args = "createwallet", []any{wallet, true, true, "", false, true, true}
		}
		if err := r.call(ctx, endpoint, "wallet-prepare", method, args, &result); err != nil {
			return err
		}
		if result.Name != wallet {
			return fmt.Errorf("wallet preparation did not acknowledge its name")
		}
	}
	walletEndpoint := endpoint + "/wallet/" + wallet
	var info struct {
		Name        string `json:"walletname"`
		PrivateKeys *bool  `json:"private_keys_enabled"`
		Descriptors *bool  `json:"descriptors"`
	}
	if err := r.call(ctx, walletEndpoint, "wallet-info", "getwalletinfo", nil, &info); err != nil {
		return err
	}
	if info.Name != wallet || info.PrivateKeys == nil || *info.PrivateKeys || info.Descriptors == nil || !*info.Descriptors {
		return fmt.Errorf("wallet does not match the watch-only descriptor profile")
	}
	var descriptor struct {
		Descriptor  string `json:"descriptor"`
		PrivateKeys *bool  `json:"hasprivatekeys"`
	}
	if err := r.call(ctx, endpoint, "wallet-descriptor", "getdescriptorinfo", []any{"addr(" + address + ")"}, &descriptor); err != nil {
		return err
	}
	if descriptor.Descriptor == "" || descriptor.PrivateKeys == nil || *descriptor.PrivateKeys {
		return fmt.Errorf("public address descriptor unavailable")
	}
	var listing struct {
		Descriptors []struct {
			Desc string `json:"desc"`
		} `json:"descriptors"`
	}
	if err := r.call(ctx, walletEndpoint, "wallet-descriptors", "listdescriptors", []any{false}, &listing); err != nil {
		return err
	}
	for _, entry := range listing.Descriptors {
		if entry.Desc == descriptor.Descriptor {
			return nil
		}
	}
	var imported []struct {
		Success bool `json:"success"`
	}
	if err := r.call(ctx, walletEndpoint, "wallet-import", "importdescriptors", []any{[]any{map[string]any{"desc": descriptor.Descriptor, "timestamp": 0}}}, &imported); err != nil {
		return err
	}
	if len(imported) != 1 || !imported[0].Success {
		return fmt.Errorf("public descriptor import did not confirm")
	}
	return nil
}
