package bitcoincontrol

import (
	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	bitcoinrpc "github.com/cylewitruk-stacks/stacks-k8s/libs/bitcoin/rpc"
)

// offerBlocks is the number of blocks one offer generates; omission preserves a single block.
func offerBlocks(offer *bitcoin.BitcoinBlockOffer) int64 {
	if offer.Count < 1 {
		return 1
	}
	return int64(offer.Count)
}

// batchLimit is the most blocks one first-gate request may generate for the frozen initialization.
func batchLimit(initial *bitcoin.BitcoinInitialization) int64 {
	if initial.Spec.BatchGeneration {
		return bitcoinrpc.MaxGeneratedBlocks
	}
	return 1
}

// offerCountAuthorized bounds an offer's resulting height by its ceiling and allows more than one
// block only for first-gate bootstrap opportunities of a batching initialization.
func offerCountAuthorized(initial *bitcoin.BitcoinInitialization, offer *bitcoin.BitcoinBlockOffer) bool {
	if offer.Count < 0 {
		return false
	}
	blocks := offerBlocks(offer)
	if offer.ExpectedHeight < 0 || offer.ExpectedHeight >= offer.Ceiling ||
		blocks > offer.Ceiling-offer.ExpectedHeight {
		return false
	}
	if blocks == 1 {
		return true
	}
	return (offer.Mode == "" || offer.Mode == bitcoin.OfferBootstrap) &&
		offer.Ceiling == initial.Spec.MinimumHeight &&
		blocks <= batchLimit(initial)
}

// bootstrapBatch sizes a new first-gate opportunity: at most the batch limit, the outputs a miner
// wallet still needs (remaining > 0) or the blocks left to the ceiling, whichever is smallest.
func bootstrapBatch(initial *bitcoin.BitcoinInitialization, height, ceiling, remaining int64) int32 {
	// batchLimit never exceeds the client limit; naming it keeps the int32 conversion visibly bounded.
	blocks := min(batchLimit(initial), ceiling-height, bitcoinrpc.MaxGeneratedBlocks)
	if remaining > 0 {
		blocks = min(blocks, remaining)
	}
	if blocks <= 1 || ceiling != initial.Spec.MinimumHeight {
		return 0
	}
	return int32(blocks)
}
