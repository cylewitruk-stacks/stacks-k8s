package production

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	bitcoinv1alpha1 "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// accountingOutageClient fails only receipt-accounting writes while the outage is active.
type accountingOutageClient struct {
	client.Client
	unavailable atomic.Bool
	attempts    atomic.Int32
}

func (c *accountingOutageClient) Status() client.SubResourceWriter {
	return &accountingOutageWriter{SubResourceWriter: c.Client.Status(), owner: c}
}

// accountingOutageWriter preserves successful arm/status writes during an accounting outage.
type accountingOutageWriter struct {
	client.SubResourceWriter
	owner *accountingOutageClient
}

func (w *accountingOutageWriter) Patch(ctx context.Context, object client.Object, patch client.Patch, options ...client.SubResourcePatchOption) error {
	if policy, ok := object.(*bitcoinv1alpha1.BitcoinBlockProduction); ok && policy.Status.DispatchState == "Idle" {
		w.owner.attempts.Add(1)
		if w.owner.unavailable.Load() {
			return fmt.Errorf("accounting API unavailable")
		}
	}
	return w.SubResourceWriter.Patch(ctx, object, patch, options...)
}

// waitLocalPhase synchronizes tests with published collector state.
func waitLocalPhase(t *testing.T, f *productionFixture, phase string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		ledger := f.ledger(t)
		if f.r.collectors.phase(ledger.Status.DispatchID, ledger.UID) == phase {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("collector did not reach %s", phase)
}

// startDispatch invokes the production reconcile without joining the asynchronous collector.
func startDispatch(t *testing.T, f *productionFixture) {
	t.Helper()
	_, err := f.r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(f.policy)})
	if err != nil {
		t.Fatal(err)
	}
}

func TestReceivedReceiptSurvivesAccountingOutageBeyondAttemptBound(t *testing.T) {
	f := fixture(t)
	outage := &accountingOutageClient{Client: f.r.Client}
	outage.unavailable.Store(true)
	f.r.Client = outage
	startDispatch(t, f)
	waitLocalPhase(t, f, "Accounting")
	originalReceiptTime := f.now
	f.now = f.now.Add(time.Hour)
	time.Sleep(accountingTimeout + 100*time.Millisecond)
	startDispatch(t, f)
	if got := f.ledger(t).Status; got.Phase != "Accounting" || got.DispatchState != "Armed" || got.BlocksProduced != 0 {
		t.Fatalf("received evidence was abandoned: %#v", got)
	}
	if outage.attempts.Load() < 2 {
		t.Fatal("accounting was not retried during the outage")
	}
	outage.unavailable.Store(false)
	f.waitCollectors(t)
	got := f.ledger(t).Status
	if f.rpc.sends != 1 || got.BlocksProduced != 1 || got.LastCompletedAt == nil || !got.LastCompletedAt.Time.Equal(originalReceiptTime) {
		t.Fatalf("receipt replayed, recounted or retimestamped: %#v", got)
	}
}

