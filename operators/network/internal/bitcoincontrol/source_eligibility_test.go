package bitcoincontrol

import (
	"fmt"
	"strings"
	"testing"
	"time"

	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/foundation"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// pinBitcoinSource attaches retained public provenance to an already admitted test participant.
func pinBitcoinSource(t *testing.T, f *testFixture, p *api.StacksNetworkParticipant) client.Object {
	t.Helper()
	source := foundation.DefinitionObject(p.Spec.Kind)
	source.SetName("source-" + p.Name)
	source.SetNamespace(p.Namespace)
	source.SetUID(types.UID("source-" + string(p.UID)))
	source.SetGeneration(1)
	if err := f.c.Create(t.Context(), source); err != nil {
		t.Fatal(err)
	}
	if err := f.c.Get(t.Context(), client.ObjectKeyFromObject(p), p); err != nil {
		t.Fatal(err)
	}
	p.Spec.Source = api.Source{Name: source.GetName(), UID: source.GetUID(), Generation: 1, Digest: "original"}
	if err := f.c.Update(t.Context(), p); err != nil {
		t.Fatal(err)
	}
	p.Status.Admission.Source = p.Spec.Source
	if err := f.c.Status().Update(t.Context(), p); err != nil {
		t.Fatal(err)
	}
	return source
}

func TestBitcoinSourceWithdrawalBlocksSendsAndKeepsNativeObservations(t *testing.T) {
	for _, kind := range []string{"actor", "production"} {
		for _, mode := range []string{"missing", "replaced", "deleting", "withdrawn", "newer-rejected"} {
			t.Run(kind+"/"+mode, func(t *testing.T) {
				f := newFixture(t)
				participant := f.node
				if kind == "production" {
					participant = f.production
				}
				source := pinBitcoinSource(t, f, participant)
				switch mode {
				case "withdrawn":
					meta.SetStatusCondition(
						&participant.Status.Conditions,
						metav1.Condition{
							Type:               "AdmissionReady",
							Status:             metav1.ConditionFalse,
							ObservedGeneration: participant.Generation,
							Reason:             "IdentityUnavailable",
							Message:            "Retained dependency lost",
							LastTransitionTime: metav1.NewTime(f.now),
						},
					)
					if err := f.c.Status().Update(t.Context(), participant); err != nil {
						t.Fatal(err)
					}
				case "newer-rejected":
					source.SetGeneration(2)
					if err := f.c.Update(t.Context(), source); err != nil {
						t.Fatal(err)
					}
				default:
					if mode == "deleting" {
						source.SetFinalizers([]string{"test"})
						if err := f.c.Update(t.Context(), source); err != nil {
							t.Fatal(err)
						}
					}
					if err := f.c.Delete(t.Context(), source); err != nil {
						t.Fatal(err)
					}
					if mode == "replaced" {
						source.SetUID("replacement")
						source.SetResourceVersion("")
						if err := f.c.Create(t.Context(), source); err != nil {
							t.Fatal(err)
						}
					}
				}
				f.offer(t, 200)
				_ = f.worker.Step(t.Context())
				f.worker.workers.Wait()
				record := f.readRecord(t)
				want := 0
				if mode == "newer-rejected" {
					want = 1
				}
				if f.rpc.count() != want || record.Status.Armed != nil || record.Status.Observation == nil ||
					record.Status.Observation.Height != 200 {
					t.Fatalf("source mode=%s sends=%d record=%+v", mode, f.rpc.count(), record.Status)
				}
			})
		}
	}
}

func TestBitcoinSourceLossKeepsBaselineReceiptAccounting(t *testing.T) {
	f := baselineFixture(t)
	source := pinBitcoinSource(t, f, f.production)
	scheduler := f.schedulerFor()
	armBaseline(t, f, scheduler)
	f.now = f.now.Add(time.Second)
	for range 3 {
		f.reconcile(t, scheduler)
	}
	record := f.readRecord(t)
	offer := record.Spec.Offer.DeepCopy()
	if offer == nil {
		t.Fatal("offer missing")
	}
	received := metav1.NewTime(f.now)
	record.Status.CompletedOffer = offer.Number
	record.Status.LastReceipt = &bitcoin.BitcoinRPCReceipt{
		Request:    bitcoin.BitcoinArmedRPC{Method: "Generate", Offer: offer, Target: record.Status.Observation.Target},
		BlockHash:  strings.Repeat("b", 64),
		ReceivedAt: received,
	}
	if err := f.c.Status().Update(t.Context(), record); err != nil {
		t.Fatal(err)
	}
	if err := f.c.Delete(t.Context(), source); err != nil {
		t.Fatal(err)
	}
	f.reconcile(t, scheduler)
	f.reconcile(t, scheduler)
	state := f.readInitial(t).Status
	if state.Baseline.Scheduling.Acknowledged != 1 || !state.Baseline.Scheduling.LastAcknowledgedAt.Equal(&received) ||
		!generationAccounted(f.readInitial(t), record) ||
		state.Offer != nil {
		t.Fatal("source withdrawal lost accounting or kept send offer", state)
	}
}

func TestBitcoinSourceLossKeepsFiniteCompensation(t *testing.T) {
	f, _, rpc := reorganizationFixture(t)
	source := pinBitcoinSource(t, f, f.node)
	if err := f.worker.Step(t.Context()); err != nil {
		t.Fatal(err)
	}
	f.worker.workers.Wait()
	if !f.readRecord(t).Status.Action.InvalidationAcknowledged {
		t.Fatal("invalidation was not acknowledged")
	}
	if err := f.c.Delete(t.Context(), source); err != nil {
		t.Fatal(err)
	}
	_ = f.worker.Step(t.Context())
	f.worker.workers.Wait()
	if f.readRecord(t).Status.Action.BlocksGenerated != 0 {
		t.Fatal("source withdrawal permitted replacement generation")
	}
	// The finite deadline permits only the previously captured marker's compensation.
	f.now = f.readRecord(t).Status.Action.ExpiresAt.Add(time.Second)
	for range 4 {
		if err := f.worker.Step(t.Context()); err != nil {
			t.Fatal(err)
		}
		f.worker.workers.Wait()
	}
	state := f.readRecord(t).Status.Action
	if !state.CleanupAcknowledged || state.BlocksGenerated != 0 ||
		fmt.Sprint(rpc.sequence) != "[invalidateblock reconsiderblock]" {
		t.Fatalf("source loss prevented bounded compensation: %+v sequence=%v", state, rpc.sequence)
	}
}

func TestBitcoinActorSourceLossHoldsWalletMutation(t *testing.T) {
	f := newFixture(t)
	source := pinBitcoinSource(t, f, f.node)
	if err := f.c.Delete(t.Context(), source); err != nil {
		t.Fatal(err)
	}
	f.rpc.exists, f.rpc.loaded, f.rpc.imported = false, false, false
	_ = f.worker.Step(t.Context())
	f.worker.workers.Wait()
	record := f.readRecord(t)
	if f.rpc.count() != 0 || record.Status.Armed != nil || record.Status.Observation == nil {
		t.Fatal("source loss sent wallet mutation or hid native reads", record.Status)
	}
}

func TestBitcoinSourceLossKeepsBootstrapReceiptAccounting(t *testing.T) {
	f := newFixture(t)
	source := pinBitcoinSource(t, f, f.production)
	f.offer(t, 200)
	if err := f.worker.Step(t.Context()); err != nil {
		t.Fatal(err)
	}
	f.worker.workers.Wait()
	if f.rpc.count() != 1 {
		t.Fatal("initial block was not generated")
	}
	if err := f.c.Delete(t.Context(), source); err != nil {
		t.Fatal(err)
	}
	if err := f.worker.Step(t.Context()); err != nil {
		t.Fatal(err)
	}
	f.reconcile(t, f.schedulerFor())
	state := f.readInitial(t).Status
	if state.LastAccountedOffer != 1 || state.Offer != nil || state.Reason != "ProductionSourceUnavailable" ||
		len(state.Funded) != 1 ||
		state.Funded[0].Outputs != 1 {
		t.Fatal("bootstrap receipt was lost or next offer survived source withdrawal", state)
	}
}

func TestBitcoinReadScopeUsesRetainedDefinitionProvenance(t *testing.T) {
	f := baselineFixture(t)
	for _, participant := range []*api.StacksNetworkParticipant{f.node, f.production} {
		_ = pinBitcoinSource(t, f, participant)
		participant.Spec.Source = api.Source{Name: "candidate-only", UID: "candidate", Generation: 2}
		if err := f.c.Update(t.Context(), participant); err != nil {
			t.Fatal(err)
		}
	}
	refs, err := publicReadScope(t.Context(), f.c, f.node, f.initial)
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]bool{}
	for _, ref := range refs {
		if ref.Name == "candidate-only" || ref.Kind == "Secret" {
			t.Fatal("unexpected worker source grant", ref)
		}
		if ref.Kind == "BitcoinNode" || ref.Kind == "BitcoinBlockProduction" {
			found[ref.Name] = true
		}
	}
	if !found["source-node"] || !found["source-production"] {
		t.Fatal("retained definition reads absent", refs)
	}
}
