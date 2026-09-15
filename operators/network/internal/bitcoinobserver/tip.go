package bitcoinobserver

import (
	"encoding/json"
	"fmt"
)

// UnmarshalJSON requires every tip field, distinguishing absent numeric fields from zero.
func (t *Tip) UnmarshalJSON(data []byte) error {
	var wire struct {
		Height       *int64  `json:"height"`
		Hash         *string `json:"hash"`
		BranchLength *int64  `json:"branchlen"`
		Status       *string `json:"status"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return fmt.Errorf("invalid chain tip")
	}
	if wire.Height == nil || wire.Hash == nil || wire.BranchLength == nil || wire.Status == nil {
		return fmt.Errorf("chain tip omitted required field")
	}
	*t = Tip{Height: *wire.Height, Hash: *wire.Hash, BranchLength: *wire.BranchLength, Status: *wire.Status}
	return nil
}
