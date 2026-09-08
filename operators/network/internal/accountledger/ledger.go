// Package accountledger serializes managed Stacks transactions through durable account records.
package accountledger

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	stacks "github.com/cylewitruk-stacks/stacks-k8s/apis/network/stacks/v1alpha1"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/stackstx"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// Finalizer retains account authority until the owning environment is removed.
const Finalizer = "stacks.stacks.org/retain-account-ledger"

// ErrWaiting means that the consumer must observe again without another submission.
var ErrWaiting = errors.New("account operation is awaiting evidence or acknowledgement")

// ErrSuperseded refuses a stale operation after a newer ordinal was authorized.
var ErrSuperseded = errors.New("account operation has been superseded")

// Target records the currently admitted ingress and its runtime identity.
type Target struct {
	// Endpoint addresses the admitted Pod directly.
	Endpoint string
	// ActorUID identifies the StacksNode incarnation.
	ActorUID string
	// PodUID identifies the Pod incarnation.
	PodUID string
	// ContainerID identifies the admitted running container.
	ContainerID string
}

// Admission checks uncached parent, ledger, consumer and runtime identity before use.
// Mutation additionally requires current unpaused desired operation.
type Admission interface {
	Admit(context.Context, *stacks.StacksAccount, string, bool) (Target, error)
}

// Transaction contains public signed bytes and their independently checked identity.
type Transaction = stackstx.Transaction

// Inclusion reports exact canonical transaction execution at observation time.
type Inclusion = stackstx.Inclusion

// RPC separates reads from one explicitly authorized submission attempt.
type RPC interface {
	Nonce(context.Context, string, string) (int64, error)
	Inclusion(context.Context, string, string) (Inclusion, error)
	Submit(context.Context, string, Transaction) error
}

// Rejection exposes only a matched, bounded native rejection classification.
type Rejection interface {
	error
	Reason() string
}

// Request identifies an idempotent capability operation and its offline signer.
type Request struct {
	// ConsumerUID is pinned for the lifetime of this account's operation stream.
	ConsumerUID string
	// Ordinal strictly increases for a different operation on this account.
	Ordinal int64
	// Digest binds public intent independently of nondeterministic signature bytes.
	Digest string
	// Sign uses the supplied nonce without discovery, submission or other side effects.
	Sign func(context.Context, int64) (Transaction, error)
}

// Ledger supplies shared transaction accounting to resource-focused controllers.
type Ledger struct {
	// Client performs optimistic-lock ledger writes.
	Client client.Client
	// Reader supplies uncached ledger and identity reads.
	Reader client.Reader
	// Admission validates the independently required runtime and capability authority.
	Admission Admission
	// RPC supplies bounded native chain observations and single-shot submission.
	RPC RPC
	// Now supplies evidence observation time; nil uses time.Now.
	Now func() time.Time
}

