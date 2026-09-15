package networkruntime

import (
	"testing"
	"time"

	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// TestBurnchainFactsNeverGateOperational covers bursts and unavailable diagnostic sources.
func TestBurnchainFactsNeverGateOperational(t *testing.T) {
	for _, mode := range []string{"burst", "missing", "stale", "replaced", "paused-miner"} {
		t.Run(mode, func(t *testing.T) {
			now := time.Now().Truncate(time.Second)
			root, g, ps, execution := operationalFixture(t, now)
			set(root, "Initialized", metav1.ConditionTrue, "BootstrapCompleted", "complete")
			execution.Status.Observation.Height = 1000
			execution.Status.Observation.ObservedAt = metav1.NewTime(now.Add(-time.Second))
			switch mode {
			case "stale":
				execution.Status.Observation.ObservedAt = metav1.NewTime(now.Add(-time.Minute))
			case "replaced":
				execution.Status.Observation.Target.ContainerID = "new-process"
			case "paused-miner":
				root.Spec.Participants[0].Control = &api.Control{Suspended: ptr.To(true)}
			}
			initial := &bitcoin.BitcoinInitialization{
				ObjectMeta: metav1.ObjectMeta{
					Name:            "initial",
					Namespace:       root.Namespace,
					UID:             "initial",
					OwnerReferences: g.OwnerReferences,
				},
				Spec: bitcoin.BitcoinInitializationSpec{NetworkUID: root.UID},
			}
			root.Status.Bitcoin.InitializationRef = &common.Binding{
				Kind: "BitcoinInitialization",
				Name: initial.Name,
				UID:  initial.UID,
			}
			objects := []client.Object{g, initial}
			if mode != "missing" {
				objects = append(objects, execution)
			}
			r := fixtureReconciler(t, objects...)
			if err := r.projectOperation(t.Context(), root, ps, now); err != nil {
				t.Fatal(err)
			}
			facts := root.Status.BurnchainObservations
			if facts == nil {
				t.Fatal("diagnostic missing")
			}
			if mode == "paused-miner" {
				if facts.Miner != nil || !meta.IsStatusConditionFalse(root.Status.Conditions, "Operational") {
					t.Fatal("suspended miner used")
				}
				return
			}
			if !meta.IsStatusConditionTrue(root.Status.Conditions, "Operational") {
				t.Fatalf("diagnostic affected health: %+v", root.Status.Conditions)
			}
			if facts.Miner == nil || facts.Miner.Participant.UID != ps[0].UID ||
				facts.Miner.Pod.UID != ps[0].Status.Runtime.PodRef.UID ||
				facts.Miner.ContainerID != ps[0].Status.Runtime.ContainerID {
				t.Fatal("miner attribution lost")
			}
			if mode != "burst" {
				if facts.Bitcoin != nil {
					t.Fatal("invalid bitcoin sample published")
				}
				return
			}
			if facts.Bitcoin == nil || facts.Bitcoin.Height != 1000 ||
				facts.Bitcoin.Pod.UID != execution.Status.Observation.Target.Pod.UID ||
				!facts.Bitcoin.FirstObservedAt.Equal(&execution.Status.Observation.ObservedAt) {
				t.Fatal("original source identity/time lost")
			}
		})
	}
}

// TestBurnchainMaximumHasStableProvenance avoids list-order-dependent identity changes.
func TestBurnchainMaximumHasStableProvenance(t *testing.T) {
	a, b := &api.BurnHeightSample{Height: 100}, &api.BurnHeightSample{Height: 100}
	a.Participant.UID, b.Participant.UID = "a", "b"
	if greatestSample(a, b) != a || greatestSample(b, a) != a {
		t.Fatal("unstable tie")
	}
	b.Height++
	if greatestSample(a, b) != b {
		t.Fatal("not greatest")
	}
}
