package bitcoincontrol

import (
	"context"
	"testing"
	"time"

	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/foundation"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/naming"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func (f *testFixture) readInitial(t *testing.T) *bitcoin.BitcoinInitialization {
	t.Helper()
	record := &bitcoin.BitcoinInitialization{}
	if e := f.c.Get(context.Background(), client.ObjectKeyFromObject(f.initial), record); e != nil {
		t.Fatal(e)
	}
	return record
}
func TestSchedulerFreezesFirstGateAndFundsBeforeMaturity(t *testing.T) {
	f := newFixture(t)
	f.worker.Now = func() time.Time { return f.now }
	s := f.schedulerFor()
	if e := f.worker.Step(context.Background()); e != nil {
		t.Fatal(e)
	}
	f.reconcile(t, s)
	for expected := int64(1); expected <= 203; expected++ {
		f.now = f.now.Add(time.Second)
		f.reconcile(t, s)
		f.reconcile(t, s)
		record := f.readRecord(t)
		if record.Spec.Offer == nil || record.Spec.Offer.ExpectedHeight != expected-1 || record.Spec.Offer.Ceiling != 203 {
			t.Fatalf("height %d offer %+v", expected, record.Spec.Offer)
		}
		if e := f.worker.Step(context.Background()); e != nil {
			t.Fatalf("height %d: %v", expected, e)
		}
		eventually(t, func() bool { return f.readRecord(t).Status.CompletedOffer == expected })
		if e := f.worker.Step(context.Background()); e != nil {
			t.Fatal(e)
		}
		f.reconcile(t, s)
	}
	initial := f.readInitial(t)
	if initial.Status.PreparedAt == nil || initial.Status.Phase != "Held" || initial.Status.Reason != "NextGateNotImplemented" || initial.Status.LastAccountedOffer != 203 || len(initial.Status.Funded) != 1 || initial.Status.Funded[0].Outputs != 203 {
		t.Fatalf("unexpected first-gate result %+v", initial.Status)
	}
	before := f.rpc.count()
	for range 3 {
		f.now = f.now.Add(time.Second)
		f.reconcile(t, s)
		_ = f.worker.Step(context.Background())
	}
	if f.rpc.count() != before {
		t.Fatal("crossed frozen first ceiling")
	}
}

func TestSchedulerHoldsUnknownAndStaleObservations(t *testing.T) {
	for _, kind := range []string{"unknown", "stale", "process", "wallet", "failed"} {
		t.Run(kind, func(t *testing.T) {
			f := newFixture(t)
			if e := f.worker.Step(context.Background()); e != nil {
				t.Fatal(e)
			}
			record := f.readRecord(t)
			switch kind {
			case "unknown":
				record.Status.Armed = &bitcoin.BitcoinArmedRPC{ID: "unknown"}
			case "stale":
				record.Status.Observation.ObservedAt = metav1.NewTime(f.now.Add(-time.Minute))
			case "process":
				record.Status.Observation.Target.ContainerID = "old-process"
			case "wallet":
				record.Status.Observation.Wallets = nil
			case "failed":
				root := &api.StacksNetwork{}
				_ = f.c.Get(context.Background(), client.ObjectKeyFromObject(f.root), root)
				root.Status.Conditions = []metav1.Condition{{Type: "Failed", Status: metav1.ConditionTrue}}
				_ = f.c.Status().Update(context.Background(), root)
			}
			if e := f.c.Status().Update(context.Background(), record); e != nil {
				t.Fatal(e)
			}
			s := f.schedulerFor()
			f.reconcile(t, s)
			f.now = f.now.Add(time.Second)
			f.reconcile(t, s)
			if f.readInitial(t).Status.Offer != nil {
				t.Fatal("incomplete observation authorized generation")
			}
		})
	}
}

func TestCadenceAndCeilingDeadlineSurvivePause(t *testing.T) {
	f := newFixture(t)
	f.rpc.height = 203
	if e := f.worker.Step(context.Background()); e != nil {
		t.Fatal(e)
	}
	record := f.readRecord(t)
	record.Status.Observation.Wallets[0].MatureOutputs = 0
	if e := f.c.Status().Update(context.Background(), record); e != nil {
		t.Fatal(e)
	}
	s := f.schedulerFor()
	f.reconcile(t, s)
	started := f.readInitial(t).Status.FirstCeilingObservedAt
	if started == nil {
		t.Fatal("ceiling deadline not retained")
	}
	root := &api.StacksNetwork{}
	_ = f.c.Get(context.Background(), client.ObjectKeyFromObject(f.root), root)
	root.Spec.Operation = "Paused"
	_ = f.c.Update(context.Background(), root)
	f.now = f.now.Add(121 * time.Second)
	f.reconcile(t, s)
	if state := f.readInitial(t).Status; state.Phase != "Blocked" || state.Reason != "PrepareBitcoinObservationDeadline" {
		t.Fatal("pause hid an expired initialization deadline", state)
	}
	root.Spec.Operation = "Running"
	_ = f.c.Update(context.Background(), root)
	record = f.readRecord(t)
	record.Status.Observation.ObservedAt = metav1.NewTime(f.now)
	record.Status.Observation.Wallets[0].MatureOutputs = 10
	_ = f.c.Status().Update(context.Background(), record)
	f.reconcile(t, s)
	after := f.readInitial(t)
	if after.Status.Reason != "PrepareBitcoinObservationDeadline" || after.Status.PreparedAt != nil || !after.Status.FirstCeilingObservedAt.Equal(started) {
		t.Fatal("pause reset frozen ceiling deadline")
	}
}

func TestUniformCadenceDrawOnlyWhenChoosingNewAnchor(t *testing.T) {
	f := newFixture(t)
	production := &api.StacksNetworkParticipant{}
	_ = f.c.Get(context.Background(), client.ObjectKeyFromObject(f.production), production)
	production.Status.Admission.Configuration.BitcoinBlockProduction.Schedule = &bitcoin.BitcoinBlockScheduleSpec{Cadence: bitcoin.Cadence{Mode: "Uniform", MinimumInterval: ptr.To(common.Duration("1s")), MaximumInterval: ptr.To(common.Duration("3s"))}}
	production.Status.Admission.PolicyDigest = foundation.Digest(production.Status.Admission.Configuration)
	_ = f.c.Status().Update(context.Background(), production)
	if e := f.worker.Step(context.Background()); e != nil {
		t.Fatal(e)
	}
	draws := 0
	s := f.schedulerFor()
	s.Draw = func(int64) int64 { draws++; return 0 }
	f.reconcile(t, s)
	f.reconcile(t, s)
	if draws != 1 {
		t.Fatalf("redrew stable anchor %d times", draws)
	}
}

// countingReader distinguishes allocator runtime reads from identity graph resolution.
type countingReader struct {
	client.Reader
	walletReads int
}

func (r *countingReader) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	if _, ok := obj.(*bitcoin.BitcoinWallet); ok {
		r.walletReads++
	}
	return r.Reader.Get(ctx, key, obj, opts...)
}
func TestAllocatorRetainsLossAndSkipsExistingIdentityGraph(t *testing.T) {
	f := newFixture(t)
	actualName := naming.RuntimeName(string(f.root.UID), string(f.node.UID), "BitcoinNode", f.node.Spec.ParticipantName, "execution")
	record := f.readRecord(t)
	if e := f.c.Delete(context.Background(), record); e != nil {
		t.Fatal(e)
	}
	record.ResourceVersion = ""
	record.Name = actualName
	if e := f.c.Create(context.Background(), record); e != nil {
		t.Fatal(e)
	}
	f.record = record
	f.root.Status.Bitcoin.ExecutionRefs[0].Name = actualName
	reader := &countingReader{Reader: f.c}
	refs, init, e := EnsureRecords(context.Background(), f.c, reader, f.c.Scheme(), f.root)
	if e != nil || len(refs) != 1 || init == nil || reader.walletReads != 0 {
		t.Fatalf("existing allocator crossed identity graph: %v", e)
	}
	if e = f.c.Delete(context.Background(), f.record); e != nil {
		t.Fatal(e)
	}
	if _, _, e = EnsureRecords(context.Background(), f.c, reader, f.c.Scheme(), f.root); e == nil {
		t.Fatal("recreated pinned execution record")
	}
}

