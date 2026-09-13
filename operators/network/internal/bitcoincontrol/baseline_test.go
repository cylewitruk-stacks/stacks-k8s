package bitcoincontrol

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/foundation"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

// baselineFixture starts from a completed immutable bootstrap and two independent actors.
func baselineFixture(t *testing.T) *testFixture {
	t.Helper()
	f := newFixture(t)
	ctx := context.Background()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(f.c.Get(ctx, client.ObjectKeyFromObject(f.node), f.node))
	f.node.Status.Runtime.ObservedGeneration = f.node.Generation
	meta.SetStatusCondition(&f.node.Status.Conditions, metav1.Condition{Type: "WorkloadReady", Status: metav1.ConditionTrue, ObservedGeneration: f.node.Generation, Reason: "Ready", Message: "Ready", LastTransitionTime: metav1.NewTime(f.now)})
	must(f.c.Status().Update(ctx, f.node))
	other := f.node.DeepCopy()
	other.Name = "node-two"
	other.UID = "node-two-uid"
	other.ResourceVersion = ""
	other.Spec.ParticipantName = "bitcoin-two"
	other.Status.Runtime.PodRef.UID = "pod-two-uid"
	must(f.c.Create(ctx, other))
	second := f.record.DeepCopy()
	second.Name = "execution-two"
	second.UID = "execution-two-uid"
	second.ResourceVersion = ""
	second.Spec.Participant = binding("StacksNetworkParticipant", other)
	must(f.c.Create(ctx, second))
	must(f.c.Get(ctx, client.ObjectKeyFromObject(f.root), f.root))
	f.root.Spec.Participants = append(f.root.Spec.Participants, api.Participant{Name: "bitcoin-two", Kind: "BitcoinNode"})
	must(f.c.Update(ctx, f.root))
	f.root.Status.Identities = append(f.root.Status.Identities, api.InstanceIdentity{Name: "bitcoin-two", UID: other.UID})
	f.root.Status.Bitcoin.ExecutionRefs = append(f.root.Status.Bitcoin.ExecutionRefs, binding("BitcoinExecution", second))
	f.root.Status.Initialization = &api.InitializationStatus{GenesisUID: f.root.Status.GenesisRef.UID, GenesisDigest: f.root.Status.GenesisDigest, GateIndex: 2, AuthorizedCeiling: 234, Completed: true, Gates: []api.GateObservation{{Name: "PrepareBitcoin", CompletedAt: ptr.To(metav1.NewTime(f.now))}, {Name: "EnrollPoX4", CompletedAt: ptr.To(metav1.NewTime(f.now))}}}
	must(f.c.Status().Update(ctx, f.root))
	var wallet bitcoin.BitcoinWallet
	must(f.c.Get(ctx, client.ObjectKey{Namespace: "test", Name: "miner"}, &wallet))
	wallet.Status.Identity = &common.PublicIdentity{}
	wallet.Status.Conditions = []metav1.Condition{{Type: "Resolved", Status: metav1.ConditionTrue, Reason: "Resolved", Message: "Resolved", LastTransitionTime: metav1.NewTime(f.now)}}
	must(f.c.Update(ctx, &wallet))
	must(f.c.Get(ctx, client.ObjectKeyFromObject(f.production), f.production))
	policy := f.production.Status.Admission.Configuration.BitcoinBlockProduction
	policy.PayoutWalletRef = &common.NameRef{Name: "miner"}
	policy.Initialization = &bitcoin.Initialization{TargetNodeRef: common.NameRef{Name: "bitcoin"}, MinimumHeight: 203, MatureOutputsPerMiner: 1, MinerWalletRefs: ptr.To([]common.NameRef{{Name: "miner"}})}
	policy.Targets = ptr.To([]bitcoin.ProductionTarget{{NodeRef: common.NameRef{Name: "bitcoin"}, Weight: 1}, {NodeRef: common.NameRef{Name: "bitcoin-two"}, Weight: 3}})
	f.production.Status.Admission.Dependencies = []common.Binding{f.initial.Spec.PayoutWallet.Wallet, binding("StacksNetworkParticipant", f.node), binding("StacksNetworkParticipant", other)}
	f.production.Status.Admission.PolicyDigest = foundation.Digest(f.production.Status.Admission.Configuration)
	must(f.c.Status().Update(ctx, f.production))
	f.initial = f.readInitial(t)
	f.initial.Status.PreparedAt = ptr.To(metav1.NewTime(f.now))
	f.initial.Status.LastAccountedOffer = 203
	f.initial.Status.Funded = []bitcoin.BitcoinFundingCount{{WalletUID: f.initial.Spec.PayoutWallet.Wallet.UID, Outputs: 203}}
	must(f.c.Status().Update(ctx, f.initial))
	f.worker.Now = func() time.Time { return f.now }
	for _, pair := range []struct {
		p *api.StacksNetworkParticipant
		e *bitcoin.BitcoinExecution
	}{{f.node, f.record}, {other, second}} {
		must(f.c.Get(ctx, client.ObjectKeyFromObject(pair.e), pair.e))
		rt := pair.p.Status.Runtime
		pair.e.Status.Observation = &bitcoin.BitcoinObservation{Target: bitcoin.BitcoinTargetIdentity{Participant: binding("StacksNetworkParticipant", pair.p), Pod: *rt.PodRef, ContainerID: rt.ContainerID, Configuration: *rt.ConfigRef, Credentials: *rt.RPCSecretRef, PolicyDigest: rt.PolicyDigest}, Height: 234, Tip: strings.Repeat("a", 64), ObservedAt: metav1.NewTime(f.now), Wallets: []bitcoin.BitcoinWalletObservation{{Wallet: f.initial.Spec.PayoutWallet.Wallet, Name: "miner", Address: testAddress, Ready: true}}}
		must(f.c.Status().Update(ctx, pair.e))
	}
	return f
}

