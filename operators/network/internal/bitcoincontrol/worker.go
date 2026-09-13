package bitcoincontrol

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"

	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/objectref"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// RPC is the non-retrying native transport required by one scoped control worker.
type RPC interface {
	// Call sends one named native request and validates its response envelope.
	Call(context.Context, string, string, string, []any, any) error
	// Check verifies regtest and payout validity without mutation.
	Check(context.Context, string, string) error
	// Generate sends exactly one block-generation request.
	Generate(context.Context, string, string, string) (string, error)
}

// WorkerInput is immutable per-Deployment control-worker enrollment.
type WorkerInput struct {
	// ActionsEnabled opts this worker into finite generation API consumption.
	ActionsEnabled bool `json:"actionsEnabled,omitempty"`
	// ReorganizationEnabled opts into bounded local suffix replacement.
	ReorganizationEnabled bool `json:"reorganizationEnabled,omitempty"`
	// Namespace scopes every API access.
	Namespace string `json:"namespace"`
	// RecordName names the retained per-node execution record.
	RecordName string `json:"recordName"`
	// RecordUID prevents same-name record recreation from reopening authority.
	RecordUID types.UID `json:"recordUID"`
	// CredentialsName identifies the mounted immutable RPC Secret.
	CredentialsName string `json:"credentialsName"`
	// CredentialsUID pins that Secret's exact incarnation.
	CredentialsUID types.UID `json:"credentialsUID"`
}

// Worker serializes per-node mutations and independently retains one receipt collector.
type Worker struct {
	// Client writes only this node's execution status.
	Client client.Client
	// Reader must bypass controller caches for authorization and accounting.
	Reader client.Reader
	// RPC contains this node's statically loaded mutation credentials only.
	RPC RPC
	// Input is the immutable exact-record/credential enrollment.
	Input WorkerInput
	// Now supplies successful-read and receipt timestamps.
	Now func() time.Time
	// ProcessNonce is freshly generated at process start; tests may supply one.
	ProcessNonce string
	// PollInterval bounds idle observation cadence.
	PollInterval time.Duration
	// DrainTimeout defaults to the qualified design's 25-second graceful allowance.
	DrainTimeout time.Duration
	// mu protects collector admission against drain.
	mu sync.Mutex
	// active bounds outstanding transport and retained receipt memory to one.
	active bool
	// closed blocks new arming during shutdown.
	closed bool
	// cancel terminates collection only after drain exhaustion.
	cancel context.CancelFunc
	// workers includes pre-arm activity so drain cannot race a late collector.
	workers sync.WaitGroup
}

// Run observes desired state until shutdown, then drains receipts for at most 25 seconds.
func (w *Worker) Run(ctx context.Context) error {
	if e := w.defaults(); e != nil {
		return e
	}
	ticker := time.NewTicker(w.PollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			w.drain()
			return nil
		default:
		}
		attempt, cancel := context.WithTimeout(ctx, 10*time.Second)
		_ = w.Step(attempt)
		cancel()
		select {
		case <-ctx.Done():
			w.drain()
			return nil
		case <-ticker.C:
		}
	}
}

// defaults validates enrollment and allocates a unique process-start nonce.
func (w *Worker) defaults() error {
	if w.Client == nil || w.Reader == nil || w.RPC == nil || w.Input.Namespace == "" || w.Input.RecordName == "" ||
		w.Input.RecordUID == "" ||
		w.Input.CredentialsUID == "" {
		return fmt.Errorf("incomplete Bitcoin worker enrollment")
	}
	if w.Now == nil {
		w.Now = time.Now
	}
	if w.PollInterval == 0 {
		w.PollInterval = time.Second
	}
	if w.DrainTimeout == 0 {
		w.DrainTimeout = 25 * time.Second
	}
	if w.ProcessNonce == "" {
		var b [16]byte
		if _, e := rand.Read(b[:]); e != nil {
			return e
		}
		w.ProcessNonce = hex.EncodeToString(b[:])
	}
	return nil
}

