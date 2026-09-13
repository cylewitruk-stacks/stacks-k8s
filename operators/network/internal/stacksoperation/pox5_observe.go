package stacksoperation

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"math/big"
	"time"

	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/libs/stacks/clarity"
	"github.com/cylewitruk-stacks/stacks-k8s/libs/stacks/rpc"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/protocolcontracts"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// PoX5Contract is the supported epoch-4 boot contract.
const PoX5Contract = pox4Address + ".pox-5"

// PoX5Node adds canonical contract-source observation to the direct stacking interface.
type PoX5Node interface {
	PoX4Node
	SourceAt(context.Context, string, string, string) (string, bool, error)
}

// PoX5Inputs contains public direct-manager policy and frozen activation requirements.
type PoX5Inputs struct {
	// InitialCohort requires the immutable bootstrap cycle; later instances follow current native cycles.
	InitialCohort bool
	// Node is the admitted native ingress.
	Node PoX5Node
	// Holder, Administrator and SignerPublicKey must match immutable mounted identities.
	Holder, Administrator, SignerPublicKey string
	// Amount is the declared uint128 micro-STX stake.
	Amount *big.Int
	// LockCycles and RenewWhenRemainingCycles define the maintenance horizon.
	LockCycles, RenewWhenRemainingCycles uint64
	// TargetCycle and Epoch4Height are frozen bootstrap requirements.
	TargetCycle, Epoch4Height uint64
}

// pox5State binds every management, account and cycle read to one canonical index block.
type pox5State struct {
	view                                                        rpc.ChainView
	pox                                                         rpc.PoX
	holder, administrator                                       rpc.Account
	source                                                      string
	sourceFound, registered, granted, exists, conflict, prepare bool
	first, end                                                  uint64
	amount                                                      *big.Int
	observation                                                 *api.PoX5EnrollmentObservation
}

// manager returns the sole supported administrator-qualified contract name.
func (i PoX5Inputs) manager() string { return i.Administrator + ".direct-signer" }