// armBaseline adopts policy and establishes its first future anchor.
func armBaseline(t *testing.T, f *testFixture, s *Scheduler) {
	t.Helper()
	if _, err := baselineInputs(context.Background(), s.Reader, f.root, f.readInitial(t), f.production); err != nil {
		t.Fatal(err)
	}
	f.reconcile(t, s)
	f.reconcile(t, s)
	state := f.readInitial(t).Status
	if state.NextOpportunityAt == nil || state.Baseline == nil || state.Baseline.Scheduling.EligibleTargets != 2 {
		t.Fatalf("baseline not armed: %+v", state)
	}
}

func TestBaselineWeightedBusyTargetSkipsWithoutRedraw(t *testing.T) {
	f := baselineFixture(t)
	s := f.schedulerFor()
	draws := 0
	s.Draw = func(n int64) int64 {
		draws++
		if draws == 1 {
			return 0
		}
		return n - 1
	}
	armBaseline(t, f, s)
	record := f.readRecord(t)
	record.Status.Armed = &bitcoin.BitcoinArmedRPC{ID: "pending-old-call"}
	if err := f.c.Status().Update(context.Background(), record); err != nil {
		t.Fatal(err)
	}
	f.now = f.now.Add(time.Second)
	f.reconcile(t, s)
	f.reconcile(t, s)
	state := f.readInitial(t).Status
	if draws != 1 || state.Baseline.Stage != "Skipped" || state.Baseline.Scheduling.Skipped != 1 || state.Baseline.Scheduling.EligibleTargets != 1 {
		t.Fatalf("busy selection redrawn or globally blocked: draws %d state %+v", draws, state)
	}
	f.now = f.now.Add(time.Second)
	for range 3 {
		f.reconcile(t, s)
	}
	state = f.readInitial(t).Status
	if draws != 2 || state.Baseline.Stage != "Assigned" || state.Offer.Target.UID != "node-two-uid" || state.Baseline.Scheduling.Assigned != 1 {
		t.Fatalf("healthy peer did not progress: %+v draws %d", state, draws)
	}
	if f.readRecord(t).Status.Armed == nil || len(state.Funded) != 1 || state.Funded[0].Outputs != 203 || state.LastAccountedOffer != 203 {
		t.Fatal("baseline changed pending RPC or initial funding")
	}
}