func TestDelayedReceiptDoesNotOccupyReconcilerOrInheritPreflightDeadline(t *testing.T) {
	f := fixture(t)
	f.r.RPCTimeout = 10 * time.Millisecond
	started, release := make(chan struct{}), make(chan struct{})
	f.rpc.generate = func(ctx context.Context) error {
		close(started)
		select {
		case <-release:
			return ctx.Err()
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	startDispatch(t, f)
	<-started
	time.Sleep(30 * time.Millisecond)
	startDispatch(t, f)
	if got := f.ledger(t).Status; got.Phase != "Collecting" || got.DispatchState != "Armed" {
		t.Fatalf("slow surviving receipt was discarded: %#v", got)
	}
	close(release)
	f.waitCollectors(t)
	if f.ledger(t).Status.BlocksProduced != 1 {
		t.Fatal("late receipt not accounted")
	}
}

func TestCollectorShutdownDrainsInFlightReceiptAndRejectsNewReservations(t *testing.T) {
	f := fixture(t)
	started, release := make(chan struct{}), make(chan struct{})
	f.rpc.generate = func(ctx context.Context) error {
		close(started)
		select {
		case <-release:
			return ctx.Err()
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	startDispatch(t, f)
	<-started
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- f.r.collectors.Start(ctx) }()
	cancel()
	deadline := time.Now().Add(time.Second)
	for {
		f.r.collectors.mutex.Lock()
		closed := f.r.collectors.closed
		f.r.collectors.mutex.Unlock()
		if closed {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("pool did not stop admission")
		}
		time.Sleep(time.Millisecond)
	}
	if f.r.collectors.reserve("new", "new-uid") {
		t.Fatal("shutdown admitted another dispatch")
	}
	close(release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("shutdown did not finish after accounting")
	}
	if f.ledger(t).Status.DispatchState != "Idle" {
		t.Fatal("drain lost a surviving receipt")
	}
}

func TestCollectorDrainExhaustionLeavesLedgerClosed(t *testing.T) {
	f := fixture(t)
	f.r.collectors.drainTimeout = 20 * time.Millisecond
	started := make(chan struct{})
	f.rpc.generate = func(ctx context.Context) error { close(started); <-ctx.Done(); return ctx.Err() }
	startDispatch(t, f)
	<-started
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := f.r.collectors.Start(ctx); err != nil {
		t.Fatal(err)
	}
	f.waitCollectors(t)
	startDispatch(t, f)
	if got := f.ledger(t).Status; got.Phase != "Blocked" || got.DispatchState != "Armed" || f.rpc.sends != 1 {
		t.Fatalf("exhausted drain reopened target: %#v", got)
	}
}

func TestUnaccountedReceiptUsesCollectorCapacity(t *testing.T) {
	f := fixture(t)
	f.r.collectors.limit = 1
	outage := &accountingOutageClient{Client: f.r.Client}
	outage.unavailable.Store(true)
	f.r.Client = outage
	startDispatch(t, f)
	waitLocalPhase(t, f, "Accounting")
	if f.r.collectors.reserve("second", "another-ledger") {
		t.Fatal("unaccounted receipt did not retain bounded capacity")
	}
	outage.unavailable.Store(false)
	f.waitCollectors(t)
	if !f.r.collectors.reserve("second", "another-ledger") {
		t.Fatal("accounting did not release capacity")
	}
	f.r.collectors.release("second")
}

func TestRestartCannotReconstructUnaccountedLocalReceipt(t *testing.T) {
	f := fixture(t)
	outage := &accountingOutageClient{Client: f.r.Client}
	outage.unavailable.Store(true)
	f.r.Client = outage
	startDispatch(t, f)
	waitLocalPhase(t, f, "Accounting")
	f.r.collectors.drainTimeout = 20 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := f.r.collectors.Start(ctx); err != nil {
		t.Fatal(err)
	}
	f.waitCollectors(t)
	outage.unavailable.Store(false)
	f.r.ProcessNonce = "replacement-process"
	f.r.collectors = newCollectorPool(f.r, collectorLimit, time.Second)
	startDispatch(t, f)
	if got := f.ledger(t).Status; got.DispatchState != "Armed" || got.Phase != "Blocked" || got.BlocksProduced != 0 || f.rpc.sends != 1 {
		t.Fatalf("restart invented lost local evidence: %#v", got)
	}
}

func TestEnvironmentAbandonmentCancelsOutstandingCollector(t *testing.T) {
	f := fixture(t)
	started := make(chan struct{})
	f.rpc.generate = func(ctx context.Context) error { close(started); <-ctx.Done(); return ctx.Err() }
	startDispatch(t, f)
	<-started
	if err := f.r.Client.Delete(context.Background(), f.parent); err != nil {
		t.Fatal(err)
	}
	startDispatch(t, f)
	f.waitCollectors(t)
	if got := f.ledger(t).Status; got.Phase != "Abandoned" || got.DispatchState != "Armed" || got.BlocksProduced != 0 {
		t.Fatalf("abandonment claimed recovery: %#v", got)
	}
}
