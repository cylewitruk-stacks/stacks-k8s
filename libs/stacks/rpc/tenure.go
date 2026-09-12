package rpc

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
)

// NakamotoTip checks the native header family and height of the supplied tenure tip.
// Callers must bracket this read with ChainView to reject concurrent tip changes.
func (c *Client) NakamotoTip(ctx context.Context, view ChainView) (bool, error) {
	consensus, err := hex.DecodeString(view.ConsensusHash)
	if err != nil || len(consensus) != 20 || view.ConsensusHash != hex.EncodeToString(consensus) || view.ConsensusHash == strings.Repeat("0", 40) || view.StacksHeight == 0 || !validHash(view.IndexBlockID) {
		return false, errors.New("invalid tenure tip identity")
	}
	var wire struct {
		Header map[string]json.RawMessage `json:"anchored_header"`
	}
	if err := c.get(ctx, "/v3/tenures/tip_metadata/"+view.ConsensusHash, &wire); err != nil {
		return false, err
	}
	if len(wire.Header) != 1 {
		return false, errors.New("native header family unavailable")
	}
	if raw, ok := wire.Header["Epoch2"]; ok {
		var legacy map[string]json.RawMessage
		if json.Unmarshal(raw, &legacy) != nil || len(legacy) == 0 {
			return false, errors.New("invalid legacy header")
		}
		return false, nil
	}
	var header struct {
		Height    *uint64 `json:"chain_length"`
		Consensus string  `json:"consensus_hash"`
	}
	if json.Unmarshal(wire.Header["Nakamoto"], &header) != nil || header.Height == nil || *header.Height != view.StacksHeight || header.Consensus != view.ConsensusHash {
		return false, errors.New("native Nakamoto header differs from canonical tip")
	}
	return true, nil
}
