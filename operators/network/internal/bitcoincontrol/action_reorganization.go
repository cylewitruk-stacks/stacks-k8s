package bitcoincontrol

import (
	"context"
	"errors"
	"fmt"
	"time"

	action "github.com/cylewitruk-stacks/stacks-k8s/apis/network/actions/v1alpha2"
	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	"k8s.io/apimachinery/pkg/api/equality"
)

// errExternalChainMovement identifies a successfully read tip that invalidates captured ancestry.
var errExternalChainMovement = errors.New("external chain movement")

// actionHeader validates the fixed public header fields used for local ancestry proofs.
func (w *Worker) actionHeader(ctx context.Context, endpoint, hash string) (*action.BitcoinChainPoint, error) {
	var h struct {
		Hash     string `json:"hash"`
		Height   *int64 `json:"height"`
		Previous string `json:"previousblockhash"`
		Work     string `json:"chainwork"`
	}
	if !hashValid(hash) {
		return nil, fmt.Errorf("invalid block identity")
	}
	if err := w.RPC.Call(ctx, endpoint, "action-header", "getblockheader", []any{hash, true}, &h); err != nil {
		return nil, err
	}
	if h.Hash != hash || h.Height == nil || *h.Height < 0 || !hashValid(h.Work) || *h.Height > 0 && !hashValid(h.Previous) {
		return nil, fmt.Errorf("incomplete block header")
	}
	return &action.BitcoinChainPoint{Hash: h.Hash, Height: *h.Height, PreviousBlockHash: h.Previous, Chainwork: h.Work}, nil
}

// actionTip couples the canonical height and hash to a validated header.
func (w *Worker) actionTip(ctx context.Context, endpoint string) (*action.BitcoinChainPoint, error) {
	height, hash, err := w.chain(ctx, endpoint)
	if err != nil {
		return nil, err
	}
	tip, err := w.actionHeader(ctx, endpoint, hash)
	if err != nil {
		return nil, err
	}
	if tip.Height != height {
		return nil, fmt.Errorf("tip height differs")
	}
	return tip, nil
}

// actionHashAt reads a local canonical position without accepting malformed hashes.
func (w *Worker) actionHashAt(ctx context.Context, endpoint string, height int64) (string, error) {
	var hash string
	err := w.RPC.Call(ctx, endpoint, "action-hash", "getblockhash", []any{height}, &hash)
	if err != nil {
		return "", err
	}
	if !hashValid(hash) {
		return "", fmt.Errorf("canonical hash malformed")
	}
	return hash, nil
}

// captureReorganization pins a bounded suffix before granting any mutation authority.
func (w *Worker) captureReorganization(ctx context.Context, a admitted, state *bitcoin.BitcoinActionReservation) error {
	spec := state.Reorganization
	if spec == nil || spec.Depth < 1 || spec.Depth > 6 || !spec.BoundaryPolicy.AllowEpochBoundaryCrossing || !spec.BoundaryPolicy.AllowPreparePhaseBoundaryCrossing || !spec.BoundaryPolicy.AllowRewardCycleBoundaryCrossing {
		return fmt.Errorf("reorganization boundary policy unavailable")
	}
	var tips []struct {
		Status string `json:"status"`
		Hash   string `json:"hash"`
	}
	if err := w.RPC.Call(ctx, a.target.Endpoint, "action-tips", "getchaintips", nil, &tips); err != nil {
		return err
	}
	active := 0
	if len(tips) == 0 || len(tips) > 64 {
		return fmt.Errorf("bounded chain tip inventory unavailable")
	}
	for _, tip := range tips {
		if tip.Status == "invalid" || !hashValid(tip.Hash) {
			return fmt.Errorf("preexisting invalid branch")
		}
		if tip.Status == "active" {
			active++
		}
	}
	if active != 1 {
		return fmt.Errorf("canonical tip ambiguous")
	}
	original, err := w.actionTip(ctx, a.target.Endpoint)
	if err != nil {
		return err
	}
	if original.Height < int64(spec.Depth) {
		return fmt.Errorf("suffix extends before genesis")
	}
	cursor := original
	for i := int32(0); i < spec.Depth; i++ {
		state.InvalidatedHash = cursor.Hash
		parent, err := w.actionHeader(ctx, a.target.Endpoint, cursor.PreviousBlockHash)
		if err != nil {
			return err
		}
		if parent.Height+1 != cursor.Height || parent.Chainwork >= cursor.Chainwork {
			return fmt.Errorf("suffix ancestry differs")
		}
		cursor = parent
	}
	latest, err := w.actionTip(ctx, a.target.Endpoint)
	if err != nil {
		return err
	}
	if *latest != *original {
		return fmt.Errorf("tip changed during admission")
	}
	state.OriginalChain = original
	state.ForkParent = cursor
	return nil
}

// reorganizationPreflight refuses external chain movement before each irreversible send.
func (w *Worker) reorganizationPreflight(ctx context.Context, a admitted, state *bitcoin.BitcoinActionReservation, replacement bool) error {
	expected := state.OriginalChain
	if replacement {
		expected = state.ForkParent
		if state.AcceptedChain != nil {
			expected = state.AcceptedChain
		}
		if state.ForkParent == nil || expected == nil || expected.Height-state.ForkParent.Height != int64(state.BlocksGenerated) {
			return fmt.Errorf("replacement receipts unverified")
		}
	}
	if expected == nil {
		return fmt.Errorf("captured chain unavailable")
	}
	tip, err := w.actionTip(ctx, a.target.Endpoint)
	if err != nil {
		return err
	}
	if *tip != *expected {
		return errExternalChainMovement
	}
	return nil
}

