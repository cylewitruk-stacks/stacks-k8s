package main

import (
	"context"
	"errors"
	"time"

	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/libs/bitcoin/rpc"
)

// blockHeader captures immutable ancestry fields, excluding moving confirmations.
type blockHeader struct {
	Hash       string  `json:"hash"`
	Height     *uint64 `json:"height"`
	Previous   string  `json:"previousblockhash,omitempty"`
	MerkleRoot string  `json:"merkleroot"`
	Time       *uint64 `json:"time"`
}

// inspectBitcoin walks hash-linked branches, never a moving height selection.
func inspectBitcoin(ctx context.Context, r request, w *writer) error {
	c := bitcoin.New(r.Credentials)
	var info struct {
		Chain  string  `json:"chain"`
		Height *uint64 `json:"blocks"`
		Tip    string  `json:"bestblockhash"`
	}
	start := time.Now().UTC()
	err := c.Call(ctx, r.Endpoint, "chain", bitcoin.MethodGetBlockchainInfo, nil, &info)
	if err == nil && (info.Chain != bitcoin.ChainRegtest || info.Height == nil || !hashPattern.MatchString(info.Tip)) {
		err = errors.New("incomplete or non-regtest chain identity")
	}
	w.emit(recordBitcoinTip, start, nil, info, err)
	if err != nil || w.err != nil {
		return errors.Join(err, w.err)
	}
	tip := r.Tip
	if tip == "" {
		tip = info.Tip
	}
	left, leftComplete, leftErr := walk(ctx, c, r, w, tip, nil)
	err = errors.Join(err, leftErr)
	if r.CompareTip != "" {
		if w.err != nil {
			return errors.Join(err, w.err)
		}
		var right map[string]blockHeader
		rightComplete := true
		if r.CompareTip != tip {
			var rightErr error
			right, rightComplete, rightErr = walk(ctx, c, r, w, r.CompareTip, left)
			err = errors.Join(err, rightErr)
		}
		if !leftComplete || !rightComplete || w.err != nil {
			return errors.Join(err, w.err)
		}
		relation := ancestryUnknown
		if _, ok := left[r.CompareTip]; ok {
			relation = ancestryComparisonAncestor
		} else if _, ok := right[tip]; ok {
			relation = ancestryPrimaryAncestor
		} else {
			for h := range left {
				if _, ok := right[h]; ok {
					relation = ancestryShared
					break
				}
			}
		}
		if tip == r.CompareTip {
			relation = ancestrySame
		}
		w.emit(recordAncestry, time.Now().UTC(), nil, struct {
			Primary    string           `json:"primary"`
			Comparison string           `json:"comparison"`
			Relation   ancestryRelation `json:"relation"`
		}{tip, r.CompareTip, relation}, nil)
	}
	return err
}

// walk captures at most Blocks headers and transaction-ID summaries per branch.
// Its completion flag tracks validated header traversal independently of summary reads.
func walk(
	ctx context.Context,
	c *bitcoin.Client,
	r request,
	w *writer,
	tip string,
	known map[string]blockHeader,
) (map[string]blockHeader, bool, error) {
	seen := map[string]blockHeader{}
	var result error
	var childHeight *uint64
	for i := 0; i < r.Blocks; i++ {
		if h, ok := known[tip]; ok {
			if childHeight != nil && (*childHeight == 0 || *h.Height != *childHeight-1) {
				err := errors.New("invalid hash-linked header intersection")
				w.emit(recordBitcoinHeader, time.Now().UTC(), &subject{BlockHash: tip}, nil, err)
				return seen, false, errors.Join(result, err)
			}
			seen[tip] = h
			return seen, true, result
		}
		if ctx.Err() != nil || w.err != nil {
			return seen, false, errors.Join(result, ctx.Err(), w.err)
		}
		start := time.Now().UTC()
		var h blockHeader
		err := c.Call(ctx, r.Endpoint, "header", bitcoin.MethodGetBlockHeader, []any{tip, true}, &h)
		if err == nil &&
			(h.Hash != tip || h.Height == nil || h.Time == nil || !hashPattern.MatchString(h.MerkleRoot) ||
				(*h.Height > 0 && !hashPattern.MatchString(h.Previous)) ||
				(*h.Height == 0 && h.Previous != "") ||
				(childHeight != nil && (*childHeight == 0 || *h.Height != *childHeight-1))) {
			err = errors.New("invalid hash-linked header")
		}
		w.emit(recordBitcoinHeader, start, &subject{BlockHash: tip}, h, err)
		if err != nil || w.err != nil {
			return seen, false, errors.Join(result, err, w.err)
		}
		seen[tip] = h
		childHeight = h.Height
		var block struct {
			Hash string    `json:"hash"`
			Tx   *[]string `json:"tx"`
		}
		start = time.Now().UTC()
		err = c.Call(ctx, r.Endpoint, "block", bitcoin.MethodGetBlock, []any{tip, 1}, &block)
		if err == nil && (block.Hash != tip || block.Tx == nil || len(*block.Tx) == 0) {
			err = errors.New("incomplete block transaction list")
		}
		if err == nil {
			for _, id := range *block.Tx {
				if !hashPattern.MatchString(id) {
					err = errors.New("invalid transaction identity")
					break
				}
			}
		}
		if err != nil {
			w.emit(recordBitcoinTransactions, start, &subject{BlockHash: tip}, nil, err)
			result = errors.Join(result, err)
		} else {
			ids := *block.Tx
			count := len(ids)
			if count > 128 {
				ids = ids[:128]
			}
			w.emit(recordBitcoinTransactions, start, &subject{BlockHash: tip}, struct {
				Hash      string   `json:"hash"`
				Count     int      `json:"count"`
				IDs       []string `json:"txids"`
				Truncated bool     `json:"truncated"`
			}{tip, count, ids, count > len(ids)}, nil)
		}
		if *h.Height == 0 {
			return seen, true, result
		}
		tip = h.Previous
	}
	return seen, true, result
}
