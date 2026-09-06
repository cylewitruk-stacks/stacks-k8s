package production

import (
	"context"
	"encoding/json"
	"fmt"

	actionv1 "github.com/cylewitruk-stacks/stacks-k8s/apis/network/actions/v1alpha1"
)

// ReorganizationRPC is the fixed opt-in local-chain and compensation surface.
type ReorganizationRPC interface {
	RPC
	Tip(context.Context, string) (actionv1.BitcoinChainPoint, error)
	Header(context.Context, string, string) (actionv1.BitcoinChainPoint, error)
	HashAt(context.Context, string, int64) (string, error)
	CheckTips(context.Context, string) error
	Invalidate(context.Context, string, string, string) error
	Reconsider(context.Context, string, string, string) error
}

// validHash accepts the canonical fixed-width lowercase Bitcoin encoding.
func validHash(value string) bool {
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

// Header reads and validates the exact requested header without changing chain state.
func (r *BitcoinRPC) Header(ctx context.Context, endpoint, hash string) (actionv1.BitcoinChainPoint, error) {
	var point actionv1.BitcoinChainPoint
	if !validHash(hash) {
		return point, fmt.Errorf("invalid requested header hash")
	}
	if err := r.call(ctx, endpoint, "header", "getblockheader", []any{hash, true}, &point); err != nil {
		return point, err
	}
	if point.Hash != hash || point.Height < 0 || !validHash(point.Chainwork) || (point.Height > 0 && !validHash(point.PreviousBlockHash)) {
		return actionv1.BitcoinChainPoint{}, fmt.Errorf("invalid header identity")
	}
	return point, nil
}

// Tip reads a regtest canonical tip and checks its corresponding header.
func (r *BitcoinRPC) Tip(ctx context.Context, endpoint string) (actionv1.BitcoinChainPoint, error) {
	var chain struct {
		Chain  string `json:"chain"`
		Hash   string `json:"bestblockhash"`
		Height int64  `json:"blocks"`
		Work   string `json:"chainwork"`
	}
	if err := r.call(ctx, endpoint, "tip", "getblockchaininfo", []any{}, &chain); err != nil {
		return actionv1.BitcoinChainPoint{}, err
	}
	point, err := r.Header(ctx, endpoint, chain.Hash)
	if err != nil {
		return point, err
	}
	if chain.Chain != "regtest" || point.Height != chain.Height || point.Chainwork != chain.Work {
		return actionv1.BitcoinChainPoint{}, fmt.Errorf("canonical tip changed or is not regtest")
	}
	return point, nil
}

// HashAt reads the local canonical hash at one bounded height.
func (r *BitcoinRPC) HashAt(ctx context.Context, endpoint string, height int64) (string, error) {
	var hash string
	if height < 0 {
		return "", fmt.Errorf("negative chain height")
	}
	if err := r.call(ctx, endpoint, "hash-at-height", "getblockhash", []any{height}, &hash); err != nil {
		return "", err
	}
	if !validHash(hash) {
		return "", fmt.Errorf("invalid canonical hash")
	}
	return hash, nil
}

// CheckTips rejects known invalid branch state and oversized admission inventories.
func (r *BitcoinRPC) CheckTips(ctx context.Context, endpoint string) error {
	var tips []struct {
		Hash   string `json:"hash"`
		Status string `json:"status"`
	}
	if err := r.call(ctx, endpoint, "tips", "getchaintips", []any{}, &tips); err != nil {
		return err
	}
	if len(tips) == 0 || len(tips) > 64 {
		return fmt.Errorf("unsupported tip inventory")
	}
	active := 0
	for _, tip := range tips {
		if !validHash(tip.Hash) || tip.Status == "invalid" {
			return fmt.Errorf("unsupported branch validity state")
		}
		if tip.Status == "active" {
			active++
		}
	}
	if active != 1 {
		return fmt.Errorf("ambiguous active tip inventory")
	}
	return nil
}

// Invalidate requests the one admitted local invalidity marker and requires a null receipt.
func (r *BitcoinRPC) Invalidate(ctx context.Context, endpoint, hash, id string) error {
	return r.marker(ctx, endpoint, hash, id, "invalidateblock")
}

// Reconsider compensates only the original admitted marker and requires a null receipt.
func (r *BitcoinRPC) Reconsider(ctx context.Context, endpoint, hash, id string) error {
	return r.marker(ctx, endpoint, hash, id, "reconsiderblock")
}

// marker keeps the two fixed null-returning operations non-retrying and attributable.
func (r *BitcoinRPC) marker(ctx context.Context, endpoint, hash, id, method string) error {
	if !validHash(hash) {
		return fmt.Errorf("invalid marker hash")
	}
	var value json.RawMessage
	if err := r.call(ctx, endpoint, id, method, []any{hash}, &value); err != nil {
		return err
	}
	if string(value) != "null" {
		return fmt.Errorf("invalid marker receipt")
	}
	return nil
}