// Ensure accounts an existing operation or authorizes one new, monotonically numbered operation.
// An error never authorizes the caller to resend. Subsequent calls only observe an armed TxID.
func (l *Ledger) Ensure(ctx context.Context, key client.ObjectKey, request Request) (*stacks.AccountTransaction, error) {
	if request.ConsumerUID == "" || request.Ordinal < 1 || request.Ordinal > 9007199254740991 || !digestValid(request.Digest) || request.Sign == nil {
		return nil, fmt.Errorf("invalid account operation")
	}
	account := &stacks.StacksAccount{}
	if err := l.Reader.Get(ctx, key, account); err != nil {
		return nil, err
	}
	if account.Status.Phase == "Abandoned" || !account.DeletionTimestamp.IsZero() {
		return nil, fmt.Errorf("account ledger is retired")
	}
	if current := account.Status.Transaction; current != nil {
		if current.ConsumerUID != request.ConsumerUID {
			return nil, fmt.Errorf("account consumer identity changed")
		}
		if request.Ordinal < current.Ordinal {
			return nil, ErrSuperseded
		}
		if request.Ordinal == current.Ordinal && request.Digest != current.OperationDigest {
			return nil, fmt.Errorf("account operation identity changed")
		}
		if current.Receipt == nil {
			if err := l.observe(ctx, account, request.ConsumerUID); err != nil {
				return nil, err
			}
		}
		if request.Ordinal == current.Ordinal {
			return account.Status.Transaction.DeepCopy(), nil
		}
		if !account.Status.Transaction.Acknowledged {
			return nil, ErrWaiting
		}
	}
	if !controllerutil.ContainsFinalizer(account, Finalizer) {
		base := account.DeepCopy()
		controllerutil.AddFinalizer(account, Finalizer)
		if err := l.Client.Patch(ctx, account, client.MergeFromWithOptions(base, client.MergeFromWithOptimisticLock{})); err != nil {
			return nil, err
		}
		return nil, ErrWaiting
	}
	target, err := l.Admission.Admit(ctx, account, request.ConsumerUID, true)
	if err != nil {
		return nil, err
	}
	nonce, err := l.RPC.Nonce(ctx, target.Endpoint, account.Spec.Policy.Address)
	if err != nil {
		return nil, err
	}
	if nonce < 0 || nonce >= 9007199254740991 || (account.Status.NextNonce != nil && nonce != *account.Status.NextNonce) {
		return nil, fmt.Errorf("account nonce does not match expected canonical state")
	}
	tx, err := request.Sign(ctx, nonce)
	if err != nil {
		return nil, err
	}
	if !ValidTransaction(tx) {
		return nil, fmt.Errorf("offline signer returned an invalid transaction identity")
	}
	// Signing may take time. Admission re-reads current intent and identity before the CAS.
	currentTarget, err := l.Admission.Admit(ctx, account, request.ConsumerUID, true)
	if err != nil || currentTarget != target {
		return nil, fmt.Errorf("account authority or ingress changed during signing")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	base := account.DeepCopy()
	account.Status.Transaction = &stacks.AccountTransaction{Ordinal: request.Ordinal, OperationDigest: request.Digest, ConsumerUID: request.ConsumerUID,
		TxID: tx.TxID, Nonce: nonce, TargetUID: target.ActorUID, PodUID: target.PodUID, ContainerID: target.ContainerID, AuthorizedAt: l.now()}
	account.Status.NextNonce = &nonce
	account.Status.Phase = "Pending"
	if err := l.patchStatus(ctx, account, base); err != nil {
		// A lost acknowledgement may have committed. No send occurs on this path.
		return nil, err
	}
	// Only the process that received this successful authorization acknowledgement sends.
	err = l.RPC.Submit(ctx, target.Endpoint, tx)
	base = account.DeepCopy()
	if err == nil {
		account.Status.Transaction.Accepted = true
	} else {
		account.Status.Phase = "Ambiguous"
		var rejected Rejection
		if errors.As(err, &rejected) {
			reason := rejected.Reason()
			if reason == "FeeTooLow" || reason == "BadNonce" || reason == "Other" {
				account.Status.Transaction.RejectionReason = reason
				account.Status.Phase = "Blocked"
			}
		}
	}
	if err := l.patchStatus(ctx, account, base); err != nil {
		return nil, err
	}
	return nil, ErrWaiting
}

// observe accounts a receipt without freeing the consumer's reservation.
func (l *Ledger) observe(ctx context.Context, account *stacks.StacksAccount, consumerUID string) error {
	target, err := l.Admission.Admit(ctx, account, consumerUID, false)
	if err != nil {
		return err
	}
	receipt, err := l.RPC.Inclusion(ctx, target.Endpoint, account.Status.Transaction.TxID)
	if err != nil {
		return err
	}
	if !receipt.Found {
		return ErrWaiting
	}
	return l.RecordReceipt(ctx, account, receipt)
}

// RecordReceipt persists a previously validated canonical execution on the exact current authorization.
// Its optimistic lock prevents event delivery from accounting a different or newer transaction.
func (l *Ledger) RecordReceipt(ctx context.Context, account *stacks.StacksAccount, receipt Inclusion) error {
	if account.Status.Transaction == nil || account.Status.Phase == "Abandoned" || !receipt.Found {
		return fmt.Errorf("no live account authorization for receipt")
	}
	if !hashValid(receipt.BlockID) {
		return fmt.Errorf("execution block identity unavailable")
	}
	if account.Status.Transaction.Receipt != nil {
		return fmt.Errorf("account receipt is already retained")
	}
	source := receipt.Source
	if source == "" {
		source = "NativeIndex"
	}
	if source != "NativeIndex" && source != "LegacyEvent" {
		return fmt.Errorf("unsupported receipt evidence source")
	}
	base := account.DeepCopy()
	account.Status.Transaction.Receipt = &stacks.AccountReceipt{Source: source, BlockID: receipt.BlockID, Success: receipt.Success, ObservedAt: l.now()}
	next := account.Status.Transaction.Nonce + 1
	account.Status.NextNonce = &next
	account.Status.Phase = "Executed"
	return l.patchStatus(ctx, account, base)
}

// Acknowledge permits newer operations after the consumer durably retains this exact receipt.
func (l *Ledger) Acknowledge(ctx context.Context, key client.ObjectKey, consumerUID string, transaction *stacks.AccountTransaction) error {
	if transaction == nil || transaction.Receipt == nil {
		return fmt.Errorf("execution receipt is required for acknowledgement")
	}
	account := &stacks.StacksAccount{}
	if err := l.Reader.Get(ctx, key, account); err != nil {
		return err
	}
	current := account.Status.Transaction
	if current == nil || current.ConsumerUID != consumerUID || current.Ordinal != transaction.Ordinal || current.TxID != transaction.TxID || current.Receipt == nil || *current.Receipt != *transaction.Receipt {
		return fmt.Errorf("account receipt identity changed")
	}
	if current.Acknowledged {
		return nil
	}
	base := account.DeepCopy()
	current.Acknowledged = true
	return l.patchStatus(ctx, account, base)
}

// patchStatus preserves conflicting account updates rather than merging authorization facts.
func (l *Ledger) patchStatus(ctx context.Context, account, base *stacks.StacksAccount) error {
	return l.Client.Status().Patch(ctx, account, client.MergeFromWithOptions(base, client.MergeFromWithOptimisticLock{}))
}

// now supplies a UTC evidence timestamp.
func (l *Ledger) now() metav1.Time {
	now := time.Now
	if l.Now != nil {
		now = l.Now
	}
	return metav1.NewTime(now().UTC())
}

// ValidTransaction checks bounded consensus bytes independently of the SDK.
func ValidTransaction(tx Transaction) bool { return stackstx.Valid(tx) }

// hashValid checks a lower-case, 32-byte identity.
func hashValid(value string) bool {
	raw, err := hex.DecodeString(value)
	return err == nil && len(raw) == 32 && strings.ToLower(value) == value
}

// digestValid checks a public operation digest.
func digestValid(value string) bool {
	return strings.HasPrefix(value, "sha256:") && hashValid(strings.TrimPrefix(value, "sha256:"))
}