func TestBaselineLostSelectionAndAssignmentAcknowledgements(t *testing.T) {
	for _, boundary := range []string{"selection", "assignment"} {
		t.Run(boundary, func(t *testing.T) {
			f := baselineFixture(t)
			s := f.schedulerFor()
			draws := 0
			s.Draw = func(int64) int64 { draws++; return 0 }
			armBaseline(t, f, s)
			f.now = f.now.Add(time.Second)
			lost := false
			s.Client = interceptor.NewClient(f.c.(client.WithWatch), interceptor.Funcs{
				SubResourceUpdate: func(ctx context.Context, c client.Client, sub string, obj client.Object, opts ...client.SubResourceUpdateOption) error {
					err := c.SubResource(sub).Update(ctx, obj, opts...)
					if initial, ok := obj.(*bitcoin.BitcoinInitialization); ok && err == nil && !lost && boundary == "selection" && initial.Status.Baseline.Stage == "Selected" {
						lost = true
						return errors.New("selection response lost")
					}
					return err
				},
				Update: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
					err := c.Update(ctx, obj, opts...)
					if _, ok := obj.(*bitcoin.BitcoinExecution); ok && err == nil && !lost && boundary == "assignment" {
						lost = true
						return errors.New("assignment response lost")
					}
					return err
				},
			})
			failures := 0
			for range 5 {
				if _, err := s.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(f.initial)}); err != nil {
					failures++
				}
			}
			state := f.readInitial(t).Status
			if !lost || failures != 1 || draws != 1 || state.Baseline.Scheduling.Opportunities != 1 || state.Baseline.Scheduling.Assigned != 1 || state.Baseline.Stage != "Assigned" {
				t.Fatalf("lost ack changed durable selection: %+v draws %d failures %d", state, draws, failures)
			}
		})
	}
}

func TestBaselineNoCatchUpAndPauseUsesLatestPolicy(t *testing.T) {
	f := baselineFixture(t)
	s := f.schedulerFor()
	armBaseline(t, f, s)
	f.now = f.now.Add(time.Hour)
	f.reconcile(t, s)
	state := f.readInitial(t).Status
	if state.Baseline.Scheduling.Opportunities != 1 || !state.NextOpportunityAt.Time.Equal(f.now.Add(time.Second)) {
		t.Fatal("missed opportunities caught up", state)
	}
	root := f.root.DeepCopy()
	_ = f.c.Get(context.Background(), client.ObjectKeyFromObject(root), root)
	root.Spec.Operation = "Paused"
	if e := f.c.Update(context.Background(), root); e != nil {
		t.Fatal(e)
	}
	f.reconcile(t, s)
	state = f.readInitial(t).Status
	if state.NextOpportunityAt != nil || state.Baseline.Scheduling.Unassigned != 1 {
		t.Fatal("pause retained unsent authority", state)
	}
	p := f.production.DeepCopy()
	_ = f.c.Get(context.Background(), client.ObjectKeyFromObject(p), p)
	p.Status.Admission.Configuration.BitcoinBlockProduction.Schedule.Cadence.Interval = ptr.To(common.Duration("2m"))
	p.Status.Admission.PolicyDigest = foundation.Digest(p.Status.Admission.Configuration)
	if e := f.c.Status().Update(context.Background(), p); e != nil {
		t.Fatal(e)
	}
	f.reconcile(t, s)
	root.Spec.Operation = "Running"
	if e := f.c.Update(context.Background(), root); e != nil {
		t.Fatal(e)
	}
	f.reconcile(t, s)
	state = f.readInitial(t).Status
	if !state.NextOpportunityAt.Time.Equal(f.now.Add(2*time.Minute)) || state.Baseline.Scheduling.ProgressWindowSeconds != 370 || state.Baseline.Scheduling.Opportunities != 1 {
		t.Fatal("resume restored old cadence or caught up", state)
	}
}

