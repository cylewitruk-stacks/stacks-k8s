package main

import stacks "github.com/cylewitruk-stacks/stacks-k8s/libs/stacks/rpc"

// stacksTipFacts defines the public snapshot shape independently of library structs.
type stacksTipFacts struct {
	// NetworkID is the node's native network identifier.
	NetworkID uint32 `json:"networkID"`
	// BurnHeight is the processed Bitcoin height.
	BurnHeight uint64 `json:"burnHeight"`
	// StacksHeight is the canonical Stacks height.
	StacksHeight uint64 `json:"stacksHeight"`
	// Tip is the lowercase hexadecimal Stacks header hash.
	Tip string `json:"tip"`
	// ConsensusHash identifies the canonical Stacks tip's sortition.
	ConsensusHash string `json:"consensusHash"`
	// BurnConsensusHash identifies the processed Bitcoin fork.
	BurnConsensusHash string `json:"burnConsensusHash"`
	// IndexBlockID is the lowercase hexadecimal canonical block identifier.
	IndexBlockID string `json:"indexBlockID"`
	// FullySynced is the native synchronization report, not a liveness assertion.
	FullySynced bool `json:"fullySynced"`
}

// poxFacts captures timing at one exact index block ID.
type poxFacts struct {
	// Tip binds the PoX read to the canonical index block ID.
	Tip string `json:"tip"`
	// PoX contains only public protocol fields with explicit encodings.
	PoX poxView `json:"pox"`
}

// poxView keeps full-width monetary values out of JSON numbers.
type poxView struct {
	// Contract is the qualified PoX contract principal.
	Contract string `json:"contract"`
	// BurnHeight is the native processed Bitcoin height.
	BurnHeight uint64 `json:"burnHeight"`
	// RewardCycle is the current cycle identifier.
	RewardCycle uint64 `json:"rewardCycle"`
	// CycleLength is the number of Bitcoin blocks per cycle.
	CycleLength uint64 `json:"cycleLength"`
	// MinThresholdMicroSTX is an unsigned decimal uint128 string.
	MinThresholdMicroSTX string `json:"minThresholdMicroSTX"`
	// BlocksUntilPrepare preserves negative native distances after the boundary.
	BlocksUntilPrepare int64 `json:"blocksUntilPrepare"`
}

// rewardSetFacts separates explicit native unavailability from a prepared set.
type rewardSetFacts struct {
	// Tip binds the read to the canonical index block ID.
	Tip string `json:"tip"`
	// Cycle is the requested reward cycle.
	Cycle uint64 `json:"cycle"`
	// Available is the native availability report, independent of capture success.
	Available bool `json:"available"`
	// UnavailableReason contains only the RPC client's allowlisted vocabulary.
	UnavailableReason stacks.PreparedSetReason `json:"unavailableReason,omitempty"`
	// Prepared is present only when the native set is available.
	Prepared *preparedRewardSet `json:"prepared,omitempty"`
}

// preparedRewardSet preserves native signer order and threshold without Clarity internals.
type preparedRewardSet struct {
	// Version identifies the native reward-set encoding.
	Version uint32 `json:"version"`
	// Signers preserves the native order, including an empty array when appropriate.
	Signers []preparedSigner `json:"signers"`
	// ThresholdMicroSTX is an unsigned decimal uint128 string.
	ThresholdMicroSTX string `json:"thresholdMicroSTX"`
}

// preparedSigner exposes the public signer identity, voting weight and stake.
type preparedSigner struct {
	// PublicKey is a compressed public key encoded as lowercase hexadecimal.
	PublicKey string `json:"publicKey"`
	// Weight is the native voting weight.
	Weight uint32 `json:"weight"`
	// StackedAmountMicroSTX is an unsigned decimal uint128 string.
	StackedAmountMicroSTX string `json:"stackedAmountMicroSTX"`
}

// publicTip projects only the fields promised by the tool's output contract.
func publicTip(v stacks.ChainView) stacksTipFacts {
	return stacksTipFacts{
		NetworkID: v.NetworkID, BurnHeight: v.BurnHeight, StacksHeight: v.StacksHeight, Tip: v.Tip,
		ConsensusHash: v.ConsensusHash, BurnConsensusHash: v.BurnConsensusHash,
		IndexBlockID: v.IndexBlockID, FullySynced: v.FullySynced,
	}
}

// publicPoX is called only for a successfully decoded native PoX response.
func publicPoX(tip string, v stacks.PoX) poxFacts {
	return poxFacts{Tip: tip, PoX: poxView{
		Contract: v.Contract, BurnHeight: v.BurnHeight, RewardCycle: v.RewardCycle, CycleLength: v.CycleLength,
		MinThresholdMicroSTX: v.MinThreshold.Integer.String(), BlocksUntilPrepare: v.BlocksUntilPrepare,
	}}
}

// publicRewardSet is called only after the RPC client validates the native response.
func publicRewardSet(tip string, cycle uint64, v stacks.StackerSet) rewardSetFacts {
	result := rewardSetFacts{Tip: tip, Cycle: cycle, Available: v.Available, UnavailableReason: v.UnavailableReason}
	if !v.Available {
		return result
	}
	set := &preparedRewardSet{
		Version:           v.Version,
		Signers:           make([]preparedSigner, 0, len(v.Signers)),
		ThresholdMicroSTX: v.Threshold.Integer.String(),
	}
	for _, s := range v.Signers {
		set.Signers = append(
			set.Signers,
			preparedSigner{
				PublicKey:             s.PublicKey,
				Weight:                s.Weight,
				StackedAmountMicroSTX: s.StackedAmount.Integer.String(),
			},
		)
	}
	result.Prepared = set
	return result
}
