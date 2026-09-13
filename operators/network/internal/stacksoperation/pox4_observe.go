package stacksoperation

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"math"
	"math/big"
	"time"

	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/libs/stacks/clarity"
	"github.com/cylewitruk-stacks/stacks-k8s/libs/stacks/identity"
	"github.com/cylewitruk-stacks/stacks-k8s/libs/stacks/rpc"
	"github.com/cylewitruk-stacks/stacks-k8s/libs/stacks/signing"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// PoX4Contract is the supported testnet/regtest boot contract.
	PoX4Contract = "ST000000000000000000002AMW42H.pox-4"
	pox4Address  = "ST000000000000000000002AMW42H"
)

// PoX4Node supplies native public state and the shared submission stream.
type PoX4Node interface {
	Node
	ChainView(context.Context) (rpc.ChainView, error)
	PoX(context.Context) (rpc.PoX, error)
	PoXAt(context.Context, string) (rpc.PoX, error)
	AccountAt(context.Context, string, string) (rpc.Account, error)
	ReadOnlyAt(context.Context, string, string, string, string, string, []clarity.Value) (clarity.Value, error)
}

// PoX4Inputs contains complete public policy resolved against the current worker snapshot.
type PoX4Inputs struct {
	// InitialCohort requires the immutable bootstrap cycle; later instances follow current native cycles.
	InitialCohort bool
	// Node is the exact admitted mutation ingress.
	Node PoX4Node
	// Holder and SignerPublicKey must match the mounted role keys.
	Holder, SignerPublicKey string
	// Amount is the exact desired uint128 micro-STX stake.
	Amount *big.Int
	// LockCycles and RenewWhenRemainingCycles bound the declared coverage policy.
	LockCycles, RenewWhenRemainingCycles uint64
	// TargetCycle is the frozen initial enrollment requirement, ignored for later instances.
	TargetCycle uint64
	// EnrollmentCeiling holds initial-cohort enrollment at the frozen observation boundary.
	EnrollmentCeiling uint64
	// Epoch3Height marks native Nakamoto confirmation availability.
	Epoch3Height uint64
}

// pox4State combines one tip-bracketed account, contract and exact reward-set observation.
type pox4State struct {
	info        rpc.Info
	tip         string
	pox         rpc.PoX
	account     rpc.Account
	exists      bool
	first, end  uint64
	observation *api.PoX4EnrollmentObservation
}