func TestBaselineRetainsAdmissionAcrossRejectedCandidateButClosesLostDependency(t *testing.T) {
	f := baselineFixture(t)
	ctx := context.Background()
	s := f.schedulerFor()
	p := f.production.DeepCopy()
	_ = f.c.Get(ctx, client.ObjectKeyFromObject(p), p)
	schedule := &bitcoin.BitcoinBlockSchedule{ObjectMeta: metav1.ObjectMeta{Name: "admitted", Namespace: "test", UID: "schedule-uid"}, Spec: *p.Status.Admission.Configuration.BitcoinBlockProduction.Schedule.DeepCopy()}
	if e := f.c.Create(ctx, schedule); e != nil {
		t.Fatal(e)
	}
	pin := binding("BitcoinBlockSchedule", schedule)
	pin.Fingerprint = foundation.Digest(schedule.Spec)
	p.Status.Admission.Dependencies = append(p.Status.Admission.Dependencies, pin)
	if e := f.c.Status().Update(ctx, p); e != nil {
		t.Fatal(e)
	}
	armBaseline(t, f, s)
	old := f.readInitial(t).Status.Baseline.Scheduling.AdmissionDigest
	if e := f.c.Get(ctx, client.ObjectKeyFromObject(p), p); e != nil {
		t.Fatal(e)
	}
	p.Spec.Configuration = api.Configuration{BitcoinBlockProduction: &bitcoin.BitcoinBlockProductionSpec{ScheduleRef: &common.NameRef{Name: "missing-candidate"}}}
	if e := f.c.Update(ctx, p); e != nil {
		t.Fatal(e)
	}
	meta.SetStatusCondition(&p.Status.Conditions, metav1.Condition{Type: "Resolved", Status: metav1.ConditionFalse, Reason: "RequiresReplacement", Message: "Candidate rejected", LastTransitionTime: metav1.NewTime(f.now)})
	if e := f.c.Status().Update(ctx, p); e != nil {
		t.Fatal(e)
	}
	f.now = f.now.Add(time.Second)
	for range 3 {
		f.reconcile(t, s)
	}
	state := f.readInitial(t).Status
	if state.Baseline.Scheduling.AdmissionDigest != old || state.Baseline.Stage != "Assigned" {
		t.Fatal("rejected candidate stopped retained admission", state)
	}
	if e := f.c.Delete(ctx, schedule); e != nil {
		t.Fatal(e)
	}
	replacement := schedule.DeepCopy()
	replacement.ResourceVersion = ""
	replacement.UID = "replacement-schedule-uid"
	if e := f.c.Create(ctx, replacement); e != nil {
		t.Fatal(e)
	}
	f.reconcile(t, s)
	state = f.readInitial(t).Status
	if state.Offer != nil || state.Reason != "BaselineInputsUnavailable" || state.Baseline.Scheduling.EligibleTargets != 0 {
		t.Fatal("replacement schedule inherited authority", state)
	}
}

func TestBaselineReceiptCursorKeepsOriginalTimeAcrossProducerDeletion(t *testing.T) {
	f := baselineFixture(t)
	s := f.schedulerFor()
	armBaseline(t, f, s)
	f.now = f.now.Add(time.Second)
	for range 3 {
		f.reconcile(t, s)
	}
	record := f.readRecord(t)
	offer := record.Spec.Offer.DeepCopy()
	received := metav1.NewTime(f.now)
	record.Status.CompletedOffer = offer.Number
	record.Status.LastReceipt = &bitcoin.BitcoinRPCReceipt{Request: bitcoin.BitcoinArmedRPC{Method: "Generate", Offer: offer, Target: record.Status.Observation.Target}, BlockHash: strings.Repeat("b", 64), ReceivedAt: received}
	if e := f.c.Status().Update(context.Background(), record); e != nil {
		t.Fatal(e)
	}
	if e := f.c.Delete(context.Background(), f.production); e != nil {
		t.Fatal(e)
	}
	f.reconcile(t, s)
	f.now = f.now.Add(time.Second)
	f.reconcile(t, s)
	state := f.readInitial(t).Status
	if state.Baseline.Scheduling.Acknowledged != 1 || !state.Baseline.Scheduling.LastAcknowledgedAt.Equal(&received) || len(state.Baseline.Receipts) != 1 || !generationAccounted(f.readInitial(t), record) || state.LastAccountedOffer != 203 || state.Funded[0].Outputs != 203 {
		t.Fatal("receipt replayed, retimed, or lost during producer removal", state)
	}
}

