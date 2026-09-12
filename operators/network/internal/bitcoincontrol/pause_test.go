package bitcoincontrol

import (
	"context"
	"testing"
	"time"

	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/foundation"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestLongPauseRefreshesNativeFactsAndOwnAcknowledgementWithoutSending(t *testing.T) {
	for _, walletMissing := range []bool{false, true} {
		t.Run(map[bool]string{false: "ready-wallet", true: "wallet-needs-mutation"}[walletMissing], func(t *testing.T) {
			f := newFixture(t)
			ctx := context.Background()
			f.worker.Now = func() time.Time { return f.now }
			root := &api.StacksNetwork{}
			if err := f.c.Get(ctx, client.ObjectKeyFromObject(f.root), root); err != nil {
				t.Fatal(err)
			}
			root.Spec.Operation = "Paused"
			if err := f.c.Update(ctx, root); err != nil {
				t.Fatal(err)
			}
			f.rpc.exists = !walletMissing
			f.rpc.loaded = !walletMissing
			f.rpc.imported = !walletMissing
			var original time.Time
			for elapsed := 0; elapsed <= 100; elapsed += 5 {
				f.now = f.now.Add(5 * time.Second)
				f.rpc.height = 203 + int64(elapsed)
				if err := f.worker.Step(ctx); err != nil {
					t.Fatal(err)
				}
				record := f.readRecord(t)
				if record.Status.Observation == nil || record.Status.Observation.Height != f.rpc.height || f.now.Sub(record.Status.Observation.ObservedAt.Time) > foundation.ObservationFreshness() || record.Status.Control == nil || record.Status.Control.ProcessNonce != f.worker.ProcessNonce || !record.Status.Control.HeartbeatAt.Time.Equal(f.now) {
					t.Fatalf("paused observations or heartbeat went stale: %+v", record.Status)
				}
				if original.IsZero() {
					original = record.Status.Control.ObservedAt.Time
				} else if !original.Equal(record.Status.Control.ObservedAt.Time) {
					t.Fatal("pause heartbeat changed original acknowledgement")
				}
				if record.Status.Armed != nil || f.rpc.count() != 0 {
					t.Fatal("paused observation dispatched a mutation")
				}
			}
			if _, err := f.worker.authorize(ctx, f.readRecord(t)); err == nil {
				t.Fatal("read-only pause admission authorized mutation")
			}
		})
	}
}

func TestPausedObservationPreservesUnknownArmAndCannotAcknowledgeQuiescence(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.worker.Now = func() time.Time { return f.now }
	root := &api.StacksNetwork{}
	if err := f.c.Get(ctx, client.ObjectKeyFromObject(f.root), root); err != nil {
		t.Fatal(err)
	}
	root.Spec.Operation = "Paused"
	if err := f.c.Update(ctx, root); err != nil {
		t.Fatal(err)
	}
	record := f.readRecord(t)
	record.Status.Armed = &bitcoin.BitcoinArmedRPC{ID: "unknown-prior-process", ProcessNonce: "old"}
	if err := f.c.Status().Update(ctx, record); err != nil {
		t.Fatal(err)
	}
	if err := f.worker.Step(ctx); err != nil {
		t.Fatal(err)
	}
	record = f.readRecord(t)
	if record.Status.Armed == nil || record.Status.Armed.ID != "unknown-prior-process" || record.Status.Control != nil || record.Status.Observation == nil || f.rpc.count() != 0 {
		t.Fatal("native reads cleared unknown authority or claimed pause quiescence", record.Status)
	}
}

func TestPauseSameRootRevisionRebindsRestartedProcess(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.worker.Now = func() time.Time { return f.now }
	root := &api.StacksNetwork{}
	if err := f.c.Get(ctx, client.ObjectKeyFromObject(f.root), root); err != nil {
		t.Fatal(err)
	}
	root.Spec.Operation = "Paused"
	if err := f.c.Update(ctx, root); err != nil {
		t.Fatal(err)
	}
	if err := f.worker.Step(ctx); err != nil {
		t.Fatal(err)
	}
	old := f.readRecord(t).Status.Control.DeepCopy()
	f.now = f.now.Add(time.Second)
	f.worker.ProcessNonce = "restarted-process"
	if err := f.worker.Step(ctx); err != nil {
		t.Fatal(err)
	}
	ack := f.readRecord(t).Status.Control
	if ack.NetworkGeneration != old.NetworkGeneration || ack.ProcessNonce == old.ProcessNonce || !ack.ObservedAt.Time.Equal(f.now) || !ack.HeartbeatAt.Time.Equal(f.now) {
		t.Fatal("same-generation restart inherited prior process acknowledgement", ack)
	}
}
