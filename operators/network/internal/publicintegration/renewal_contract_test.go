//go:build live

package publicintegration

import (
	"context"
	"fmt"
	"testing"
	"time"

	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/foundation"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// unsettledCohortReader exposes a temporarily unavailable stacker before settled observations.
type unsettledCohortReader struct {
	client.Client
	root         *api.StacksNetwork
	participants []api.StacksNetworkParticipant
	recover      bool
	lists        int
}

func (r *unsettledCohortReader) Get(
	_ context.Context,
	_ client.ObjectKey,
	out client.Object,
	_ ...client.GetOption,
) error {
	root, ok := out.(*api.StacksNetwork)
	if !ok {
		return fmt.Errorf("unexpected root read")
	}
	*root = *r.root.DeepCopy()
	return nil
}

func (r *unsettledCohortReader) List(_ context.Context, out client.ObjectList, _ ...client.ListOption) error {
	switch list := out.(type) {
	case *api.StacksNetworkParticipantList:
		for i := range r.participants {
			list.Items = append(list.Items, *r.participants[i].DeepCopy())
		}
		r.lists++
		if r.lists == 1 || !r.recover {
			list.Items[0].Status.Execution.Phase = "Blocked"
			list.Items[0].Status.Execution.PoX5 = nil
		}
	case *bitcoin.BitcoinExecutionList:
	default:
		return fmt.Errorf("unexpected collection read")
	}
	return nil
}

func TestRenewalBaselineWaitsForFreshSettledCohort(t *testing.T) {
	for _, recover := range []bool{false, true} {
		t.Run(fmt.Sprint(recover), func(t *testing.T) {
			root := waitRoot()
			root.Status = renewalFixture().Status
			root.Status.Identities = nil
			root.Status.Conditions = []metav1.Condition{
				{Type: "Operational", Status: metav1.ConditionTrue, ObservedGeneration: 1},
			}
			reader := &unsettledCohortReader{root: root, recover: recover}
			for i := range 2 {
				name := fmt.Sprintf("stacker-%d", i)
				uid := types.UID(name)
				status := renewalFixture().Participants[0].Status.DeepCopy()
				status.Execution.NetworkGeneration = 1
				status.Execution.ObservedAt = metav1.NewTime(time.Now().Add(-time.Second))
				status.Execution.PoX5.ObservedAt = status.Execution.ObservedAt
				p := &api.StacksNetworkParticipant{
					ObjectMeta: metav1.ObjectMeta{
						Name:       name,
						Namespace:  "test",
						UID:        uid,
						Generation: 1,
						OwnerReferences: []metav1.OwnerReference{
							{
								APIVersion: api.GroupVersion.String(),
								Kind:       "StacksNetwork",
								Name:       "network",
								UID:        root.UID,
								Controller: ptr.To(true),
							},
						},
					},
					Spec: api.StacksNetworkParticipantSpec{
						NetworkUID:      root.UID,
						ParticipantName: name,
						Kind:            "StacksStacker",
					},
					Status: *status,
				}
				reader.participants = append(reader.participants, *p)
				id := renewalFixture().Status.Identities[0]
				id.UID, id.Name = uid, name
				root.Status.Identities = append(root.Status.Identities, id)
			}
			h := &harness{
				c:        reader,
				rootUID:  root.UID,
				config:   liveConfig{fixtureOptions: fixtureOptions{namespace: "test"}},
				evidence: t.TempDir(),
			}
			h.config.progressTimeout = 100 * time.Millisecond
			if recover {
				h.config.progressTimeout = 3 * time.Second
			}
			settled, err := h.awaitRenewalBaseline(context.Background())
			if (err == nil) != recover {
				t.Fatalf("recover=%v: %v", recover, err)
			}
			if recover {
				cohort, ready := renewalCohort(settled)
				if reader.lists < 2 || !ready || len(cohort) != 2 || renewalAdvanced(cohort, cohort) {
					t.Fatal("settlement bypassed the wait or counted as a new renewal")
				}
			}
		})
	}
}

// renewalFixture represents public native facts independently of their acceptance predicate.
func renewalFixture() snapshot {
	now := time.Unix(1000, 0)
	policy := foundation.ObservationPolicy()
	return snapshot{
		At:   now,
		Root: identity{UID: "root", Generation: 2},
		Status: api.StacksNetworkStatus{
			ObservationPolicy: &policy,
			Identities: []api.InstanceIdentity{
				{
					Name:   "holder",
					UID:    "participant",
					Worker: &api.WorkerSession{Pod: api.WorkerPodBinding{UID: "pod"}, ProfileDigest: "profile"},
				},
			},
		},
		Participants: []participantEvidence{
			{
				Identity: identity{UID: "participant", Generation: 1},
				Kind:     "StacksStacker",
				Status: api.ParticipantStatus{
					Admission: &api.Admission{PolicyDigest: "policy"},
					Execution: &api.WorkerExecutionStatus{
						Phase:               "Active",
						ProcessNonce:        "process",
						PodUID:              "pod",
						ProfileDigest:       "profile",
						AppliedPolicyDigest: "policy",
						ObservedGeneration:  1,
						NetworkGeneration:   2,
						ObservedAt:          metav1.NewTime(now),
						Transactions:        &api.TransactionExecutionStatus{Offered: 3, PostconditionObserved: 3},
						PoX5: &api.PoX5EnrollmentObservation{
							Holder:                  "holder",
							Manager:                 "manager",
							ManagerSourceDigest:     "source",
							SignerPublicKey:         "key",
							AmountMicroSTX:          "100",
							DelegatedAmountMicroSTX: "100",
							FirstCycle:              15,
							EndCycleExclusive:       25,
							TargetCycle:             15,
							TargetCycleMatched:      true,
							UnlockHeight:            500,
							BurnHeight:              300,
							StacksTip:               "tip",
							ObservedAt:              metav1.NewTime(now),
						},
					},
				},
			},
		},
	}
}

func TestRenewalQualificationRejectsStaleAndUnboundState(t *testing.T) {
	if _, ok := renewalCohort(renewalFixture()); !ok {
		t.Fatal("valid native cohort unavailable")
	}
	for name, change := range map[string]func(*snapshot){
		"stale observation": func(s *snapshot) {
			s.Participants[0].Status.Execution.PoX5.ObservedAt = metav1.NewTime(s.At.Add(-17 * time.Second))
		},
		"future observation": func(s *snapshot) {
			s.Participants[0].Status.Execution.PoX5.ObservedAt = metav1.NewTime(s.At.Add(time.Second))
		},
		"stale execution": func(s *snapshot) {
			s.Participants[0].Status.Execution.ObservedAt = metav1.NewTime(s.At.Add(-17 * time.Second))
		},
		"replacement Pod":    func(s *snapshot) { s.Participants[0].Status.Execution.PodUID = "replacement" },
		"stale policy":       func(s *snapshot) { s.Participants[0].Status.Execution.AppliedPolicyDigest = "old" },
		"stale controls":     func(s *snapshot) { s.Participants[0].Status.Execution.NetworkGeneration-- },
		"pending submission": func(s *snapshot) { s.Participants[0].Status.Execution.Pending = 1 },
		"not current member": func(s *snapshot) {
			s.Participants[0].Status.Execution.PoX5.TargetCycleMatched = false
		},
		"unlocked": func(s *snapshot) { s.Participants[0].Status.Execution.PoX5.UnlockHeight = 300 },
		"wrong delegated amount": func(s *snapshot) {
			s.Participants[0].Status.Execution.PoX5.DelegatedAmountMicroSTX = "99"
		},
		"empty cohort": func(s *snapshot) { s.Participants = nil },
	} {
		t.Run(name, func(t *testing.T) {
			s := renewalFixture()
			change(&s)
			if _, ok := renewalCohort(s); ok {
				t.Fatal("invalid renewal evidence accepted")
			}
		})
	}
}

func TestRenewalQualificationRequiresContinuousExtensionForEveryHolder(t *testing.T) {
	original, _ := renewalCohort(renewalFixture())
	old := original["participant"]
	next := old
	next.Observation.EndCycleExclusive = 30
	next.Observation.UnlockHeight = 600
	next.Observation.BurnHeight = 400
	next.Observation.StacksTip = "later-tip"
	next.Observation.ObservedAt = metav1.NewTime(time.Unix(1100, 0))
	next.Transactions.Offered++
	next.Transactions.PostconditionObserved++
	good := func() map[types.UID]renewalEvidence { return map[types.UID]renewalEvidence{"participant": next} }
	if !renewalAdvanced(original, good()) {
		t.Fatal("continuous native extension not recognized")
	}
	included := next
	included.Transactions.PostconditionObserved = old.Transactions.PostconditionObserved
	included.Transactions.Included++
	if !renewalAdvanced(original, map[types.UID]renewalEvidence{"participant": included}) {
		t.Fatal("native extension with exact inclusion not recognized")
	}
	for name, change := range map[string]func(*renewalEvidence){
		"relock": func(e *renewalEvidence) { e.Observation.FirstCycle++ },
		"same horizon": func(e *renewalEvidence) {
			e.Observation.EndCycleExclusive = old.Observation.EndCycleExclusive
		},
		"unchanged unlock": func(e *renewalEvidence) { e.Observation.UnlockHeight = old.Observation.UnlockHeight },
		"different holder": func(e *renewalEvidence) { e.Observation.Holder = "other" },
		"different source": func(e *renewalEvidence) { e.Observation.ManagerSourceDigest = "other" },
		"different signer": func(e *renewalEvidence) { e.Observation.SignerPublicKey = "other" },
		"process restart":  func(e *renewalEvidence) { e.Worker.ProcessNonce = "restarted" },
		"no new offer":     func(e *renewalEvidence) { e.Transactions.Offered = old.Transactions.Offered },
		"unsettled offer": func(e *renewalEvidence) {
			e.Transactions.PostconditionObserved = old.Transactions.PostconditionObserved
		},
	} {
		t.Run(name, func(t *testing.T) {
			e := next
			change(&e)
			if renewalAdvanced(original, map[types.UID]renewalEvidence{"participant": e}) {
				t.Fatal("invalid extension accepted")
			}
		})
	}
	before := map[types.UID]renewalEvidence{"participant": old, "second": old}
	after := good()
	after["second"] = old
	if renewalAdvanced(before, after) {
		t.Fatal("one holder's extension satisfied entire cohort")
	}
}