func TestBaselineWorkerExecutesOneAttributedBlockWithoutBootstrapReplay(t *testing.T) {
	f := baselineFixture(t)
	s := f.schedulerFor()
	f.rpc.height = 234
	if err := f.worker.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	armBaseline(t, f, s)
	f.now = f.now.Add(time.Second)
	for range 3 {
		f.reconcile(t, s)
	}
	offer := f.readRecord(t).Spec.Offer
	if offer == nil || offer.Mode != "Baseline" {
		t.Fatal("baseline offer missing")
	}
	if err := f.worker.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool { return f.readRecord(t).Status.CompletedOffer == offer.Number })
	f.reconcile(t, s)
	state := f.readInitial(t).Status
	if state.Baseline.Scheduling.Acknowledged != 1 || state.Funded[0].Outputs != 203 || state.LastAccountedOffer != 203 || f.rpc.count() != 1 {
		t.Fatalf("baseline repeated funding or lost receipt: %+v calls %d", state, f.rpc.count())
	}
}

func TestBaselineAccountsIndependentOutOfOrderReceipts(t *testing.T) {
	f := baselineFixture(t)
	s := f.schedulerFor()
	ctx := context.Background()
	armBaseline(t, f, s)
	f.now = f.now.Add(time.Second)
	for range 3 {
		f.reconcile(t, s)
	}
	first := f.readRecord(t)
	first.Status.Armed = &bitcoin.BitcoinArmedRPC{ID: "first-still-running"}
	if err := f.c.Status().Update(ctx, first); err != nil {
		t.Fatal(err)
	}
	s.Draw = func(n int64) int64 { return n - 1 }
	f.now = f.now.Add(time.Second)
	for range 3 {
		f.reconcile(t, s)
	}
	second := &bitcoin.BitcoinExecution{}
	if err := f.c.Get(ctx, client.ObjectKey{Namespace: "test", Name: "execution-two"}, second); err != nil {
		t.Fatal(err)
	}
	for _, record := range []*bitcoin.BitcoinExecution{second, first} {
		f.now = f.now.Add(100 * time.Millisecond)
		record.Status.Armed = nil
		record.Status.CompletedOffer = record.Spec.Offer.Number
		record.Status.LastReceipt = &bitcoin.BitcoinRPCReceipt{Request: bitcoin.BitcoinArmedRPC{Method: "Generate", Offer: record.Spec.Offer.DeepCopy(), Target: record.Status.Observation.Target}, ReceivedAt: metav1.NewTime(f.now), BlockHash: strings.Repeat("b", 64)}
		if err := f.c.Status().Update(ctx, record); err != nil {
			t.Fatal(err)
		}
		f.reconcile(t, s)
	}
	state := f.readInitial(t).Status
	if state.Baseline.Scheduling.Acknowledged != 2 || len(state.Baseline.Receipts) != 2 || !generationAccounted(f.readInitial(t), first) || !generationAccounted(f.readInitial(t), second) {
		t.Fatal("one node's receipt cursor hid another node's earlier receipt", state)
	}
}

func TestBaselineAdmissionDependencyChangeReanchorsSamePolicy(t *testing.T) {
	f := baselineFixture(t)
	s := f.schedulerFor()
	armBaseline(t, f, s)
	before := f.readInitial(t).Status.Baseline.Scheduling
	p := f.production.DeepCopy()
	ctx := context.Background()
	if err := f.c.Get(ctx, client.ObjectKeyFromObject(p), p); err != nil {
		t.Fatal(err)
	}
	p.Status.Admission.Dependencies[1].Fingerprint = "changed-public-binding"
	if err := f.c.Status().Update(ctx, p); err != nil {
		t.Fatal(err)
	}
	f.reconcile(t, s)
	after := f.readInitial(t).Status
	if after.Baseline.Scheduling.PolicyDigest != before.PolicyDigest || after.Baseline.Scheduling.AdmissionDigest == before.AdmissionDigest || after.NextOpportunityAt != nil {
		t.Fatal("same configuration hid changed admitted dependency identity", after)
	}
}

