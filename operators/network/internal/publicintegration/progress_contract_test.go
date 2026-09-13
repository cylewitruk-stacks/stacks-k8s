//go:build live

package publicintegration

import (
	"context"
	"errors"
	"testing"
	"time"

	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// progressReader supplies coherent public observations after an unavailable handoff.
type progressReader struct {
	client.Client
	current snapshot
	reads   int
	advance bool
}

func (r *progressReader) Get(_ context.Context, _ client.ObjectKey, out client.Object, _ ...client.GetOption) error {
	root, ok := out.(*api.StacksNetwork)
	if !ok {
		return errors.New("unexpected observation read")
	}
	r.reads++
	if r.reads > 1 && r.advance {
		for i := range r.current.Participants {
			p := &r.current.Participants[i]
			if p.Kind == "StacksNode" {
				p.Status.Runtime.Protocol.StacksHeight++
			}
			if p.Kind == "StacksTransactionProduction" {
				p.Status.Execution.Transactions.Included++
			}
		}
		r.current.Executions[0].Status.BlocksGenerated++
		r.current.Executions[0].Status.Observation.Height++
	}
	*root = api.StacksNetwork{
		ObjectMeta: metav1.ObjectMeta{Name: "network", Namespace: "lab", UID: "root", Generation: 1},
		Spec:       api.StacksNetworkSpec{Operation: "Running"},
		Status:     *r.current.Status.DeepCopy(),
	}
	return nil
}

func (r *progressReader) List(_ context.Context, out client.ObjectList, _ ...client.ListOption) error {
	owner := []metav1.OwnerReference{
		{
			APIVersion: api.GroupVersion.String(),
			Kind:       "StacksNetwork",
			Name:       "network",
			UID:        "root",
			Controller: ptr.To(true),
		},
	}
	switch list := out.(type) {
	case *api.StacksNetworkParticipantList:
		for _, p := range r.current.Participants {
			list.Items = append(
				list.Items,
				api.StacksNetworkParticipant{
					ObjectMeta: metav1.ObjectMeta{
						Name:            p.Identity.Name,
						UID:             p.Identity.UID,
						Namespace:       "lab",
						OwnerReferences: owner,
					},
					Spec: api.StacksNetworkParticipantSpec{
						NetworkUID:      "root",
						ParticipantName: p.Name,
						Kind:            p.Kind,
					},
					Status: *p.Status.DeepCopy(),
				},
			)
		}
	case *bitcoin.BitcoinExecutionList:
		for _, e := range r.current.Executions {
			list.Items = append(
				list.Items,
				bitcoin.BitcoinExecution{
					ObjectMeta: metav1.ObjectMeta{
						Name:            e.Identity.Name,
						UID:             e.Identity.UID,
						Namespace:       "lab",
						OwnerReferences: owner,
					},
					Spec:   bitcoin.BitcoinExecutionSpec{NetworkUID: "root"},
					Status: *e.Status.DeepCopy(),
				},
			)
			list.Items[len(list.Items)-1].Spec.Participant.UID = e.ParticipantUID
		}
	default:
		return errors.New("unexpected observation list")
	}
	return nil
}

func TestProgressHandoffWaitsForCoherenceThenRequiresNewEffects(t *testing.T) {
	for _, advance := range []bool{false, true} {
		t.Run(map[bool]string{false: "no-new-effects", true: "new-effects"}[advance], func(t *testing.T) {
			s, _, _ := actionContractFixture()
			actor := actorSnapshot()
			actor.Participants[0].Identity.UID = "stacks-participant"
			s.Status.GenesisRef = actor.Status.GenesisRef
			s.Status.ObservationPolicy = actor.Status.ObservationPolicy
			s.Participants = append(s.Participants, actor.Participants...)
			s.Participants = append(
				s.Participants,
				participantEvidence{
					Name: "traffic",
					Kind: "StacksTransactionProduction",
					Status: api.ParticipantStatus{
						Execution: &api.WorkerExecutionStatus{
							Transactions: &api.TransactionExecutionStatus{
								Included:      10,
								LastInclusion: &api.TransactionInclusion{Success: true},
							},
						},
					},
				},
			)
			s.Executions[0].Status.BlocksGenerated = 10
			s.Status.Conditions = []metav1.Condition{
				{Type: "Initialized", Status: metav1.ConditionTrue, ObservedGeneration: 1},
				{Type: "Operational", Status: metav1.ConditionTrue, ObservedGeneration: 1},
			}
			s.At = time.Now().UTC()
			if _, ok := progress(s); !ok {
				t.Fatal("invalid coherent fixture")
			}
			reader := &progressReader{current: s, advance: advance}
			h := &harness{
				t:        t,
				c:        reader,
				config:   liveConfig{namespace: "lab", progressTimeout: 100 * time.Millisecond},
				rootUID:  "root",
				evidence: t.TempDir(),
			}
			// A handoff without native evidence must acquire its baseline, never count
			// the older handoff's missing/lower counters as newly observed effects.
			_, err := h.awaitProgress(context.Background(), "handoff", snapshot{})
			if (err == nil) != advance {
				t.Fatalf("advance=%v: %v", advance, err)
			}
			if reader.reads < 2 {
				t.Fatal("coherence alone was accepted as progress")
			}
		})
	}
}
