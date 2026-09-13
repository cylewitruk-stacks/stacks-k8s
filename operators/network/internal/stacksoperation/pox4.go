package stacksoperation

import (
	"context"
	"errors"
	"math"
	"math/big"
	"time"

	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/libs/stacks/clarity"
	"github.com/cylewitruk-stacks/stacks-k8s/libs/stacks/identity"
	"github.com/cylewitruk-stacks/stacks-k8s/libs/stacks/signing"
	"github.com/cylewitruk-stacks/stacks-k8s/libs/stacks/transaction"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/foundation"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/stacksworker"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// PoX4Fee is the explicit micro-STX fee for one direct enrollment or extension.
const PoX4Fee uint64 = 3000

// pox4Goal retains the exact public outcome selected before the sole submission attempt.
type pox4Goal struct {
	input                     PoX4Inputs
	kind                      api.PostconditionKind
	first, end, target, nonce uint64
}

// PoX4Role maintains direct PoX-4 stake in one surviving worker process.
type PoX4Role struct {
	// Resolve reads current complete public policy and frozen bootstrap requirements.
	Resolve func(context.Context, stacksworker.Snapshot) (PoX4Inputs, error)
	// Now injects observation time for deterministic tests.
	Now                                func() time.Time
	holderKey, signerKey, signerPublic string
	stream                             NonceStream
	goal                               *pox4Goal
	applied                            string
	initialDone                        bool
	failed                             bool
	current                            *api.PoX4EnrollmentObservation
	cached                             *PoX4Inputs
	cachedDigest                       string
}

// NewPoX4Role validates the two mounted keys against their independently resolved identities.
func NewPoX4Role(holderKey, holder, signerKey, signerPublic string) (*PoX4Role, error) {
	compressed, err := identity.CompressedPrivateKey(holderKey)
	if err != nil {
		return nil, errors.New("invalid PoX holder key")
	}
	h, err := identity.FromPrivate(compressed)
	if err != nil || h.Address != holder {
		return nil, errors.New("PoX holder key differs")
	}
	s, err := identity.FromPrivate(signerKey)
	if err != nil || s.PublicKey != signerPublic {
		return nil, errors.New("PoX consensus key differs")
	}
	return &PoX4Role{holderKey: compressed, signerKey: signerKey, signerPublic: signerPublic, stream: NonceStream{Address: holder}}, nil
}

// now reads the configured wall clock.
func (r *PoX4Role) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

// Step observes pending operations even while paused; no ambiguous operation is replayed.
func (r *PoX4Role) Step(ctx context.Context, snapshot stacksworker.Snapshot) (stacksworker.RoleResult, error) {
	r.current = nil
	if r.failed {
		return r.result("PoX4OperationFailed"), nil
	}
	if r.goal != nil {
		reason := r.observeGoal(ctx)
		return r.result(reason), nil
	}
	if r.Resolve == nil || snapshot.Participant == nil || snapshot.Participant.Status.Admission == nil {
		return r.result("PolicyUnavailable"), nil
	}
	input, snapshot, err := r.inputs(ctx, snapshot)
	if err != nil {
		return r.result("DependenciesUnavailable"), nil
	}
	if input.Node == nil || input.Holder != r.stream.Address || input.SignerPublicKey != r.signerPublic || input.Amount == nil || input.Amount.Sign() <= 0 || input.Amount.BitLen() > 128 || input.LockCycles < 2 || input.LockCycles > 12 || input.RenewWhenRemainingCycles < 1 || input.RenewWhenRemainingCycles >= input.LockCycles || input.InitialCohort && input.EnrollmentCeiling == 0 {
		r.cached = nil
		return r.result("InvalidPolicy"), nil
	}
	r.applied = snapshot.Participant.Status.Admission.PolicyDigest
	if !snapshot.CachedApplied && snapshot.RememberApplied != nil {
		snapshot.RememberApplied(r.applied)
	}
	copy := clonePoX4Inputs(input)
	r.cached, r.cachedDigest = &copy, r.applied
	target := input.TargetCycle
	if r.initialDone || !input.InitialCohort {
		pox, err := input.Node.PoX(ctx)
		if err != nil {
			return r.result("PoXObservationUnavailable"), nil
		}
		if pox.Contract != PoX4Contract {
			return r.result("AwaitingPoX5"), nil
		}
		if pox.RewardCycle == math.MaxUint64 {
			return r.result("InvalidPoXCycle"), nil
		}
		if !input.InitialCohort || pox.RewardCycle > target {
			target = pox.RewardCycle
		}
	}
	state, err := observePoX4(ctx, input, target, r.now())
	if err != nil {
		return r.result("PoXObservationUnavailable"), nil
	}
	r.current = state.observation
	if state.observation != nil && (!input.InitialCohort || target == input.TargetCycle) {
		r.initialDone = true
	}
	if snapshot.Paused {
		return r.result(reasonPaused), nil
	}
	if !state.exists {
		if r.initialDone {
			r.failed = true
			return r.result("PoX4LockExpired"), nil
		}
		if state.pox.RewardCycle == math.MaxUint64 || input.InitialCohort && state.pox.RewardCycle+1 > input.TargetCycle {
			r.failed = true
			return r.result("BootstrapWindowMissed"), nil
		}
		first := state.pox.RewardCycle + 1
		if first > math.MaxUint64-input.LockCycles || input.InitialCohort && first+input.LockCycles <= input.TargetCycle {
			return r.result("InvalidBootstrapWindow"), nil
		}
		if input.InitialCohort && state.info.BurnHeight >= input.EnrollmentCeiling {
			return r.result("BootstrapEnrollmentHeld"), nil
		}
		if state.account.Locked.Integer == nil || state.account.Locked.Integer.Sign() != 0 {
			return r.result("ExistingAccountLock"), nil
		}
		if state.pox.MinThreshold.Integer == nil || input.Amount.Cmp(state.pox.MinThreshold.Integer) < 0 {
			return r.result("StakeBelowThreshold"), nil
		}
		if !input.InitialCohort {
			target = first
		}
		return r.offer(ctx, snapshot, input, state, api.PostconditionPoX4Enrollment, input.LockCycles, first, first+input.LockCycles, target)
	}
	if state.observation == nil {
		return r.result("EnrollmentStateMismatch"), nil
	}
	if !r.initialDone {
		return r.result("AwaitingRequiredCycle"), nil
	}
	if state.info.BurnHeight < input.Epoch3Height {
		return r.result(reasonPoX4EnrollmentObserved), nil
	}
	if state.pox.RewardCycle >= state.end {
		r.failed = true
		return r.result("PoX4LockExpired"), nil
	}
	remaining := state.end - state.pox.RewardCycle
	if remaining > input.RenewWhenRemainingCycles || state.pox.BlocksUntilPrepare <= 0 {
		return r.result(reasonPoX4EnrollmentObserved), nil
	}
	first := state.first
	if state.pox.RewardCycle > first {
		first = state.pox.RewardCycle
	}
	if state.end-first >= input.LockCycles {
		return r.result(reasonPoX4EnrollmentObserved), nil
	}
	extend := input.LockCycles - (state.end - first)
	if state.end > math.MaxUint64-extend {
		return r.result("InvalidPoXCycle"), nil
	}
	return r.offer(ctx, snapshot, input, state, api.PostconditionPoX4Extension, extend, first, state.end+extend, state.end)
}

// offer constructs explicit SIP-018 authorization and sends through the shared nonce stream once.
func (r *PoX4Role) offer(ctx context.Context, snapshot stacksworker.Snapshot, input PoX4Inputs, state pox4State, kind api.PostconditionKind, cycles, first, end, target uint64) (stacksworker.RoleResult, error) {
	payout, err := pox4Payout(input.Holder)
	if err != nil {
		return r.result("InvalidHolder"), nil
	}
	amount, err := clarity.Uint128(input.Amount.String())
	if err != nil {
		return r.result("InvalidAmount"), nil
	}
	required := new(big.Int).SetUint64(PoX4Fee)
	if kind == api.PostconditionPoX4Enrollment {
		required.Add(required, input.Amount)
	}
	var nonceUsed uint64
	reason, err := r.stream.Offer(ctx, r.now, input.Node, required, snapshot.Authorize, func(nonce uint64) (transaction.Transaction, error) {
		nonceUsed = nonce
		topic := "stack-stx"
		if kind == api.PostconditionPoX4Extension {
			topic = "stack-extend"
		}
		signature, err := signing.PoX(r.signerKey, signing.PoXAuthorization{Address: payout, RewardCycle: state.pox.RewardCycle, Topic: topic, Period: cycles, MaxAmount: amount, AuthID: clarity.Uint(nonce), ChainID: 0x80000000})
		if err != nil {
			return transaction.Transaction{}, err
		}
		authorization := signing.SignerArguments{Signature: signature[:], PublicKey: r.signerPublic, MaxAmount: amount, AuthID: clarity.Uint(nonce)}
		var args []clarity.Value
		if kind == api.PostconditionPoX4Enrollment {
			args, err = signing.StackSTXArguments(amount, payout, state.pox.BurnHeight, cycles, authorization)
		} else {
			args, err = signing.StackExtendArguments(payout, cycles, authorization)
		}
		if err != nil {
			return transaction.Transaction{}, err
		}
		return transaction.Call(transaction.Options{Version: transaction.Testnet, ChainID: 0x80000000, Nonce: nonce, Fee: PoX4Fee, PostConditionMode: transaction.Deny, PrivateKey: r.holderKey}, pox4Address, "pox-4", topic, args)
	})
	if r.stream.Pending() != 0 {
		input.Amount = new(big.Int).Set(input.Amount)
		r.goal = &pox4Goal{input: input, kind: kind, first: first, end: end, target: target, nonce: nonceUsed}
	}
	if err != nil {
		return r.result(reason), nil
	}
	return r.result(reason), nil
}

// observeGoal accepts exact canonical inclusion or the explicitly distinct legacy state proof.
func (r *PoX4Role) observeGoal(ctx context.Context) string {
	goal := r.goal
	reason, _ := r.stream.Observe(ctx, r.now())
	if reason == "ExecutionRejected" {
		r.failed = true
		return reason
	}
	state, err := observePoX4(ctx, goal.input, goal.target, r.now())
	if err != nil {
		return "PoXObservationUnavailable"
	}
	r.current = state.observation
	if state.observation == nil || state.first != goal.first || state.end != goal.end {
		return "AwaitingPoXPostcondition"
	}
	if r.stream.Pending() != 0 {
		observation := *state.observation
		observation.ObservedAt = metav1.Time{}
		evidence := api.TransactionPostcondition{TxID: r.stream.pending.transaction.TxID, Kind: goal.kind, StateDigest: foundation.Digest(observation), StacksTip: state.tip, ObservedAt: metav1.NewTime(r.now().UTC())}
		reason, err = r.stream.SettleObserved(evidence.TxID, state.account.Nonce, evidence)
		if err != nil || r.stream.Pending() != 0 {
			return reason
		}
	}
	if goal.kind == api.PostconditionPoX4Enrollment {
		r.initialDone = true
	}
	r.goal = nil
	if reason == reasonIdle {
		return reasonPoX4EnrollmentObserved
	}
	return reason
}

// Drain performs only native observations until completion or the framework's finite shutdown bound.
func (r *PoX4Role) Drain(ctx context.Context, _ stacksworker.Snapshot) (stacksworker.DrainResult, error) {
	r.current = nil
	if r.goal != nil {
		r.observeGoal(ctx)
	}
	pending := r.pending()
	return stacksworker.DrainResult{Done: pending == 0, Settled: pending == 0, Pending: pending, Transactions: r.stream.Facts(), PoX4: r.current.DeepCopy()}, nil
}

// pending includes an operation awaiting public state after its exact transaction was included.
func (r *PoX4Role) pending() int32 {
	if r.goal != nil {
		return 1
	}
	return r.stream.Pending()
}

// result snapshots public facts for retry-safe publication.
func (r *PoX4Role) result(reason string) stacksworker.RoleResult {
	return stacksworker.RoleResult{Transactions: r.stream.Facts(), PoX4: r.current.DeepCopy(), AppliedPolicyDigest: r.applied, Pending: r.pending(), Reason: reason, Blocked: poxBlocked(reason, r.pending()), Failed: r.failed, RequeueAfter: time.Second}
}
