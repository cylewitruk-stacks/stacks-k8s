package stacksoperation

import (
	"context"
	"encoding/hex"
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
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/protocolcontracts"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/stacksworker"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// pox5Goal retains the exact operation selected before its sole send attempt.
type pox5Goal struct {
	input              PoX5Inputs
	kind               api.PostconditionKind
	first, end, target uint64
	administrator      bool
}

// StackerRole preserves account nonce ownership through PoX-4 and PoX-5 maintenance.
type StackerRole struct {
	// ResolvePoX4 and ResolvePoX5 read complete current public policy.
	ResolvePoX4 func(context.Context, stacksworker.Snapshot) (PoX4Inputs, error)
	ResolvePoX5 func(context.Context, stacksworker.Snapshot) (PoX5Inputs, error)
	// Now injects observation time for tests.
	Now                               func() time.Time
	legacy                            *PoX4Role
	administrator                     *NonceStream
	administratorKey                  string
	goal                              *pox5Goal
	applied                           string
	initialDone, failed, transitioned bool
	current                           *api.PoX5EnrollmentObservation
	cached                            *PoX5Inputs
	cachedDigest                      string
}

// NewStackerRole verifies all three mounted identities before creating any nonce stream.
// Identical holder and administrator addresses share one stream, including pending PoX-4 work.
func NewStackerRole(
	holderKey, holder, signerKey, signerPublic, administratorKey, administrator string,
) (*StackerRole, error) {
	legacy, err := NewPoX4Role(holderKey, holder, signerKey, signerPublic)
	if err != nil {
		return nil, err
	}
	key, err := identity.CompressedPrivateKey(administratorKey)
	if err != nil {
		return nil, errors.New("invalid administrator key")
	}
	admin, err := identity.FromPrivate(key)
	if err != nil || admin.Address != administrator {
		return nil, errors.New("administrator key differs")
	}
	r := &StackerRole{legacy: legacy, administratorKey: key, administrator: &NonceStream{Address: administrator}}
	if administrator == holder {
		r.administrator = &legacy.stream
	}
	return r, nil
}

// now reads the injected or wall clock.
func (r *StackerRole) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

// Step observes old sends before routing any new operation across the protocol transition.
func (r *StackerRole) Step(ctx context.Context, snapshot stacksworker.Snapshot) (stacksworker.RoleResult, error) {
	r.current = nil
	r.legacy.Now, r.legacy.Resolve = r.Now, r.ResolvePoX4
	if r.failed {
		return r.result(reasonPoX5OperationFailed), nil
	}
	if !r.transitioned && r.legacy.pending() != 0 {
		result, err := r.legacyStep(ctx, snapshot)
		// Exact successful inclusion can close the old operation after PoX-4 state disappears.
		// No PoX-4 postcondition is claimed by this transition acknowledgement.
		if r.legacy.goal != nil && r.legacy.stream.Pending() == 0 && !r.legacy.failed {
			pox, e := r.legacy.goal.input.Node.PoX(ctx)
			inclusion := r.legacy.stream.facts.LastInclusion
			if e == nil && pox.Contract == PoX5Contract && inclusion != nil && inclusion.Success &&
				inclusion.TxID == r.legacy.stream.facts.LastTxID {
				r.legacy.goal = nil
				result = r.legacy.result(reasonPoX4InclusionObservedAtTransition)
			}
		}
		return result, err
	}
	if r.goal != nil {
		return r.result(r.observeGoal(ctx)), nil
	}
	if r.ResolvePoX5 == nil || snapshot.Participant == nil || snapshot.Participant.Status.Admission == nil {
		return r.result(reasonPolicyUnavailable), nil
	}
	input, snapshot, err := r.inputs(ctx, snapshot)
	if err != nil {
		//nolint:nilerr // Publish the reason and retry without failing the worker session.
		return r.result(reasonDependenciesUnavailable), nil
	}
	if input.Node == nil || input.Holder != r.legacy.stream.Address || input.Administrator != r.administrator.Address ||
		input.SignerPublicKey != r.legacy.signerPublic ||
		input.Amount == nil ||
		input.Amount.Sign() <= 0 ||
		input.Amount.BitLen() > 128 ||
		input.LockCycles < 2 ||
		input.LockCycles > 12 ||
		input.RenewWhenRemainingCycles < 1 ||
		input.RenewWhenRemainingCycles >= input.LockCycles ||
		input.Epoch4Height == 0 {
		r.cached = nil
		return r.result(reasonInvalidPolicy), nil
	}
	clonedInput := clonePoX5Inputs(input)
	r.cached, r.cachedDigest = &clonedInput, snapshot.Participant.Status.Admission.PolicyDigest
	pox, err := input.Node.PoX(ctx)
	if err != nil {
		//nolint:nilerr // Missing observation holds the policy; it does not fail the worker.
		return r.result(reasonPoXObservationUnavailable), nil
	}
	if pox.Contract == PoX4Contract && !r.transitioned {
		return r.legacyStep(ctx, snapshot)
	}
	if pox.Contract != PoX5Contract {
		return r.result(reasonAwaitingPoX5), nil
	}
	r.transitioned = true
	r.applied = snapshot.Participant.Status.Admission.PolicyDigest
	if !snapshot.CachedApplied && snapshot.RememberApplied != nil {
		snapshot.RememberApplied(r.applied)
	}
	target := input.TargetCycle
	if !input.InitialCohort || r.initialDone && pox.RewardCycle > target {
		target = pox.RewardCycle
	}
	state, err := observePoX5(ctx, input, target, r.now())
	if err != nil {
		//nolint:nilerr // Missing observation holds the policy; it does not fail the worker.
		return r.result(reasonPoXObservationUnavailable), nil
	}
	r.current = state.observation
	if state.conflict {
		return r.result(reasonConflict), nil
	}
	if state.observation != nil && (!input.InitialCohort || target == input.TargetCycle) {
		r.initialDone = true
	}
	if input.InitialCohort && !r.initialDone && state.pox.RewardCycle >= input.TargetCycle {
		r.failed = true
		return r.result(reasonBootstrapWindowMissed), nil
	}
	if snapshot.Paused {
		return r.result(reasonPaused), nil
	}
	if !state.sourceFound {
		return r.offer(ctx, snapshot, input, state, api.PostconditionManagerDeployment, 0, 0, target)
	}
	if !state.registered || !state.granted {
		return r.offer(ctx, snapshot, input, state, api.PostconditionSignerRegistration, 0, 0, target)
	}
	if state.prepare {
		return r.result(reasonAwaitingMaintenanceWindow), nil
	}
	if !state.exists {
		if state.pox.RewardCycle == math.MaxUint64 || state.pox.RewardCycle+1 > math.MaxUint64-input.LockCycles {
			return r.result(reasonInvalidPoXCycle), nil
		}
		first := state.pox.RewardCycle + 1
		if input.InitialCohort && !r.initialDone &&
			(first > input.TargetCycle || first+input.LockCycles <= input.TargetCycle) {
			r.failed = true
			return r.result(reasonBootstrapWindowMissed), nil
		}
		if state.holder.Locked.Integer == nil || state.holder.Locked.Integer.Sign() != 0 {
			return r.result(reasonAwaitingPoX4Unlock), nil
		}
		if r.initialDone || !input.InitialCohort {
			target = first
		}
		return r.offer(
			ctx,
			snapshot,
			input,
			state,
			api.PostconditionPoX5Enrollment,
			first,
			first+input.LockCycles,
			target,
		)
	}
	if state.amount.Cmp(input.Amount) != 0 {
		return r.result(reasonAwaitingUnlockForAmountChange), nil
	}
	if state.observation == nil {
		return r.result(reasonEnrollmentStateMismatch), nil
	}
	if state.pox.RewardCycle >= state.end || state.pox.RewardCycle == math.MaxUint64 {
		return r.result(reasonAwaitingUnlock), nil
	}
	remaining := state.end - state.pox.RewardCycle
	if remaining > input.RenewWhenRemainingCycles {
		return r.result(reasonPoX5EnrollmentObserved), nil
	}
	firstNext := state.pox.RewardCycle + 1
	if firstNext > math.MaxUint64-input.LockCycles {
		return r.result(reasonInvalidPoXCycle), nil
	}
	end := firstNext + input.LockCycles
	if end <= state.end {
		return r.result(reasonPoX5EnrollmentObserved), nil
	}
	return r.offer(ctx, snapshot, input, state, api.PostconditionPoX5Extension, state.first, end, state.end)
}

// offer constructs a single explicit manager or direct-stake operation.
func (r *StackerRole) offer(
	ctx context.Context,
	snapshot stacksworker.Snapshot,
	input PoX5Inputs,
	state pox5State,
	kind api.PostconditionKind,
	first, end, target uint64,
) (stacksworker.RoleResult, error) {
	admin := kind == api.PostconditionManagerDeployment || kind == api.PostconditionSignerRegistration
	stream, key := &r.legacy.stream, r.legacy.holderKey
	if admin {
		stream, key = r.administrator, r.administratorKey
	}
	required := new(big.Int).SetUint64(PoX4Fee)
	if kind == api.PostconditionPoX5Enrollment {
		required.Add(required, input.Amount)
	}
	reason, _ := stream.Offer(
		ctx,
		r.now,
		input.Node,
		required,
		snapshot.Authorize,
		func(nonce uint64) (transaction.Transaction, error) {
			options := transaction.Options{
				Version:           transaction.Testnet,
				ChainID:           0x80000000,
				Nonce:             nonce,
				Fee:               PoX4Fee,
				PostConditionMode: transaction.Deny,
				PrivateKey:        key,
			}
			if kind == api.PostconditionManagerDeployment {
				source, err := protocolcontracts.DirectManager(input.Holder)
				if err != nil {
					return transaction.Transaction{}, err
				}
				return transaction.Deploy(options, "direct-signer", source, 6)
			}
			manager, err := clarity.Principal(input.manager())
			if err != nil {
				return transaction.Transaction{}, err
			}
			if kind == api.PostconditionSignerRegistration {
				sig, err := signing.SignerGrant(r.legacy.signerKey, input.manager(), clarity.Uint(nonce), 0x80000000)
				if err != nil {
					return transaction.Transaction{}, err
				}
				public, err := hex.DecodeString(input.SignerPublicKey)
				if err != nil {
					return transaction.Transaction{}, err
				}
				return transaction.Call(
					options,
					input.Administrator,
					"direct-signer",
					"register-self",
					[]clarity.Value{
						manager,
						{Type: clarity.Buffer, Bytes: public},
						clarity.Uint(nonce),
						{Type: clarity.Buffer, Bytes: sig[:]},
					},
				)
			}
			// The selected profile explicitly permits the direct native STX lock.
			options.PostConditionMode = transaction.Allow
			if kind == api.PostconditionPoX5Enrollment {
				amount, err := clarity.Uint128(input.Amount.String())
				if err != nil {
					return transaction.Transaction{}, err
				}
				return transaction.Call(
					options,
					pox4Address,
					"pox-5",
					"stake",
					[]clarity.Value{
						manager,
						amount,
						clarity.Uint(end - first),
						clarity.Uint(state.pox.BurnHeight),
						{Type: clarity.None},
					},
				)
			}
			return transaction.Call(
				options,
				pox4Address,
				"pox-5",
				"stake-update",
				[]clarity.Value{manager, manager, clarity.Uint(end - state.end), clarity.Uint(0), {Type: clarity.None}},
			)
		},
	)
	if stream.Pending() != 0 {
		input.Amount = new(big.Int).Set(input.Amount)
		r.goal = &pox5Goal{input: input, kind: kind, first: first, end: end, target: target, administrator: admin}
	}
	return r.result(reason), nil
}

// observeGoal settles exact inclusion separately from an unattributed canonical postcondition.
func (r *StackerRole) observeGoal(ctx context.Context) string {
	goal := r.goal
	stream := &r.legacy.stream
	if goal.administrator {
		stream = r.administrator
	}
	reason, _ := stream.Observe(ctx, r.now())
	if reason == reasonExecutionRejected {
		r.failed = true
		return reason
	}
	state, err := observePoX5(ctx, goal.input, goal.target, r.now())
	if err != nil {
		return reasonPoXObservationUnavailable
	}
	r.current = state.observation
	if state.conflict {
		return reasonConflict
	}
	var proof any
	//nolint:exhaustive // Only manager lifecycle goals use this observer; enrollment uses the other path.
	switch goal.kind {
	case api.PostconditionManagerDeployment:
		if state.sourceFound {
			proof = struct{ Manager, SourceDigest string }{goal.input.manager(), sourceDigest(state.source)}
		}
	case api.PostconditionSignerRegistration:
		if state.sourceFound && state.registered && state.granted {
			proof = struct{ Manager, SourceDigest, Key string }{
				goal.input.manager(),
				sourceDigest(state.source),
				goal.input.SignerPublicKey,
			}
		}
	default:
		if state.observation != nil && state.first == goal.first && state.end == goal.end {
			observation := *state.observation
			observation.ObservedAt = metav1.Time{}
			proof = observation
		}
	}
	if proof == nil {
		return reasonAwaitingPoXPostcondition
	}
	if stream.Pending() != 0 {
		nonce := state.holder.Nonce
		if goal.administrator {
			nonce = state.administrator.Nonce
		}
		evidence := api.TransactionPostcondition{
			TxID:        stream.pending.transaction.TxID,
			Kind:        goal.kind,
			StateDigest: foundation.Digest(proof),
			StacksTip:   state.view.IndexBlockID,
			ObservedAt:  metav1.NewTime(r.now().UTC()),
		}
		reason, err = stream.SettleObserved(evidence.TxID, nonce, evidence)
		if err != nil || stream.Pending() != 0 {
			return reason
		}
	}
	if goal.kind == api.PostconditionPoX5Enrollment {
		r.initialDone = true
	}
	r.goal = nil
	if reason == reasonIdle {
		return reasonPoX5PostconditionObserved
	}
	return reason
}

// pending counts unresolved goals without double-counting a shared account stream.
func (r *StackerRole) pending() int32 {
	if r.goal != nil {
		return 1
	}
	pending := r.legacy.pending()
	if r.administrator != &r.legacy.stream {
		pending += r.administrator.Pending()
	}
	return pending
}

// result snapshots both independent account streams and the latest native evidence.
func (r *StackerRole) result(reason string) stacksworker.RoleResult {
	result := stacksworker.RoleResult{
		Transactions:        r.legacy.stream.Facts(),
		PoX5:                r.current.DeepCopy(),
		AppliedPolicyDigest: r.applied,
		Pending:             r.pending(),
		Failed:              r.failed,
		Blocked:             poxBlocked(reason, r.pending()),
		Reason:              reason,
		RequeueAfter:        time.Second,
	}
	if r.administrator != &r.legacy.stream {
		result.AdministratorTransactions = r.administrator.Facts()
	}
	return result
}

// Drain observes pending transactions without creating maintenance work.
func (r *StackerRole) Drain(ctx context.Context, snapshot stacksworker.Snapshot) (stacksworker.DrainResult, error) {
	r.current = nil
	if !r.transitioned && r.legacy.pending() != 0 {
		_, _ = r.legacy.Drain(ctx, snapshot)
	}
	if r.goal != nil {
		r.observeGoal(ctx)
	}
	result := r.result(reasonDraining)
	return stacksworker.DrainResult{
		Done:                      result.Pending == 0,
		Settled:                   result.Pending == 0,
		Pending:                   result.Pending,
		Transactions:              result.Transactions,
		AdministratorTransactions: result.AdministratorTransactions,
		PoX4:                      r.legacy.current.DeepCopy(),
		PoX5:                      result.PoX5,
	}, nil
}