func TestBaselineCadenceBoundsAndWeightedRanges(t *testing.T) {
	for _, value := range []string{"999ms", "3601s", "24h", "bad"} {
		if _, _, err := cadenceBounds(&bitcoin.BitcoinBlockScheduleSpec{Cadence: bitcoin.Cadence{Mode: "Fixed", Interval: ptr.To(common.Duration(value))}}); err == nil {
			t.Fatalf("accepted invalid cadence %s", value)
		}
	}
	for _, value := range []string{"1s", "1h"} {
		if _, _, err := cadenceBounds(&bitcoin.BitcoinBlockScheduleSpec{Cadence: bitcoin.Cadence{Mode: "Fixed", Interval: ptr.To(common.Duration(value))}}); err != nil {
			t.Fatal(err)
		}
	}
	schedule := &bitcoin.BitcoinBlockScheduleSpec{Cadence: bitcoin.Cadence{Mode: "Uniform", MinimumInterval: ptr.To(common.Duration("1s")), MaximumInterval: ptr.To(common.Duration("2s"))}}
	s := &Scheduler{Draw: func(n int64) int64 { return n - 1 }}
	if got, err := s.interval(schedule); err != nil || got != 2*time.Second {
		t.Fatalf("uniform upper bound omitted: %v %v", got, err)
	}
	schedule.Cadence.MinimumInterval = ptr.To(common.Duration("3s"))
	if _, _, err := cadenceBounds(schedule); err == nil {
		t.Fatal("reversed uniform bounds accepted")
	}
	targets := []bitcoin.BitcoinScheduledTarget{{Participant: common.Binding{Name: "first"}, Weight: 1}, {Participant: common.Binding{Name: "second"}, Weight: 3}}
	for draw := int64(0); draw < 4; draw++ {
		s.Draw = func(n int64) int64 {
			if n != 4 {
				t.Fatalf("incorrect weight total %d", n)
			}
			return draw
		}
		got, err := s.selectBaselineTarget(targets)
		want := "second"
		if draw == 0 {
			want = "first"
		}
		if err != nil || got.Name != want {
			t.Fatalf("weighted range draw %d -> %s: %v", draw, got.Name, err)
		}
	}
}

func TestBaselineTerminalRootStillAccountsExistingReceipt(t *testing.T) {
	for _, operation := range []string{"Stopped", "Failed"} {
		t.Run(operation, func(t *testing.T) {
			f := baselineFixture(t)
			s := f.schedulerFor()
			ctx := context.Background()
			armBaseline(t, f, s)
			f.now = f.now.Add(time.Second)
			for range 3 {
				f.reconcile(t, s)
			}
			record := f.readRecord(t)
			record.Status.CompletedOffer = record.Spec.Offer.Number
			record.Status.LastReceipt = &bitcoin.BitcoinRPCReceipt{Request: bitcoin.BitcoinArmedRPC{Method: "Generate", Offer: record.Spec.Offer.DeepCopy(), Target: record.Status.Observation.Target}, ReceivedAt: metav1.NewTime(f.now), BlockHash: strings.Repeat("b", 64)}
			if err := f.c.Status().Update(ctx, record); err != nil {
				t.Fatal(err)
			}
			root := f.root.DeepCopy()
			if err := f.c.Get(ctx, client.ObjectKeyFromObject(root), root); err != nil {
				t.Fatal(err)
			}
			if operation == "Failed" {
				root.Status.Phase = "Failed"
				if err := f.c.Status().Update(ctx, root); err != nil {
					t.Fatal(err)
				}
			} else {
				root.Spec.Operation = operation
				if err := f.c.Update(ctx, root); err != nil {
					t.Fatal(err)
				}
			}
			f.reconcile(t, s)
			state := f.readInitial(t).Status
			if state.Baseline.Scheduling.Acknowledged != 1 || state.Offer != nil || state.NextOpportunityAt != nil {
				t.Fatal("terminal root lost receipt accounting or retained new-send authority", state)
			}
		})
	}
}

