package production

import (
	"context"
	"errors"
	"sync"
	"time"

	bitcoinv1alpha1 "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
)

const (
	// collectorLimit bounds receivers and retained receipts together, across the process.
	collectorLimit = 32
	// collectorDrain leaves time inside the manager's 30-second shutdown allowance.
	collectorDrain = 25 * time.Second
	// accountingTimeout bounds one API attempt, never the lifetime of a received receipt.
	accountingTimeout = 5 * time.Second
)

// receivedReceipt is immutable evidence retained until accounting or administrative retirement.
type receivedReceipt struct {
	// method identifies a reorganization mutation; empty means ordinary generation.
	method string
	// hash is the matching single-block success result.
	hash string
	// completed is the original receipt time, not the time of a later accounting retry.
	completed metav1.Time
}

// collectorEntry occupies one bounded slot from before arming through receipt accounting.
type collectorEntry struct {
	// uid binds the slot to an exact ledger incarnation.
	uid types.UID
	// phase distinguishes active transport from received-but-unaccounted evidence.
	phase string
	// receipt remains available while API writes are unavailable.
	receipt *receivedReceipt
	// ctx and cancel control this receiver without borrowing a reconcile lifetime.
	ctx    context.Context
	cancel context.CancelFunc
}

// collectorPool owns bounded process-local receipt knowledge and manager shutdown draining.
type collectorPool struct {
	// reconciler supplies typed RPC and identity-checked accounting operations.
	reconciler *Reconciler
	// mutex protects slots, receipt publication, and the shutdown admission boundary.
	mutex sync.Mutex
	// entries is keyed by the full process-bound DispatchID.
	entries map[string]*collectorEntry
	// workers includes pre-arm reservations so shutdown cannot race a late worker start.
	workers sync.WaitGroup
	// closed rejects new reservations once draining begins.
	closed bool
	// limit bounds outstanding work and retained receipts together.
	limit int
	// drainTimeout bounds graceful draining after manager cancellation.
	drainTimeout time.Duration
}

// newCollectorPool constructs a pool before controller registration.
func newCollectorPool(r *Reconciler, limit int, drain time.Duration) *collectorPool {
	return &collectorPool{reconciler: r, entries: make(map[string]*collectorEntry), limit: limit, drainTimeout: drain}
}

// NeedLeaderElection keeps draining in the same manager shutdown group as the producer.
func (*collectorPool) NeedLeaderElection() bool { return true }

// Start stops admission on shutdown and retains collectors for the bounded drain period.
func (p *collectorPool) Start(ctx context.Context) error {
	<-ctx.Done()
	p.mutex.Lock()
	p.closed = true
	pending := len(p.entries)
	p.mutex.Unlock()
	logger := ctrl.Log.WithName("bitcoin-receipts")
	logger.Info("Draining Bitcoin receipt collectors", "pending", pending)
	done := make(chan struct{})
	go func() { p.workers.Wait(); close(done) }()
	timer := time.NewTimer(p.drainTimeout)
	defer timer.Stop()
	select {
	case <-done:
		logger.Info("Bitcoin receipt drain completed")
	case <-timer.C:
		p.mutex.Lock()
		for _, entry := range p.entries {
			entry.cancel()
		}
		pending = len(p.entries)
		p.mutex.Unlock()
		logger.Info("Bitcoin receipt drain exhausted; unresolved ledgers remain closed", "pending", pending)
	}
	return nil
}

// reserve allocates capacity before the durable authorization attempt.
func (p *collectorPool) reserve(id string, uid types.UID) bool {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	if p.closed || len(p.entries) >= p.limit || p.entries[id] != nil {
		return false
	}
	ctx, cancel := context.WithCancel(context.Background())
	p.entries[id] = &collectorEntry{uid: uid, phase: "Collecting", ctx: ctx, cancel: cancel}
	p.workers.Add(1)
	return true
}

// release retires one slot after a failed arm, completed accounting, or lost receipt.
func (p *collectorPool) release(id string) {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	if entry := p.entries[id]; entry != nil {
		entry.cancel()
		delete(p.entries, id)
		p.workers.Done()
	}
}

// phase reports only local knowledge for the exact dispatch and ledger UID.
func (p *collectorPool) phase(id string, uid types.UID) string {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	if entry := p.entries[id]; entry != nil && entry.uid == uid {
		return entry.phase
	}
	return ""
}

// abandon cancels local work for a ledger being administratively removed.
func (p *collectorPool) abandon(uid types.UID) {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	for _, entry := range p.entries {
		if entry.uid == uid {
			entry.cancel()
		}
	}
}

// collect starts exactly one authorized send without occupying a reconcile worker.
func (p *collectorPool) collect(armed *bitcoinv1alpha1.BitcoinProductionTarget, endpoint string) {
	p.mutex.Lock()
	entry := p.entries[armed.Status.DispatchID]
	p.mutex.Unlock()
	go func() {
		defer p.release(armed.Status.DispatchID)
		var hash, method string
		var err error
		if record := armed.Status.Reorganization; record != nil {
			rpc, ok := p.reconciler.RPC.(ReorganizationRPC)
			if !ok {
				return
			}
			method = record.Step
			switch method {
			case "Invalidate":
				err = rpc.Invalidate(entry.ctx, endpoint, record.InvalidatedHash, armed.Status.DispatchID)
			case "Generate":
				hash, err = rpc.Generate(entry.ctx, endpoint, record.Spec.Address, armed.Status.DispatchID)
			case "Reconsider":
				err = rpc.Reconsider(entry.ctx, endpoint, record.InvalidatedHash, armed.Status.DispatchID)
			default:
				return
			}
		} else {
			hash, err = p.reconciler.RPC.Generate(entry.ctx, endpoint, armed.Spec.Policy.Address, armed.Status.DispatchID)
		}
		if err != nil {
			return
		} // The persistent Armed record, without a local collector, becomes Blocked.
		receipt := &receivedReceipt{method: method, hash: hash, completed: metav1.NewTime(p.reconciler.Now().UTC())}
		p.mutex.Lock()
		entry.receipt, entry.phase = receipt, "Accounting"
		p.mutex.Unlock()
		for entry.ctx.Err() == nil {
			attempt, cancel := context.WithTimeout(entry.ctx, accountingTimeout)
			err = p.reconciler.account(attempt, armed, *receipt)
			cancel()
			if err == nil || errors.Is(err, errLedgerRetired) {
				return
			}
			ctrl.Log.WithName("bitcoin-receipts").Error(err, "Retaining received block pending durable accounting", "dispatchID", armed.Status.DispatchID)
			timer := time.NewTimer(time.Second)
			select {
			case <-entry.ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
	}()
}
