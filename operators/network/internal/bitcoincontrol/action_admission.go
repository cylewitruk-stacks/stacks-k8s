package bitcoincontrol

import (
	"context"
	"fmt"
	"time"

	action "github.com/cylewitruk-stacks/stacks-k8s/apis/network/actions/v1alpha2"
	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/foundation"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// authorizeMutation keeps ordinary sends and bounded finite compensation on separate control checks.
func (w *Worker) authorizeMutation(ctx context.Context, record *bitcoin.BitcoinExecution, operation bitcoin.BitcoinArmedRPC) (admitted, error) {
	if operation.Action == nil {
		return w.authorize(ctx, record)
	}
	state := record.Status.Action
	if state == nil || state.Request != *operation.Action || record.Status.Reservation == nil || *record.Status.Reservation != state.Request || state.CleanupUnsafe {
		return admitted{}, fmt.Errorf("finite action reservation unavailable")
	}
	cleanup := operation.Method == bitcoin.RPCReconsiderBlock
	if !cleanup && (state.Generation != nil && !w.Input.ActionsEnabled || state.Reorganization != nil && !w.Input.ReorganizationEnabled) {
		return admitted{}, fmt.Errorf("finite action kind disabled")
	}
	a, err := w.resolveActor(ctx, record, cleanup)
	if err != nil {
		return admitted{}, err
	}
	if !equality.Semantic.DeepEqual(a.target, state.Runtime) {
		return admitted{}, fmt.Errorf("finite action runtime differs")
	}
	if !cleanup {
		if err := foundation.ValidateAdmissionEligibility(ctx, w.Reader, a.participant); err != nil {
			return admitted{}, err
		}
		if err := foundation.ValidatePublicParticipantAdmission(ctx, w.Reader, a.root, a.participant); err != nil {
			return admitted{}, err
		}
	}
	if cleanup {
		if state.Reorganization == nil || !state.InvalidationAcknowledged || state.CleanupAcknowledged || operation.BlockHash != state.InvalidatedHash || !w.Now().Before(state.ExpiresAt.Add(30*time.Second)) {
			return admitted{}, fmt.Errorf("compensation authority unavailable")
		}
		return a, nil
	}
	if a.root.Spec.Operation == api.NetworkOperationPaused || state.StopReason != "" || state.EffectUncertain || !w.Now().Before(state.ExpiresAt.Time) {
		return admitted{}, fmt.Errorf("finite action stopped")
	}
	view, same, err := w.readAction(ctx, record)
	if err != nil {
		return admitted{}, err
	}
	if !same || view.object.GetDeletionTimestamp() != nil || action.IsTerminalPhase(view.status.Phase) || !actionAdmissionAcknowledged(view, record) {
		return admitted{}, fmt.Errorf("finite request admission unavailable")
	}
	if operation.Method == bitcoin.RPCInvalidateBlock {
		if state.Reorganization == nil || state.InvalidationAcknowledged || operation.BlockHash != state.InvalidatedHash {
			return admitted{}, fmt.Errorf("invalidation already consumed")
		}
		if err := w.reorganizationPreflight(ctx, a, state, false); err != nil {
			return admitted{}, err
		}
	} else if operation.Method == bitcoin.RPCGenerate {
		count := int32(0)
		address := ""
		if state.Generation != nil {
			count = state.Generation.Count
			address = state.Generation.Address
		} else if state.Reorganization != nil {
			count = state.Reorganization.Depth + 1
			address = state.Reorganization.Address
			if !state.InvalidationAcknowledged || state.CleanupAcknowledged {
				return admitted{}, fmt.Errorf("replacement generation unavailable")
			}
		}
		if state.BlocksGenerated >= count || operation.Address != address || state.NextDispatchAt != nil && w.Now().Before(state.NextDispatchAt.Time) {
			return admitted{}, fmt.Errorf("finite generation quota or cadence differs")
		}
		if state.Reorganization != nil {
			if err := w.reorganizationPreflight(ctx, a, state, true); err != nil {
				return admitted{}, err
			}
		}
	} else {
		return admitted{}, fmt.Errorf("unsupported finite method")
	}
	height, _, err := w.chain(ctx, a.target.Endpoint)
	if err != nil {
		return admitted{}, err
	}
	if err := w.actionCeiling(ctx, a, height); err != nil {
		return admitted{}, err
	}
	if operation.Method == bitcoin.RPCGenerate {
		if err := w.RPC.Check(ctx, a.target.Endpoint, operation.Address); err != nil {
			return admitted{}, err
		}
	}
	return a, nil
}

// accountActionReceipt updates finite progress in the same CAS that consumes its Armed slot.
func accountActionReceipt(record *bitcoin.BitcoinExecution, receipt *bitcoin.BitcoinRPCReceipt) error {
	state := record.Status.Action
	if state == nil || receipt.Request.Action == nil || state.Request != *receipt.Request.Action {
		return fmt.Errorf("finite receipt owner differs")
	}
	state.LastDispatchID = receipt.Request.ID
	completed := receipt.ReceivedAt.Time
	if !completed.Equal(completed.Truncate(time.Second)) {
		completed = completed.Truncate(time.Second).Add(time.Second)
	}
	state.LastCompletedAt = &metav1.Time{Time: completed.UTC()}
	switch receipt.Request.Method {
	case bitcoin.RPCGenerate:
		if !hashValid(receipt.BlockHash) {
			return fmt.Errorf("finite generation receipt malformed")
		}
		state.BlocksGenerated++
		state.LastBlockHash = receipt.BlockHash
		record.Status.BlocksGenerated++
		delay := time.Second
		if state.Generation != nil {
			if state.BlocksGenerated >= state.Generation.Count {
				state.NextDispatchAt = nil
				return nil
			}
			var err error
			delay, err = actionDelay(state.Generation.Cadence, state.Generation.Count, state.BlocksGenerated, receipt.Request.ID)
			if err != nil {
				return err
			}
		} else {
			state.ReplacementBlockHashes = append(state.ReplacementBlockHashes, receipt.BlockHash)
		}
		due := receipt.ReceivedAt.Add(delay)
		if delay > 0 && !due.Equal(due.Truncate(time.Second)) {
			due = due.Truncate(time.Second).Add(time.Second)
		}
		state.NextDispatchAt = &metav1.Time{Time: due.UTC()}
	case bitcoin.RPCInvalidateBlock:
		state.InvalidationAcknowledged = true
	case bitcoin.RPCReconsiderBlock:
		state.CleanupAcknowledged = true
	default:
		return fmt.Errorf("unsupported finite receipt")
	}
	return nil
}