// verifyReplacement consumes one known receipt as a contiguous, increasing-work header.
func (w *Worker) verifyReplacement(ctx context.Context, a admitted, record *bitcoin.BitcoinExecution) (bool, error) {
	state := record.Status.Action
	if state.ForkParent == nil {
		return false, fmt.Errorf("fork parent unavailable")
	}
	previous := state.ForkParent
	if state.AcceptedChain != nil {
		previous = state.AcceptedChain
	}
	verified := previous.Height - state.ForkParent.Height
	if verified < 0 || verified > int64(state.BlocksGenerated) || len(state.ReplacementBlockHashes) != int(state.BlocksGenerated) {
		return false, fmt.Errorf("replacement receipt accounting differs")
	}
	if verified == int64(state.BlocksGenerated) {
		return false, nil
	}
	header, err := w.actionHeader(ctx, a.target.Endpoint, state.ReplacementBlockHashes[verified])
	if err != nil {
		return false, err
	}
	if header.Height != previous.Height+1 || header.PreviousBlockHash != previous.Hash || header.Chainwork <= previous.Chainwork {
		return false, fmt.Errorf("replacement ancestry differs")
	}
	state.AcceptedChain = header
	return true, w.Client.Status().Update(ctx, record)
}

// stepReorganization performs bounded suffix replacement and only the captured compensation.
func (w *Worker) stepReorganization(ctx context.Context, record *bitcoin.BitcoinExecution, view actionView, same bool) error {
	state := record.Status.Action
	if state.FinalChain != nil {
		return nil
	}
	if !state.InvalidationAcknowledged && (state.StopReason != "" || state.EffectUncertain) {
		return nil
	}
	cleanup := state.InvalidationAcknowledged && (state.StopReason != "" || state.EffectUncertain || state.BlocksGenerated >= state.Reorganization.Depth+1)
	a, err := w.resolveActor(ctx, record, cleanup || state.CleanupAcknowledged)
	if err != nil {
		return err
	}
	if !equality.Semantic.DeepEqual(a.target, state.Runtime) {
		return w.stopAction(ctx, record, "IdentityDiverged", true)
	}
	if state.InvalidationAcknowledged && !state.CleanupAcknowledged && !w.Now().Before(state.ExpiresAt.Add(30*time.Second)) {
		return w.stopAction(ctx, record, "CleanupDeadlineExceeded", true)
	}
	if state.InvalidationAcknowledged && state.StopReason == "" {
		changed, err := w.verifyReplacement(ctx, a, record)
		if err != nil {
			return w.stopAction(ctx, record, "ReplacementVerificationFailed", false)
		}
		if changed {
			return nil
		}
	}
	if state.CleanupAcknowledged {
		if state.StopReason != "" || state.EffectUncertain {
			return nil
		}
		if state.BlocksGenerated != state.Reorganization.Depth+1 || state.AcceptedChain == nil || state.AcceptedChain.Chainwork <= state.OriginalChain.Chainwork {
			return w.stopAction(ctx, record, "ReplacementWorkInsufficient", true)
		}
		for i, hash := range state.ReplacementBlockHashes {
			actual, err := w.actionHashAt(ctx, a.target.Endpoint, state.ForkParent.Height+int64(i)+1)
			if err != nil {
				return err
			}
			if actual != hash {
				return w.stopAction(ctx, record, "ReplacementNotCanonical", true)
			}
		}
		final, err := w.actionTip(ctx, a.target.Endpoint)
		if err != nil {
			return err
		}
		if final.Chainwork <= state.OriginalChain.Chainwork {
			return w.stopAction(ctx, record, "ReplacementWorkInsufficient", true)
		}
		state.FinalChain = final
		return w.Client.Status().Update(ctx, record)
	}
	if cleanup {
		return w.arm(ctx, record, a, bitcoin.BitcoinArmedRPC{Method: "ReconsiderBlock", Action: state.Request.DeepCopy(), BlockHash: state.InvalidatedHash})
	}
	if !same || !actionAdmissionAcknowledged(view, record) || a.root.Spec.Operation == "Paused" {
		return nil
	}
	if !state.InvalidationAcknowledged {
		return w.armReorganization(ctx, record, a, bitcoin.BitcoinArmedRPC{Method: "InvalidateBlock", Action: state.Request.DeepCopy(), BlockHash: state.InvalidatedHash})
	}
	if state.NextDispatchAt != nil && w.Now().Before(state.NextDispatchAt.Time) {
		return nil
	}
	return w.armReorganization(ctx, record, a, bitcoin.BitcoinArmedRPC{Method: "Generate", Action: state.Request.DeepCopy(), Address: state.Reorganization.Address})
}

// armReorganization records proven ancestry divergence without classifying failed reads as evidence.
func (w *Worker) armReorganization(ctx context.Context, record *bitcoin.BitcoinExecution, a admitted, operation bitcoin.BitcoinArmedRPC) error {
	err := w.arm(ctx, record, a, operation)
	if errors.Is(err, errExternalChainMovement) {
		return w.stopAction(ctx, record, "ExternalChainMovement", false)
	}
	return err
}
