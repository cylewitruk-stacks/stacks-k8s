package stacksoperation

import (
	"context"
	"errors"
	"math/big"
	"time"

	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/foundation"

	"github.com/cylewitruk-stacks/stacks-k8s/libs/stacks/identity"
	"github.com/cylewitruk-stacks/stacks-k8s/libs/stacks/rpc"
	"github.com/cylewitruk-stacks/stacks-k8s/libs/stacks/transaction"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/stacksworker"
)

// TrafficNode supplies native chain identity and nonce-stream operations.
type TrafficNode interface {
	Node
	Info(context.Context) (rpc.Info, error)
	ChainView(context.Context) (rpc.ChainView, error)
}

// TransferInputs is the resolved complete policy for one traffic observation.
type TransferInputs struct {
	// Node is the current admitted native ingress.
	Node TrafficNode
	// Target contains the public bindings of the exact native ingress.
	Target api.TrafficObservation
	// Recipient is a validated testnet principal.
	Recipient string
	// Amount and Fee are explicit micro-STX amounts.
	Amount, Fee uint64
	// Interval offers transfers without promising consensus block cadence.
	Interval time.Duration
	// StartHeight is the immutable epoch-3 activation boundary from frozen genesis.
	StartHeight uint64
}

// TransferRole offers one small transfer at a time from a mounted signing account.
type TransferRole struct {
	// Resolve reads the admitted public dependencies for the supplied snapshot.
	Resolve func(context.Context, stacksworker.Snapshot) (TransferInputs, error)
	// Now allows protocol cadence to be tested without wall-clock sleeps.
	Now     func() time.Time
	key     string
	stream  NonceStream
	next    time.Time
	applied string
	// inputs retains only the last locally validated transfer policy.
	inputs appliedInputs[TransferInputs]
	// pendingTarget binds submitted bytes until exact inclusion.
	pendingTarget *TransferInputs
	// includedTarget retains the original ingress for canonical rechecks.
	includedTarget *TransferInputs
	// traffic retains the last successful canonical read and availability.
	traffic *api.TrafficObservation
	// nextObservation bounds polling independently of transfer cadence.
	nextObservation time.Time
	// interval tracks the actually applied cadence.
	interval time.Duration
}

// NewTransferRole verifies the mounted key against the admitted sender address.
func NewTransferRole(privateKey, address string) (*TransferRole, error) {
	key, err := identity.CompressedPrivateKey(privateKey)
	if err != nil {
		return nil, errors.New("invalid transfer signing key")
	}
	public, err := identity.FromPrivate(key)
	if err != nil || public.Address != address {
		return nil, errors.New("transfer signing identity differs")
	}
	return &TransferRole{key: key, stream: NonceStream{Address: address}}, nil
}

// now returns the injected or real wall clock.
func (r *TransferRole) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

// Step observes pending work even while paused and never catches up missed opportunities.
func (r *TransferRole) Step(ctx context.Context, snapshot stacksworker.Snapshot) (result stacksworker.RoleResult, stepErr error) {
	now := r.now()
	defer func() {
		r.observeTraffic(ctx, now)
		result.Traffic = r.trafficEvidence()
		result.RequeueAfter = min(result.RequeueAfter, time.Duration(foundation.ObservationPolicy().PollIntervalSeconds)*time.Second)
	}()
	if r.stream.Pending() != 0 {
		reason, err := r.stream.Observe(ctx, now)
		r.captureInclusion()
		// Preserve the post-send result even if a subsequent observation is unavailable.
		if err != nil {
			return r.result("InclusionUnavailable", time.Second), nil
		}
		return r.result(reason, time.Second), nil
	}
	if snapshot.Paused {
		return r.result(reasonPaused, time.Second), nil
	}
	if r.Resolve == nil || snapshot.Participant == nil || snapshot.Participant.Status.Admission == nil {
		return r.result("PolicyUnavailable", time.Second), nil
	}
	input, resolved, err := r.inputs.resolve(ctx, snapshot, r.applied, r.Resolve, cloneTransferInputs)
	if err != nil {
		return r.result("DependenciesUnavailable", time.Second), nil
	}
	snapshot = resolved
	if input.Node == nil || input.Amount == 0 || input.Fee == 0 || input.Interval < time.Second || input.Interval > time.Hour {
		r.inputs.invalidate(snapshot, r.applied)
		return r.result("InvalidPolicy", time.Second), nil
	}
	r.inputs.remember(snapshot, input, cloneTransferInputs)
	r.interval = input.Interval
	digest := snapshot.Participant.Status.Admission.PolicyDigest
	if r.applied != digest {
		r.applied = digest
		r.next = now
	}
	if now.Before(r.next) {
		reason := reasonWaitingCadence
		if r.stream.rejectionReason != "" {
			reason = r.stream.rejectionReason
		}
		return r.result(reason, r.next.Sub(now)), nil
	}
	info, err := input.Node.Info(ctx)
	if err != nil {
		return r.result("ChainUnavailable", time.Second), nil
	}
	if info.NetworkID != 0x80000000 {
		return r.result("ChainIdentityMismatch", time.Second), nil
	}
	if info.BurnHeight < input.StartHeight {
		return r.result("AwaitingEpoch3", time.Second), nil
	}
	amount := new(big.Int).Add(new(big.Int).SetUint64(input.Amount), new(big.Int).SetUint64(input.Fee))
	before := r.stream.facts.Offered
	reason, err := r.stream.Offer(ctx, r.now, input.Node, amount, r.authorize(snapshot, input), func(nonce uint64) (transaction.Transaction, error) {
		return transaction.Transfer(transaction.Options{Version: transaction.Testnet, ChainID: 0x80000000, Nonce: nonce, Fee: input.Fee, PostConditionMode: transaction.Deny, PrivateKey: r.key}, input.Recipient, input.Amount, "")
	})
	if err != nil {
		return r.result(reason, time.Second), nil
	}
	if r.stream.facts.Offered != before {
		r.next = now.Add(input.Interval)
	}
	if r.stream.Pending() != 0 {
		saved := cloneTransferInputs(input)
		saved.Target.SubmittedBurnHeight = info.BurnHeight
		r.pendingTarget = &saved
	}
	return r.result(reason, time.Second), nil
}

// Drain makes read-only progress until no pending transaction remains or the runtime bound expires.
func (r *TransferRole) Drain(ctx context.Context, _ stacksworker.Snapshot) (stacksworker.DrainResult, error) {
	_, err := r.stream.Observe(ctx, r.now())
	r.captureInclusion()
	r.observeTraffic(ctx, r.now())
	pending := r.stream.Pending()
	return stacksworker.DrainResult{Done: pending == 0, Settled: pending == 0, Pending: pending, Transactions: r.stream.Facts(), Traffic: r.trafficEvidence()}, err
}

// result copies public facts so status retries cannot alias later nonce-stream updates.
func (r *TransferRole) result(reason string, delay time.Duration) stacksworker.RoleResult {
	blocked := true
	switch reason {
	case reasonPaused, reasonWaitingCadence, reasonAccepted, reasonIncluded, reasonIdle, reasonAwaitingInclusion:
		blocked = false
	}
	return stacksworker.RoleResult{AppliedPolicyDigest: r.applied, Pending: r.stream.Pending(), Reason: reason, Blocked: blocked, RequeueAfter: delay, Transactions: r.stream.Facts()}
}

// cloneTransferInputs copies the immutable client and scalar transfer policy.
func cloneTransferInputs(in TransferInputs) TransferInputs { return in }