func TestBaselineRetiredBootstrapTargetAndReplacementProducerKeepSharedRecords(t *testing.T) {
	f := baselineFixture(t)
	ctx := context.Background()
	s := f.schedulerFor()
	armBaseline(t, f, s)
	original := f.readInitial(t).Spec.DeepCopy()
	replacement := f.production.DeepCopy()
	replacement.Name = foundation.ParticipantName(string(f.root.UID), "replacement")
	replacement.UID = "replacement-uid"
	replacement.ResourceVersion = ""
	replacement.Spec.ParticipantName = "replacement"
	replacement.Status.Scheduling = nil
	if err := f.c.Create(ctx, replacement); err != nil {
		t.Fatal(err)
	}
	root := f.root.DeepCopy()
	if err := f.c.Get(ctx, client.ObjectKeyFromObject(root), root); err != nil {
		t.Fatal(err)
	}
	root.Spec.Participants = []api.Participant{{Name: "replacement", Kind: "BitcoinBlockProduction"}, {Name: "bitcoin-two", Kind: "BitcoinNode"}}
	if err := f.c.Update(ctx, root); err != nil {
		t.Fatal(err)
	}
	for i := range root.Status.Identities {
		if root.Status.Identities[i].Name != "bitcoin-two" {
			root.Status.Identities[i].Removing = true
		}
	}
	root.Status.Identities = append(root.Status.Identities, api.InstanceIdentity{Name: "replacement", UID: replacement.UID})
	if err := f.c.Status().Update(ctx, root); err != nil {
		t.Fatal(err)
	}
	if err := f.c.Delete(ctx, f.node); err != nil {
		t.Fatal(err)
	}
	if err := f.c.Delete(ctx, f.production); err != nil {
		t.Fatal(err)
	}
	s.Draw = func(n int64) int64 { return n - 1 }
	f.reconcile(t, s)
	f.reconcile(t, s)
	f.now = f.now.Add(time.Second)
	for range 3 {
		f.reconcile(t, s)
	}
	state := f.readInitial(t)
	if state.Status.Production == nil || state.Status.Production.UID != replacement.UID || state.Status.Baseline.Scheduling.EligibleTargets != 1 || state.Status.Baseline.Stage != "Assigned" || state.Status.Offer.Target.UID != "node-two-uid" || state.Spec.Target != original.Target || state.Spec.Production != original.Production || state.Status.Funded[0].Outputs != 203 {
		t.Fatal("replacement required retired bootstrap target or lost original records", state.Status)
	}
	if f.readRecord(t).UID != f.record.UID {
		t.Fatal("retired target execution identity was replaced")
	}
}

func TestSelectedTargetRechecksExecutionRatherThanCachedSummary(t *testing.T) {
	f := baselineFixture(t)
	s := f.schedulerFor()
	initial := f.readInitial(t)
	armBaseline(t, f, s)
	initial = f.readInitial(t)
	executions := s.accountBaselineReceipts(t.Context(), f.root, initial)
	pin := binding("StacksNetworkParticipant", f.node)
	if _, reason := s.baselineTarget(t.Context(), f.root, initial, &pin, executions); reason != "" {
		t.Fatal(reason)
	}
	current := executions[pin.UID].DeepCopy()
	current.Status.Armed = &bitcoin.BitcoinArmedRPC{}
	if err := f.c.Status().Update(t.Context(), current); err != nil {
		t.Fatal(err)
	}
	if executions[pin.UID].Status.Armed != nil {
		t.Fatal("cached fixture unexpectedly updated")
	}
	if _, reason := s.baselineTarget(t.Context(), f.root, initial, &pin, executions); reason != "SelectedTargetBusy" {
		t.Fatalf("stale cached availability authorized selection: %s", reason)
	}
}