// Step observes one record and authorizes at most one new bounded mutation.
func (w *Worker) Step(ctx context.Context) error {
	record := &bitcoin.BitcoinExecution{}
	if e := w.Reader.Get(
		ctx,
		client.ObjectKey{Namespace: w.Input.Namespace, Name: w.Input.RecordName},
		record,
	); e != nil {
		return e
	}
	if record.UID != w.Input.RecordUID {
		return fmt.Errorf("execution record was replaced")
	}
	if record.Status.Action != nil {
		return w.stepAction(ctx, record)
	}
	if stopped, e := w.stop(ctx, record); e != nil || stopped {
		return e
	}
	if paused, e := w.acknowledgePause(ctx, record); e != nil {
		return e
	} else if paused {
		return w.observePaused(ctx, record)
	}
	if record.Status.Armed != nil {
		return nil
	} // Existing Armed is never permission to send, including after restart.
	a, e := w.authorizeObservation(ctx, record)
	if e != nil {
		return e
	}
	previousRemoval := record.Status.PendingWalletRemoval.DeepCopy()
	observation, operation, e := w.observe(ctx, a, record)
	if e != nil {
		return e
	}
	current, e := w.authorizeObservation(ctx, record)
	if e != nil || !equality.Semantic.DeepEqual(a.target, current.target) {
		return fmt.Errorf("actor identity changed during observation")
	}
	if operation != nil && operation.Method == bitcoin.RPCUnloadWallet {
		record.Status.PendingWalletRemoval = operation.Wallet.DeepCopy()
	}
	if !equality.Semantic.DeepEqual(record.Status.Observation, observation) ||
		!equality.Semantic.DeepEqual(previousRemoval, record.Status.PendingWalletRemoval) {
		record.Status.Observation = observation
		record.Status.Phase = bitcoin.ExecutionIdle
		if e = w.Client.Status().Update(ctx, record); e != nil {
			return e
		}
	}
	if !generationAccounted(current.initialization, record) {
		return nil
	}
	if operation == nil {
		if selected, err := w.selectAction(ctx, current, record); err != nil || selected {
			return err
		}
	}
	if operation == nil && record.Spec.Offer != nil && record.Spec.Offer.Number > record.Status.CompletedOffer {
		offer := record.Spec.Offer
		if e = w.authorizeOffer(ctx, current, offer); e != nil {
			return e
		}
		if observation.Height != offer.ExpectedHeight || observation.Tip != offer.ExpectedTip {
			return fmt.Errorf("generation preflight differs from selected chain")
		}
		if e = w.RPC.Check(ctx, current.target.Endpoint, offer.Address); e != nil {
			return e
		}
		operation = &bitcoin.BitcoinArmedRPC{Method: bitcoin.RPCGenerate, Offer: offer.DeepCopy()}
	}
	if operation == nil {
		return nil
	}
	return w.arm(ctx, record, current, *operation)
}

// arm reserves local capacity before CAS and starts only its own confirmed authorization.
func (w *Worker) arm(
	ctx context.Context,
	record *bitcoin.BitcoinExecution,
	a admitted,
	operation bitcoin.BitcoinArmedRPC,
) error {
	w.mu.Lock()
	if w.closed || w.active {
		w.mu.Unlock()
		return nil
	}
	w.active = true
	w.workers.Add(1)
	w.mu.Unlock()
	release := true
	defer func() {
		if release {
			w.release()
		}
	}()
	current := &bitcoin.BitcoinExecution{}
	if e := w.Reader.Get(ctx, client.ObjectKeyFromObject(record), current); e != nil {
		return e
	}
	if current.UID != record.UID || current.Status.Armed != nil {
		return fmt.Errorf("execution authority changed")
	}
	fresh, e := w.authorizeMutation(ctx, current, operation)
	if e != nil {
		return fmt.Errorf("target admission changed before arm: %w", e)
	}
	if !equality.Semantic.DeepEqual(fresh.target, a.target) {
		return fmt.Errorf("target admission changed before arm")
	}
	reservation := objectref.BitcoinInitialization(fresh.initialization)
	if operation.Action != nil {
		reservation = *operation.Action
	}
	if current.Status.Reservation != nil && *current.Status.Reservation != reservation {
		return fmt.Errorf("another mutation owner retains exclusion")
	}
	if operation.Offer != nil {
		if !equality.Semantic.DeepEqual(current.Spec.Offer, operation.Offer) ||
			operation.Offer.Number <= current.Status.CompletedOffer {
			return fmt.Errorf("generation offer changed")
		}
		if e = w.authorizeOffer(ctx, fresh, operation.Offer); e != nil {
			return e
		}
		height, tip, e := w.chain(ctx, fresh.target.Endpoint)
		if e != nil || height != operation.Offer.ExpectedHeight || tip != operation.Offer.ExpectedTip ||
			height >= operation.Offer.Ceiling {
			return fmt.Errorf("final generation ceiling preflight differs")
		}
	}
	var nonce [16]byte
	if _, e = rand.Read(nonce[:]); e != nil {
		return e
	}
	operation.ID = w.ProcessNonce + "-" + hex.EncodeToString(nonce[:])
	operation.ProcessNonce = w.ProcessNonce
	operation.Reservation = reservation
	operation.Target = fresh.target
	operation.ArmedAt = metav1.NewTime(w.Now().UTC())
	current.Status.Reservation = &reservation
	if operation.Action != nil && current.Status.Action.StartedAt == nil {
		current.Status.Action.StartedAt = operation.ArmedAt.DeepCopy()
	}
	current.Status.Armed = &operation
	current.Status.Phase = bitcoin.ExecutionArmed
	if e = w.Client.Status().Update(ctx, current); e != nil {
		return e
	} // Lost CAS acknowledgement remains blocked and unsent.
	operation = *current.Status.Armed.DeepCopy()
	confirmed, e := w.authorizeMutation(ctx, current, operation)
	if e != nil || !equality.Semantic.DeepEqual(confirmed.target, fresh.target) {
		reason := ""
		if errors.Is(e, errExternalChainMovement) {
			reason = reasonExternalChainMovement
		}
		return w.withdraw(ctx, current, reason)
	}
	if operation.Offer != nil {
		if e = w.authorizeOffer(ctx, confirmed, operation.Offer); e != nil {
			return w.withdraw(ctx, current, "")
		}
	}
	w.mu.Lock()
	closed := w.closed
	w.mu.Unlock()
	if closed || ctx.Err() != nil {
		return w.withdraw(ctx, current, "")
	}
	receiver, cancel := context.WithCancel(context.WithoutCancel(ctx))
	w.mu.Lock()
	w.cancel = cancel
	w.mu.Unlock()
	release = false
	go w.collect(receiver, current.DeepCopy(), operation)
	return nil
}