// observePoX4 rejects partial or moving-tip reads without issuing any transaction.
func observePoX4(ctx context.Context, input PoX4Inputs, targetCycle uint64, now time.Time) (pox4State, error) {
	var state pox4State
	if input.Node == nil || input.Amount == nil {
		return state, errors.New("incomplete PoX observation inputs")
	}
	before, err := input.Node.ChainView(ctx)
	if err != nil {
		return state, err
	}
	if before.NetworkID != 0x80000000 || !before.FullySynced {
		return state, errors.New("PoX network identity differs")
	}
	state.info = before.Info
	state.tip = before.IndexBlockID
	state.pox, err = input.Node.PoXAt(ctx, before.IndexBlockID)
	if err != nil {
		return state, err
	}
	if state.pox.Contract != PoX4Contract {
		return state, errors.New("PoX-4 is not active")
	}
	state.account, err = input.Node.AccountAt(ctx, input.Holder, before.IndexBlockID)
	if err != nil {
		return state, err
	}
	principal, err := clarity.Principal(input.Holder)
	if err != nil {
		return state, err
	}
	info, err := input.Node.ReadOnlyAt(
		ctx,
		before.IndexBlockID,
		input.Holder,
		pox4Address,
		"pox-4",
		"get-stacker-info",
		[]clarity.Value{principal},
	)
	if err != nil {
		return state, err
	}
	if info.Type != clarity.None {
		tuple, err := optionalTuple(info)
		if err != nil {
			return state, err
		}
		state.exists = true
		first, err := uintField(tuple, "first-reward-cycle")
		if err != nil {
			return state, err
		}
		period, err := uintField(tuple, "lock-period")
		if err != nil || period < 1 || period > 12 || first > math.MaxUint64-period {
			return state, errors.New("invalid PoX lock coverage")
		}
		state.first, state.end = first, first+period
		indexes := tuple.Fields["reward-set-indexes"]
		if indexes.Type != clarity.List || uint64(len(indexes.Items)) != period ||
			tuple.Fields["delegated-to"].Type != clarity.None {
			return state, errors.New("direct PoX state unavailable")
		}
		if targetCycle >= first && targetCycle < state.end {
			index, err := unsigned(indexes.Items[targetCycle-first])
			if err != nil {
				return state, err
			}
			entry, err := input.Node.ReadOnlyAt(
				ctx,
				before.IndexBlockID,
				input.Holder,
				pox4Address,
				"pox-4",
				"get-reward-set-pox-address",
				[]clarity.Value{clarity.Uint(targetCycle), clarity.Uint(index)},
			)
			if err != nil {
				return state, err
			}
			reward, err := optionalTuple(entry)
			if err != nil {
				return state, err
			}
			payout, err := pox4Payout(input.Holder)
			if err != nil {
				return state, err
			}
			encodedPayout, _ := payout.Value()
			actualPayout, err := clarity.Encode(tuple.Fields["pox-addr"])
			if err != nil {
				return state, err
			}
			expectedPayout, _ := clarity.Encode(encodedPayout)
			rewardPayout, err := clarity.Encode(reward.Fields["pox-addr"])
			if err != nil {
				return state, err
			}
			amount := reward.Fields["total-ustx"]
			holder := reward.Fields["stacker"]
			signer := reward.Fields["signer"]
			public, err := hex.DecodeString(input.SignerPublicKey)
			if err != nil {
				return state, err
			}
			if amount.Type != clarity.UInt || amount.Integer == nil || state.account.Locked.Integer == nil ||
				state.account.Locked.Type != clarity.UInt {
				return state, errors.New("incomplete PoX amount")
			}
			matched := state.account.UnlockHeight > before.BurnHeight && bytes.Equal(actualPayout, expectedPayout) &&
				bytes.Equal(rewardPayout, expectedPayout) &&
				amount.Integer.Cmp(input.Amount) == 0 &&
				state.account.Locked.Integer.Cmp(input.Amount) == 0 &&
				holder.Type == clarity.Some &&
				len(holder.Items) == 1 &&
				holder.Items[0].Type == clarity.StandardPrincipal &&
				holder.Items[0].Text == input.Holder &&
				signer.Type == clarity.Buffer &&
				bytes.Equal(signer.Bytes, public)
			if matched {
				state.observation = &api.PoX4EnrollmentObservation{
					Holder:             input.Holder,
					SignerPublicKey:    input.SignerPublicKey,
					AmountMicroSTX:     input.Amount.String(),
					FirstCycle:         first,
					EndCycleExclusive:  state.end,
					TargetCycle:        targetCycle,
					TargetCycleMatched: true,
					StacksTip:          before.IndexBlockID,
					BurnHeight:         before.BurnHeight,
					ObservedAt:         metav1.NewTime(now.UTC()),
				}
			}
		}
	}
	after, err := input.Node.ChainView(ctx)
	if err != nil {
		return pox4State{}, err
	}
	if before != after || state.pox.BurnHeight != before.BurnHeight {
		return pox4State{}, errors.New("PoX canonical tip changed during observation")
	}
	return state, nil
}

// optionalTuple decodes an exact present native Clarity tuple.
func optionalTuple(value clarity.Value) (clarity.Value, error) {
	if value.Type != clarity.Some || len(value.Items) != 1 || value.Items[0].Type != clarity.Tuple {
		return clarity.Value{}, errors.New("PoX optional tuple unavailable")
	}
	return value.Items[0], nil
}

// unsigned validates a native uint64 field without truncation.
func unsigned(value clarity.Value) (uint64, error) {
	if value.Type != clarity.UInt || value.Integer == nil || !value.Integer.IsUint64() {
		return 0, errors.New("invalid PoX uint64")
	}
	return value.Integer.Uint64(), nil
}

// uintField resolves one required unsigned tuple field.
func uintField(tuple clarity.Value, name string) (uint64, error) { return unsigned(tuple.Fields[name]) }

// pox4Payout uses the holder's compressed testnet P2PKH identity as its declared reward address.
func pox4Payout(holder string) (signing.PoXAddress, error) {
	version, hash, err := identity.DecodeAddress(holder)
	if err != nil || version != 26 {
		return signing.PoXAddress{}, errors.New("unsupported PoX holder identity")
	}
	return signing.PoXAddress{Version: 0, HashBytes: append([]byte(nil), hash[:]...)}, nil
}
