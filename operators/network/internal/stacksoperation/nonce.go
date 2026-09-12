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
		return "Idle", nil
	}
	p := s.pending
	inclusion, err := p.node.Inclusion(ctx, p.transaction.TxID)
	if err != nil {
		return "InclusionUnavailable", err
	}
	if !inclusion.Found {
		return "AwaitingInclusion", nil
	}
	if s.facts.Included == math.MaxUint64 {
		return "CounterExhausted", errors.New("transaction counter exhausted")
	}
	s.facts.Included++
	s.facts.LastInclusion = &api.TransactionInclusion{TxID: p.transaction.TxID, BlockID: inclusion.BlockID, Success: inclusion.Success, ObservedAt: metav1.NewTime(now)}
	s.exhausted = p.nonce == math.MaxUint64
	if !s.exhausted {
		s.next = p.nonce + 1
	}
	s.pending = nil
	if !inclusion.Success {
		return "ExecutionRejected", nil
	}
	return "Included", nil
}

// Offer initializes from canonical account state and submits one explicitly authorized transaction.
// Amount includes all required unlocked funds; Build receives the locally expected nonce.
// Clock is sampled again after a refusal so submission latency cannot consume its backoff.
func (s *NonceStream) Offer(ctx context.Context, clock func() time.Time, node Node, amount *big.Int, authorize func(context.Context) error, build func(uint64) (transaction.Transaction, error)) (string, error) {
	if s.pending != nil {
		return "AwaitingInclusion", nil
	}
	if clock == nil {
		return "InvalidInputs", errors.New("missing transaction clock")
	}
	if clock().Before(s.retryAt) {
		return "RejectionBackoff", nil
	}
	if s.exhausted || s.facts.Offered == math.MaxUint64 {
		return "CounterExhausted", errors.New("transaction counter exhausted")
	}
	if node == nil || authorize == nil || build == nil || amount == nil || amount.Sign() < 0 {
		return "InvalidInputs", errors.New("incomplete transaction inputs")
	}
	account, err := node.Account(ctx, s.Address)
	if err != nil {
		return "AccountUnavailable", err
	}
	if account.Balance.Integer == nil || account.Locked.Integer == nil {
		return "AccountUnavailable", errors.New("incomplete account balance")
	}
	if !s.initialized {
		s.next = account.Nonce
		s.initialized = true
	}
	if account.Nonce != s.next {
		return "NonceMismatch", nil
	}
	// Native /v2/accounts balance already reports available (unlocked) funds.
	available := account.Balance.Integer
	if available.Cmp(amount) < 0 {
		return "InsufficientFunds", nil
	}
	tx, err := build(s.next)
	if err != nil {
		return "ConstructionFailed", err
	}
	if !transaction.Valid(tx) {
		return "ConstructionFailed", errors.New("invalid signed transaction")
	}
	if err := authorize(ctx); err != nil {
		return "AuthorizationUnavailable", err
	}
	// Record before the only send: connection loss cannot create an implicit retry path.
	s.pending = &pendingSubmission{node: node, transaction: transaction.Transaction{Bytes: append([]byte(nil), tx.Bytes...), TxID: tx.TxID}, nonce: s.next}
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
				s.rejectionReason = "Rejected" + rejected.Reason()
				return s.rejectionReason, nil
			}
		}
		s.facts.Uncertain++
		return "SubmissionUncertain", nil
	}
	s.facts.Accepted++
	return "Accepted", nil
}

// SettleObserved advances only the exact pending nonce after caller-verified canonical state.
// The caller must bracket the desired-state and account reads at one stable canonical tip.
// This does not attribute the state to our TxID or increment Included/LastInclusion.
func (s *NonceStream) SettleObserved(txid string, nextNonce uint64, observation api.TransactionPostcondition) (string, error) {
	if s.pending == nil || s.pending.transaction.TxID != txid || observation.TxID != txid {
		return "ObservationMismatch", errors.New("postcondition does not match pending transaction")
	}
	if s.pending.nonce == math.MaxUint64 || nextNonce != s.pending.nonce+1 {
		return "NonceMismatch", nil
	}
	if observation.Kind != "PoX4Enrollment" && observation.Kind != "PoX4Extension" && observation.Kind != "ContractDeployment" && observation.Kind != "RegistryInitialization" && observation.Kind != "ManagerDeployment" && observation.Kind != "SignerRegistration" && observation.Kind != "PoX5Enrollment" && observation.Kind != "PoX5Extension" {
		return "ObservationMismatch", errors.New("unsupported postcondition")
	}
	if !strings.HasPrefix(observation.StateDigest, "sha256:") || !canonicalHash(strings.TrimPrefix(observation.StateDigest, "sha256:")) || !canonicalHash(observation.StacksTip) || observation.ObservedAt.IsZero() {
		return "ObservationMismatch", errors.New("incomplete postcondition evidence")
	}
	if s.facts.PostconditionObserved == math.MaxUint64 {
		return "CounterExhausted", errors.New("postcondition counter exhausted")
	}
	s.facts.PostconditionObserved++
	s.facts.LastPostcondition = observation.DeepCopy()
	s.next = nextNonce
	s.pending = nil
	return "StateObserved", nil
}

// canonicalHash validates a lowercase unprefixed 32-byte observation digest.
func canonicalHash(value string) bool {
	raw, e := hex.DecodeString(value)
	return e == nil && len(raw) == 32 && hex.EncodeToString(raw) == value
}