// withdraw clears only an exact locally proven-unsent authorization, retaining its reservation.
func (w *Worker) withdraw(ctx context.Context, armed *bitcoin.BitcoinExecution, stopReason string) error {
	current := &bitcoin.BitcoinExecution{}
	if e := w.Reader.Get(ctx, client.ObjectKeyFromObject(armed), current); e != nil {
		return e
	}
	if current.UID != armed.UID || !equality.Semantic.DeepEqual(current.Status.Armed, armed.Status.Armed) {
		return fmt.Errorf("cannot withdraw changed authority")
	}
	if stopReason != "" && current.Status.Action != nil && current.Status.Action.StopReason == "" {
		current.Status.Action.StopReason = stopReason
	}
	current.Status.Armed = nil
	current.Status.Phase = bitcoin.ExecutionIdle
	return w.Client.Status().Update(ctx, current)
}

// collect preserves its response path across reconcile cancellation and API outages.
func (w *Worker) collect(ctx context.Context, record *bitcoin.BitcoinExecution, request bitcoin.BitcoinArmedRPC) {
	defer w.release()
	hash, e := w.mutate(ctx, request)
	if e != nil {
		return
	}
	receipt := bitcoin.BitcoinRPCReceipt{
		Request:    request,
		BlockHash:  hash,
		ReceivedAt: metav1.NewTime(w.Now().UTC()),
	}
	for ctx.Err() == nil {
		attempt, cancel := context.WithTimeout(ctx, 5*time.Second)
		e = w.account(attempt, record, &receipt)
		cancel()
		if e == nil || errors.Is(e, errRetired) {
			return
		}
		timer := time.NewTimer(time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

// errRetired stops local accounting after explicit loss of the bound environment.
var errRetired = errors.New("execution environment retired")

// account atomically records a receipt and its counters without releasing reservation ownership.
func (w *Worker) account(
	ctx context.Context,
	record *bitcoin.BitcoinExecution,
	receipt *bitcoin.BitcoinRPCReceipt,
) error {
	current := &bitcoin.BitcoinExecution{}
	if e := w.Reader.Get(ctx, client.ObjectKeyFromObject(record), current); e != nil {
		return e
	}
	if current.UID != record.UID {
		return errRetired
	}
	if current.Status.LastReceipt != nil && current.Status.LastReceipt.Request.ID == receipt.Request.ID {
		return nil
	}
	if !equality.Semantic.DeepEqual(current.Status.Armed, &receipt.Request) {
		return fmt.Errorf("receipt does not match outstanding authority")
	}
	current.Status.LastReceipt = receipt.DeepCopy()
	if receipt.Request.Method == bitcoin.RPCUnloadWallet {
		current.Status.PendingWalletRemoval = nil
		if observation := current.Status.Observation; observation != nil {
			retained := observation.Wallets[:0]
			for _, wallet := range observation.Wallets {
				if wallet.Wallet.UID != receipt.Request.Wallet.Wallet.UID {
					retained = append(retained, wallet)
				}
			}
			observation.Wallets = retained
		}
	}
	current.Status.Armed = nil
	current.Status.Phase = bitcoin.ExecutionIdle
	if receipt.Request.Action != nil {
		if err := accountActionReceipt(current, receipt); err != nil {
			return err
		}
	} else if receipt.Request.Method == bitcoin.RPCGenerate {
		current.Status.BlocksGenerated++
		current.Status.CompletedOffer = receipt.Request.Offer.Number
	}
	return w.Client.Status().Update(ctx, current)
}

// release frees the sole local receipt slot after completion or a lost response.
func (w *Worker) release() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.cancel != nil {
		w.cancel()
		w.cancel = nil
	}
	w.active = false
	w.workers.Done()
}

// drain stops new authorization and preserves collectors within the bounded shutdown allowance.
func (w *Worker) drain() {
	w.mu.Lock()
	w.closed = true
	w.mu.Unlock()
	done := make(chan struct{})
	go func() { w.workers.Wait(); close(done) }()
	timer := time.NewTimer(w.DrainTimeout)
	defer timer.Stop()
	select {
	case <-done:
	case <-timer.C:
		w.mu.Lock()
		if w.cancel != nil {
			w.cancel()
		}
		w.mu.Unlock()
	}
}
