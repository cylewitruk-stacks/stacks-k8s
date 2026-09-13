// Package stacksoperation implements process-local Stacks protocol roles.
package stacksoperation

import (
	"context"
	"encoding/hex"
	"errors"
	"math"
	"math/big"
	"strings"
	"time"

	stacks "github.com/cylewitruk-stacks/stacks-k8s/apis/network/stacks/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/libs/stacks/rpc"
	"github.com/cylewitruk-stacks/stacks-k8s/libs/stacks/transaction"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Node is the native account/submission surface needed by a nonce stream.
type Node interface {
	Account(context.Context, string) (rpc.Account, error)
	Submit(context.Context, transaction.Transaction) error
	Inclusion(context.Context, string) (rpc.Inclusion, error)
}

// pendingSubmission pins the original target and signed identity until settlement.
type pendingSubmission struct {
	node        Node
	transaction transaction.Transaction
	nonce       uint64
}

// NonceStream owns one account's ephemeral nonce without excluding other experiment writers.
// Its worker retains each outcome until publication or safe cumulative baseline coalescing.
type NonceStream struct {
	// Address is immutable for this surviving worker process.
	Address     string
	next        uint64
	initialized bool
	exhausted   bool
	pending     *pendingSubmission
	facts       api.TransactionExecutionStatus
	// retryAt bounds subsequent attempts after a definite native refusal.
	retryAt         time.Time
	rejectionReason string
}

// Facts returns an independent bounded public snapshot for status publication.
func (s *NonceStream) Facts() *api.TransactionExecutionStatus { return s.facts.DeepCopy() }

// Pending indicates an accepted or uncertain submission awaiting settlement.
func (s *NonceStream) Pending() int32 {
	if s.pending != nil {
		return 1
	}
	return 0
}

// Observe inspects only the original target and never resubmits or infers inclusion from a nonce.
func (s *NonceStream) Observe(ctx context.Context, now time.Time) (string, error) {
	if s.pending == nil {
		return reasonIdle, nil
	}
	p := s.pending
	inclusion, err := p.node.Inclusion(ctx, p.transaction.TxID)
	if err != nil {
		return reasonInclusionUnavailable, err
	}
	if !inclusion.Found {
		return reasonAwaitingInclusion, nil
	}
	if s.facts.Included == math.MaxUint64 {
		return reasonCounterExhausted, errors.New("transaction counter exhausted")
	}
	s.facts.Included++
	s.facts.LastInclusion = &api.TransactionInclusion{
		TxID:       p.transaction.TxID,
		BlockID:    inclusion.BlockID,
		Success:    inclusion.Success,
		ObservedAt: metav1.NewTime(now),
	}
	s.exhausted = p.nonce == math.MaxUint64
	if !s.exhausted {
		s.next = p.nonce + 1
	}
	s.pending = nil
	if !inclusion.Success {
		return reasonExecutionRejected, nil
	}
	return reasonIncluded, nil
}

// Offer initializes from canonical account state and submits one explicitly authorized transaction.
// Amount includes all required unlocked funds; Build receives the locally expected nonce.
// Clock is sampled again after a refusal so submission latency cannot consume its backoff.
func (s *NonceStream) Offer(
	ctx context.Context,
	clock func() time.Time,
	node Node,
	amount *big.Int,
	authorize func(context.Context) error,
	build func(uint64) (transaction.Transaction, error),
) (string, error) {
	if s.pending != nil {
		return reasonAwaitingInclusion, nil
	}
	if clock == nil {
		return reasonInvalidInputs, errors.New("missing transaction clock")
	}
	if clock().Before(s.retryAt) {
		return reasonRejectionBackoff, nil
	}
	if s.exhausted || s.facts.Offered == math.MaxUint64 {
		return reasonCounterExhausted, errors.New("transaction counter exhausted")
	}
	if node == nil || authorize == nil || build == nil || amount == nil || amount.Sign() < 0 {
		return reasonInvalidInputs, errors.New("incomplete transaction inputs")
	}
	account, err := node.Account(ctx, s.Address)
	if err != nil {
		return reasonAccountUnavailable, err
	}
	if account.Balance.Integer == nil || account.Locked.Integer == nil {
		return reasonAccountUnavailable, errors.New("incomplete account balance")
	}
	if !s.initialized {
		s.next = account.Nonce
		s.initialized = true
	}
	if account.Nonce != s.next {
		return reasonNonceMismatch, nil
	}
	// Native /v2/accounts balance already reports available (unlocked) funds.
	available := account.Balance.Integer
	if available.Cmp(amount) < 0 {
		return reasonInsufficientFunds, nil
	}
	tx, err := build(s.next)
	if err != nil {
		return reasonConstructionFailed, err
	}
	if !transaction.Valid(tx) {
		return reasonConstructionFailed, errors.New("invalid signed transaction")
	}
	if err := authorize(ctx); err != nil {
		return reasonAuthorizationUnavailable, err
	}
	// Record before the only send: connection loss cannot create an implicit retry path.
	s.pending = &pendingSubmission{
		node:        node,
		transaction: transaction.Transaction{Bytes: append([]byte(nil), tx.Bytes...), TxID: tx.TxID},
		nonce:       s.next,
	}
	s.facts.Offered++
	s.facts.LastTxID = tx.TxID
	s.rejectionReason = ""
	if err := node.Submit(ctx, tx); err != nil {
		var rejected *rpc.SubmissionRejection
		if errors.As(err, &rejected) && rejected.TxID() == tx.TxID {
			if rejected.Definite() {
				s.facts.Rejected++
				s.pending = nil
				s.retryAt = clock().Add(5 * time.Second)
				s.rejectionReason = stacks.RejectionReasonPrefix + rejected.Reason()
				return s.rejectionReason, nil
			}
		}
		s.facts.Uncertain++
		return reasonSubmissionUncertain, nil
	}
	s.facts.Accepted++
	return reasonAccepted, nil
}

// SettleObserved advances only the exact pending nonce after caller-verified canonical state.
// The caller must bracket the desired-state and account reads at one stable canonical tip.
// This does not attribute the state to our TxID or increment Included/LastInclusion.
func (s *NonceStream) SettleObserved(
	txid string,
	nextNonce uint64,
	observation api.TransactionPostcondition,
) (string, error) {
	if s.pending == nil || s.pending.transaction.TxID != txid || observation.TxID != txid {
		return reasonObservationMismatch, errors.New("postcondition does not match pending transaction")
	}
	if s.pending.nonce == math.MaxUint64 || nextNonce != s.pending.nonce+1 {
		return reasonNonceMismatch, nil
	}
	if observation.Kind != api.PostconditionPoX4Enrollment && observation.Kind != api.PostconditionPoX4Extension &&
		observation.Kind != api.PostconditionContractDeployment &&
		observation.Kind != api.PostconditionRegistryInitialization &&
		observation.Kind != api.PostconditionManagerDeployment &&
		observation.Kind != api.PostconditionSignerRegistration &&
		observation.Kind != api.PostconditionPoX5Enrollment &&
		observation.Kind != api.PostconditionPoX5Extension {
		return reasonObservationMismatch, errors.New("unsupported postcondition")
	}
	if !strings.HasPrefix(observation.StateDigest, "sha256:") ||
		!canonicalHash(strings.TrimPrefix(observation.StateDigest, "sha256:")) ||
		!canonicalHash(observation.StacksTip) ||
		observation.ObservedAt.IsZero() {
		return reasonObservationMismatch, errors.New("incomplete postcondition evidence")
	}
	if s.facts.PostconditionObserved == math.MaxUint64 {
		return reasonCounterExhausted, errors.New("postcondition counter exhausted")
	}
	s.facts.PostconditionObserved++
	s.facts.LastPostcondition = observation.DeepCopy()
	s.next = nextNonce
	s.pending = nil
	return reasonStateObserved, nil
}

// canonicalHash validates a lowercase unprefixed 32-byte observation digest.
func canonicalHash(value string) bool {
	raw, e := hex.DecodeString(value)
	return e == nil && len(raw) == 32 && hex.EncodeToString(raw) == value
}