// observePoX5 distinguishes known absence from unavailable reads and rejects fork changes.
func observePoX5(ctx context.Context, input PoX5Inputs, target uint64, now time.Time) (pox5State, error) {
	var s pox5State
	var err error
	if input.Node == nil || input.Amount == nil {
		return s, errors.New("PoX5 inputs unavailable")
	}
	s.view, err = input.Node.ChainView(ctx)
	if err != nil {
		return s, err
	}
	if !s.view.FullySynced || s.view.NetworkID != 0x80000000 || s.view.BurnHeight < input.Epoch4Height {
		return s, errors.New("PoX5 epoch unavailable")
	}
	tip := s.view.IndexBlockID
	s.pox, err = input.Node.PoXAt(ctx, tip)
	if err != nil {
		return s, err
	}
	if s.pox.Contract != PoX5Contract || s.pox.BurnHeight != s.view.BurnHeight {
		return s, errors.New("PoX5 native boot state unavailable")
	}
	s.holder, err = input.Node.AccountAt(ctx, input.Holder, tip)
	if err != nil {
		return s, err
	}
	s.administrator = s.holder
	if input.Administrator != input.Holder {
		s.administrator, err = input.Node.AccountAt(ctx, input.Administrator, tip)
		if err != nil {
			return s, err
		}
	}
	s.source, s.sourceFound, err = input.Node.SourceAt(ctx, input.Administrator, "direct-signer", tip)
	if err != nil {
		return s, err
	}
	expected, err := protocolcontracts.DirectManager(input.Holder)
	if err != nil {
		return s, err
	}
	s.conflict = s.sourceFound && s.source != expected
	manager, err := clarity.Principal(input.manager())
	if err != nil {
		return s, err
	}
	holder, err := clarity.Principal(input.Holder)
	if err != nil {
		return s, err
	}
	key, err := hex.DecodeString(input.SignerPublicKey)
	if err != nil || len(key) != 33 {
		return s, errors.New("PoX5 key unavailable")
	}
	read := func(method string, args ...clarity.Value) (clarity.Value, error) {
		return input.Node.ReadOnlyAt(ctx, tip, input.Holder, pox4Address, "pox-5", method, args)
	}
	prepare, err := read("is-in-prepare-phase", clarity.Uint(s.pox.RewardCycle))
	if err != nil || prepare.Type != clarity.True && prepare.Type != clarity.False {
		return s, errors.New("PoX5 contract edit window unavailable")
	}
	s.prepare = prepare.Type == clarity.True
	signer, err := read("get-signer-info", manager)
	if err != nil {
		return s, err
	}
	if signer.Type != clarity.None {
		if signer.Type != clarity.Some || len(signer.Items) != 1 || signer.Items[0].Type != clarity.Buffer {
			return s, errors.New("PoX5 signer state malformed")
		}
		s.registered = bytes.Equal(signer.Items[0].Bytes, key)
		s.conflict = s.conflict || !s.registered
	}
	grant, err := read("verify-signer-key-grant", manager, clarity.Value{Type: clarity.Buffer, Bytes: key})
	if err != nil {
		return s, err
	}
	if grant.Type != clarity.ResponseOK && grant.Type != clarity.ResponseErr {
		return s, errors.New("PoX5 grant state malformed")
	}
	s.granted = grant.Type == clarity.ResponseOK && len(grant.Items) == 1 && grant.Items[0].Type == clarity.True
	stake, err := read("get-staker-info", holder)
	if err != nil {
		return s, err
	}
	if stake.Type != clarity.None {
		st, err := optionalTuple(stake)
		if err != nil {
			return s, err
		}
		s.exists = true
		s.first, err = uintField(st, "first-reward-cycle")
		if err != nil {
			return s, err
		}
		cycles, err := uintField(st, "num-cycles")
		if err != nil || cycles < 1 || s.first > math.MaxUint64-cycles {
			return s, errors.New("PoX5 coverage malformed")
		}
		s.end = s.first + cycles
		amount := st.Fields["amount-ustx"]
		if amount.Type != clarity.UInt || amount.Integer == nil || amount.Integer.Sign() <= 0 ||
			amount.Integer.BitLen() > 128 {
			return s, errors.New("PoX5 amount malformed")
		}
		s.amount = new(big.Int).Set(amount.Integer)
		s.conflict = s.conflict || st.Fields["signer"].Type != clarity.ContractPrincipal ||
			st.Fields["signer"].Text != input.manager()
		if target >= s.first && target < s.end {
			membership, err := read("get-signer-cycle-membership", holder, clarity.Uint(target))
			if err != nil {
				return s, err
			}
			member, err := optionalTuple(membership)
			if err != nil {
				return s, err
			}
			delegated, err := read("get-amount-delegated-for-signer", manager, clarity.Uint(target))
			if err != nil {
				return s, err
			}
			included, err := read("signer-set-contains-for-cycle", manager, clarity.Uint(target))
			if err != nil {
				return s, err
			}
			unlock, err := read("reward-cycle-to-burn-height", clarity.Uint(s.end))
			if err != nil {
				return s, err
			}
			unlockHeight, err := unsigned(unlock)
			if err != nil {
				return s, err
			}
			memberAmount := member.Fields["amount-ustx"]
			matched := s.sourceFound && !s.conflict && s.registered && s.granted && s.amount.Cmp(input.Amount) == 0 &&
				s.holder.Locked.Integer != nil &&
				s.holder.Locked.Integer.Cmp(input.Amount) == 0 &&
				s.holder.UnlockHeight == unlockHeight &&
				unlockHeight > s.view.BurnHeight &&
				member.Fields["signer"].Type == clarity.ContractPrincipal &&
				member.Fields["signer"].Text == input.manager() &&
				sameAmount(memberAmount, input.Amount) &&
				sameAmount(delegated, input.Amount) &&
				included.Type == clarity.True
			if matched {
				s.observation = &api.PoX5EnrollmentObservation{
					Holder:                  input.Holder,
					Manager:                 input.manager(),
					ManagerSourceDigest:     sourceDigest(expected),
					SignerPublicKey:         input.SignerPublicKey,
					AmountMicroSTX:          input.Amount.String(),
					DelegatedAmountMicroSTX: delegated.Integer.String(),
					FirstCycle:              s.first,
					EndCycleExclusive:       s.end,
					TargetCycle:             target,
					TargetCycleMatched:      true,
					UnlockHeight:            unlockHeight,
					StacksTip:               tip,
					BurnHeight:              s.view.BurnHeight,
					ObservedAt:              metav1.NewTime(now.UTC()),
				}
			}
		}
	}
	after, err := input.Node.ChainView(ctx)
	if err != nil {
		return pox5State{}, err
	}
	if after != s.view {
		return pox5State{}, errors.New("PoX5 canonical tip changed")
	}
	return s, nil
}

// sameAmount compares a full-width native unsigned amount without narrowing.
func sameAmount(value clarity.Value, expected *big.Int) bool {
	return value.Type == clarity.UInt && value.Integer != nil && value.Integer.Cmp(expected) == 0
}

// sourceDigest identifies raw source bytes independently of JSON encoding.
func sourceDigest(source string) string {
	return fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(source)))
}