func TestTerminalDrainRetainsUnknownOutcome(t *testing.T) {
	f := newFixture(t)
	f.offer(t, 1)
	f.rpc.fail = true
	if e := f.worker.Step(context.Background()); e != nil {
		t.Fatal(e)
	}
	eventually(t, func() bool { return f.rpc.count() == 1 })
	root := &api.StacksNetwork{}
	_ = f.c.Get(context.Background(), client.ObjectKeyFromObject(f.root), root)
	root.Spec.Operation = "Stopped"
	root.Generation++
	_ = f.c.Update(context.Background(), root)
	if e := f.worker.Step(context.Background()); e != nil {
		t.Fatal(e)
	}
	record := f.readRecord(t)
	if record.Status.Drain == nil || record.Status.Drain.Outcome != "Uncertain" || record.Status.Armed == nil {
		t.Fatal("terminal stop erased unknown RPC outcome")
	}
	ready, e := CheckDrained(context.Background(), f.c, f.node)
	if e != nil || !ready {
		t.Fatalf("retained uncertainty did not permit disposal: %v", e)
	}
}

func TestCadenceRejectsInvalidBounds(t *testing.T) {
	s := &Scheduler{Draw: func(int64) int64 { return 0 }}
	for _, schedule := range []*bitcoin.BitcoinBlockScheduleSpec{nil, {Cadence: bitcoin.Cadence{Mode: "Fixed", Interval: ptr.To(common.Duration("0s"))}}, {Cadence: bitcoin.Cadence{Mode: "Uniform", MinimumInterval: ptr.To(common.Duration("2s")), MaximumInterval: ptr.To(common.Duration("1s"))}}} {
		if _, e := s.interval(schedule); e == nil {
			t.Fatal("invalid cadence accepted")
		}
	}
}

