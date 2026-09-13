package rpc

import (
	"context"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/cylewitruk-stacks/stacks-k8s/libs/stacks/clarity"
	"github.com/cylewitruk-stacks/stacks-k8s/libs/stacks/identity"
)

// ChainView contains the canonical identifiers needed to pin native read-only requests.
type ChainView struct {
	// Info contains the native chain ID, header hash and processed heights.
	Info
	// ConsensusHash identifies the canonical Stacks tip's sortition.
	ConsensusHash string
	// BurnConsensusHash identifies the current processed Bitcoin fork.
	BurnConsensusHash string
	// IndexBlockID is SHA512/256(header hash || consensus hash), as defined by Core.
	IndexBlockID string
	// FullySynced reports native synchronization status without a liveness promise.
	FullySynced bool
}

// ChainView requires complete native canonical identity, including epoch-2 tip identity.
func (c *Client) ChainView(ctx context.Context) (ChainView, error) {
	var wire struct {
		NetworkID     *uint32 `json:"network_id"`
		Burn          *uint64 `json:"burn_block_height"`
		Height        *uint64 `json:"stacks_tip_height"`
		Tip           string  `json:"stacks_tip"`
		Consensus     string  `json:"stacks_tip_consensus_hash"`
		BurnConsensus string  `json:"pox_consensus"`
		Synced        *bool   `json:"is_fully_synced"`
	}
	if err := c.get(ctx, "/v2/info", &wire); err != nil {
		return ChainView{}, err
	}
	header, err := hex.DecodeString(wire.Tip)
	consensus, ce := hex.DecodeString(wire.Consensus)
	burn, be := hex.DecodeString(wire.BurnConsensus)
	if err != nil || ce != nil || be != nil || len(header) != 32 || len(consensus) != 20 || len(burn) != 20 ||
		wire.NetworkID == nil ||
		wire.Burn == nil ||
		wire.Height == nil ||
		wire.Synced == nil {
		return ChainView{}, errors.New("complete canonical node identity unavailable")
	}
	id := sha512.Sum512_256(append(header, consensus...))
	return ChainView{
		Info: Info{
			NetworkID:    *wire.NetworkID,
			BurnHeight:   *wire.Burn,
			StacksHeight: *wire.Height,
			Tip:          hex.EncodeToString(header),
		},
		ConsensusHash:     hex.EncodeToString(consensus),
		BurnConsensusHash: hex.EncodeToString(burn),
		IndexBlockID:      hex.EncodeToString(id[:]),
		FullySynced:       *wire.Synced,
	}, nil
}

// PreparedSigner contains protocol-derived signer weight and full-width stake.
type PreparedSigner struct {
	// PublicKey identifies the native compressed consensus key.
	PublicKey string
	// Weight is calculated by Core, never inferred from desired stake.
	Weight uint32
	// StackedAmount is the native uint128 micro-STX amount.
	StackedAmount clarity.Value
}

// StackerSet contains a native prepared reward set for one explicitly requested cycle and tip.
type StackerSet struct {
	// Available is false only for Core's explicit not_available_try_again response.
	Available bool
	// Version identifies the native classic or waterfall encoding.
	Version uint32
	// Signers preserves the native signer order and weights.
	Signers []PreparedSigner
	// Threshold is the native uint128 micro-STX threshold.
	Threshold clarity.Value
}

// StackerSet reads one canonical-tip-pinned reward set without retries or protocol mutation.
func (c *Client) StackerSet(ctx context.Context, cycle uint64, indexBlockID string) (StackerSet, error) {
	if cycle > 9999999999 || !validHash(indexBlockID) {
		return StackerSet{}, errors.New("invalid prepared-set request identity")
	}
	code, data, err := c.request(
		ctx,
		http.MethodGet,
		fmt.Sprintf("/v3/stacker_set/%d?tip=%s", cycle, indexBlockID),
		"application/json",
		nil,
	)
	if err != nil {
		return StackerSet{}, err
	}
	if code == http.StatusBadRequest {
		var unavailable struct {
			Type string `json:"err_type"`
		}
		if json.Unmarshal(data, &unavailable) == nil && unavailable.Type == "not_available_try_again" {
			return StackerSet{}, nil
		}
	}
	var wire struct {
		Set *struct {
			Version uint32 `json:"reward_set_version"`
			Signers *[]struct {
				Key    string      `json:"signing_key"`
				Weight *uint32     `json:"weight"`
				Amount json.Number `json:"stacked_amt"`
			} `json:"signers"`
			Threshold json.Number `json:"pox_ustx_threshold"`
		} `json:"stacker_set"`
	}
	if code != http.StatusOK || json.Unmarshal(data, &wire) != nil || wire.Set == nil || wire.Set.Signers == nil ||
		len(*wire.Set.Signers) > 1000 ||
		wire.Set.Version > 1 {
		return StackerSet{}, errors.New("prepared signer set unavailable")
	}
	threshold, err := clarity.Uint128(wire.Set.Threshold.String())
	if err != nil || wire.Set.Threshold.String() == "0" {
		return StackerSet{}, errors.New("prepared signer threshold unavailable")
	}
	result := StackerSet{
		Available: true,
		Version:   wire.Set.Version,
		Threshold: threshold,
		Signers:   make([]PreparedSigner, 0, len(*wire.Set.Signers)),
	}
	seen := map[string]bool{}
	var weight uint64
	for _, signer := range *wire.Set.Signers {
		key, ke := identity.FromPublic(signer.Key)
		amount, ae := clarity.Uint128(signer.Amount.String())
		if ke != nil || ae != nil || signer.Weight == nil || *signer.Weight == 0 || signer.Amount.String() == "0" ||
			seen[key.PublicKey] {
			return StackerSet{}, errors.New("invalid prepared signer entry")
		}
		weight += uint64(*signer.Weight)
		if weight > uint64(^uint32(0)) {
			return StackerSet{}, errors.New("prepared signer weight exceeds bound")
		}
		seen[key.PublicKey] = true
		result.Signers = append(
			result.Signers,
			PreparedSigner{PublicKey: key.PublicKey, Weight: *signer.Weight, StackedAmount: amount},
		)
	}
	return result, nil
}