func TestStopBeforeWorkerActivationDoesNotRequireRuntimePointerAbsence(t *testing.T) {
	for _, state := range []string{"AdmissionUnavailable", "StartupPaused"} {
		for _, operation := range []string{"stop", "delete"} {
			t.Run(state+"/"+operation, func(t *testing.T) {
				f := newFixture(t)
				root := &api.StacksNetwork{}
				if e := f.c.Get(context.Background(), client.ObjectKeyFromObject(f.root), root); e != nil {
					t.Fatal(e)
				}
				root.Status.Bitcoin = nil
				if e := f.c.Status().Update(context.Background(), root); e != nil {
					t.Fatal(e)
				}
				if operation == "stop" {
					root.Spec.Operation = "Stopped"
				} else {
					root.Spec.Participants = nil
				}
				if e := f.c.Update(context.Background(), root); e != nil {
					t.Fatal(e)
				}
				p := f.node.DeepCopy()
				p.Status.Runtime = &api.ParticipantRuntimeStatus{ObservedGeneration: p.Generation}
				p.Status.Conditions = []metav1.Condition{{Type: "WorkloadReady", Status: metav1.ConditionFalse, Reason: state}}
				ready, e := CheckDrained(context.Background(), f.c, p)
				if e != nil || !ready {
					t.Fatalf("never-activated worker blocked disposal: %v", e)
				}
				p.Status.BitcoinControl = &api.BitcoinControlRuntimeStatus{DeploymentRef: &common.Binding{Kind: "Deployment", Name: "prior-worker", UID: "prior-worker-uid"}}
				if ready, e = CheckDrained(context.Background(), f.c, p); e != nil || ready {
					t.Fatal("historical control identity was treated as never activated")
				}
				p.Status.BitcoinControl = nil
				p.Status.Runtime.PodRef = &common.Binding{Kind: "Pod", Name: "prior-pod", UID: "prior-uid"}
				if ready, e = CheckDrained(context.Background(), f.c, p); e != nil || !ready {
					t.Fatal("an actor Pod was mistaken for control-worker activation")
				}
				p.Status.BitcoinControl = &api.BitcoinControlRuntimeStatus{Pods: []api.BitcoinControlPodStatus{{UID: "prior-control-pod"}}}
				if ready, e = CheckDrained(context.Background(), f.c, p); e != nil || ready {
					t.Fatal("known control process without its record authorized disposal")
				}
			})
		}
	}
}

// TestBootstrapCadenceChangePreservesOpportunitySequence exercises admission changes before initialization completes.
func TestBootstrapCadenceChangePreservesOpportunitySequence(t *testing.T) {
	f := newFixture(t)
	f.worker.Now = func() time.Time { return f.now }
	ctx := context.Background()
	s := f.schedulerFor()
	if err := f.worker.Step(ctx); err != nil {
		t.Fatal(err)
	}
	f.reconcile(t, s)
	f.now = f.now.Add(time.Second)
	f.reconcile(t, s)
	f.reconcile(t, s)
	if err := f.worker.Step(ctx); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { return f.readRecord(t).Status.CompletedOffer == 1 })
	if err := f.worker.Step(ctx); err != nil {
		t.Fatal(err)
	}
	f.reconcile(t, s)
	if f.readInitial(t).Status.LastAccountedOffer != 1 {
		t.Fatal("first receipt not accounted")
	}
	var production api.StacksNetworkParticipant
	if err := f.c.Get(ctx, client.ObjectKeyFromObject(f.production), &production); err != nil {
		t.Fatal(err)
	}
	production.Status.Admission.Configuration.BitcoinBlockProduction.Schedule.Cadence.Interval = ptr.To(common.Duration("2s"))
	production.Status.Admission.PolicyDigest = foundation.Digest(production.Status.Admission.Configuration)
	if err := f.c.Status().Update(ctx, &production); err != nil {
		t.Fatal(err)
	}
	f.reconcile(t, s)
	f.reconcile(t, s)
	f.now = f.now.Add(2 * time.Second)
	if err := f.worker.Step(ctx); err != nil {
		t.Fatal(err)
	}
	f.reconcile(t, s)
	f.reconcile(t, s)
	offer := f.readRecord(t).Spec.Offer
	if offer == nil || offer.Number != 2 || offer.ExpectedHeight != 1 || offer.PolicyDigest != production.Status.Admission.PolicyDigest {
		t.Fatalf("replacement cadence reused an old opportunity: %+v", offer)
	}
	if err := f.worker.Step(ctx); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { return f.readRecord(t).Status.CompletedOffer == 2 })
	if f.rpc.count() != 2 {
		t.Fatalf("expected exactly one new generation, got %d total", f.rpc.count())
	}
}
