package stacksworker

import (
	"context"
	"errors"
	"testing"
	"time"

	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// stopFixture retains one Stacks session and an actor under the same exact root.
func stopFixture(t *testing.T) (*api.StacksNetwork, *api.StacksNetworkParticipant) {
	t.Helper()
	root, worker, pod, _ := fixture(t)
	ProjectSession(root, worker, pod, nil, time.Now())
	actor := worker.DeepCopy()
	actor.Name = "actor"
	actor.UID = "actor-uid"
	actor.Spec.ParticipantName = "actor"
	actor.Spec.Kind = "StacksNode"
	root.Spec.Participants = append(root.Spec.Participants, api.Participant{Name: "actor", Kind: "StacksNode"})
	root.Status.Identities = append(root.Status.Identities, api.InstanceIdentity{Name: "actor", UID: actor.UID})
	root.Spec.Operation = "Stopped"
	return root, actor
}

func TestActorStopRequiresEveryRetainedDisposition(t *testing.T) {
	for _, kind := range []api.ParticipantKind{"StacksNode", "StacksSigner"} {
		for _, outcome := range []string{"Settled", "Unsettled"} {
			t.Run(string(kind)+outcome, func(t *testing.T) {
				root, actor := stopFixture(t)
				actor.Spec.Kind = kind
				c := fakeClient(t, root)
				ctx := context.Background()
				if ready, err := CheckActorStop(ctx, c, actor); ready || err != nil {
					t.Fatalf("undispositioned session bypassed %v %v", ready, err)
				}
				now := metav1.Now()
				root.Status.Identities[0].Worker.Shutdown = &api.WorkerShutdown{NetworkGeneration: root.Generation, Reason: "NetworkStopped", RequestedAt: now}
				root.Status.Identities[0].Worker.Disposal = &api.WorkerDisposal{Outcome: api.WorkerDisposalOutcome(outcome), ObservedAt: now, Terminated: false}
				if err := c.Status().Update(ctx, root); err != nil {
					t.Fatal(err)
				}
				if ready, err := CheckActorStop(ctx, c, actor); !ready || err != nil {
					t.Fatalf("bounded disposition waited for process termination %v %v", ready, err)
				}
				second := root.Status.Identities[0]
				second.Name = "second"
				second.UID = "second-uid"
				second.Worker = second.Worker.DeepCopy()
				second.Worker.Disposal = nil
				root.Status.Identities = append(root.Status.Identities, second)
				if err := c.Status().Update(ctx, root); err != nil {
					t.Fatal(err)
				}
				if ready, err := CheckActorStop(ctx, c, actor); ready || err != nil {
					t.Fatalf("second retained session ignored %v %v", ready, err)
				}
			})
		}
	}
}

func TestActorStopNeverBoundAndIndividualControls(t *testing.T) {
	root, actor := stopFixture(t)
	root.Status.Identities[0].Worker = nil
	if ready, err := CheckActorStop(context.Background(), fakeClient(t, root), actor); !ready || err != nil {
		t.Fatalf("never-bound worker delayed stop %v %v", ready, err)
	}
	for _, operation := range []string{"Running", "Paused"} {
		root, actor := stopFixture(t)
		root.Spec.Operation = api.NetworkOperation(operation)
		root.Status.Identities[1].Removing = true
		actor.DeletionTimestamp = &metav1.Time{Time: time.Now()}
		if ready, err := CheckActorStop(context.Background(), fakeClient(t, root), actor); !ready || err != nil {
			t.Fatalf("individual removal acquired root drain dependency %v %v", ready, err)
		}
	}
}

func TestActorDeleteAndIdentityUncertaintyStayClosed(t *testing.T) {
	for _, change := range []struct {
		name  string
		apply func(*api.StacksNetwork, *api.StacksNetworkParticipant)
	}{
		{"root deleting", func(r *api.StacksNetwork, p *api.StacksNetworkParticipant) {
			r.Spec.Operation = "Running"
			r.Finalizers = []string{"test"}
			r.DeletionTimestamp = &metav1.Time{Time: time.Now()}
		}},
		{"root replaced", func(r *api.StacksNetwork, p *api.StacksNetworkParticipant) { r.UID = "replacement" }},
		{"actor replaced", func(r *api.StacksNetwork, p *api.StacksNetworkParticipant) { p.UID = "replacement" }},
		{"worker identity missing", func(r *api.StacksNetwork, p *api.StacksNetworkParticipant) {
			r.Status.Identities[0].Worker.Pod.UID = ""
		}},
		{"invalid disposition", func(r *api.StacksNetwork, p *api.StacksNetworkParticipant) {
			r.Status.Identities[0].Worker.Disposal = &api.WorkerDisposal{Outcome: "Unknown", ObservedAt: metav1.Now()}
		}},
	} {
		t.Run(change.name, func(t *testing.T) {
			root, actor := stopFixture(t)
			change.apply(root, actor)
			if ready, _ := CheckActorStop(context.Background(), fakeClient(t, root), actor); ready {
				t.Fatal("uncertain actor stop permitted")
			}
		})
	}
	root, actor := stopFixture(t)
	if ready, err := CheckActorStop(context.Background(), unavailableRoot{Reader: fakeClient(t, root)}, actor); ready || err == nil {
		t.Fatal("API uncertainty skipped")
	}
	if ready, err := CheckActorStop(context.Background(), fakeClient(t), actor); ready || err == nil {
		t.Fatal("missing root skipped")
	}
}

// unavailableRoot preserves the distinction between API errors and disposition evidence.
type unavailableRoot struct{ client.Reader }

func (r unavailableRoot) Get(context.Context, client.ObjectKey, client.Object, ...client.GetOption) error {
	return errors.New("API unavailable")
}

func TestMissingBoundWorkerCannotFabricateActorDrain(t *testing.T) {
	root, p, pod, _ := fixture(t)
	now := time.Now()
	ProjectSession(root, p, pod, nil, now)
	root.Spec.Operation = "Stopped"
	ProjectSession(root, p, pod, nil, now)
	fact := ProjectSession(root, p, nil, apierrors.NewNotFound(schema.GroupResource{Resource: "pods"}, pod.Name), now.Add(2*ShutdownBound))
	if !fact.Unknown || root.Status.Identities[0].Worker.Disposal != nil {
		t.Fatal("unavailable Pod became bounded disposition evidence")
	}
	actor := p.DeepCopy()
	actor.Name = "actor"
	actor.UID = "actor-uid"
	actor.Spec.ParticipantName = "actor"
	actor.Spec.Kind = "StacksNode"
	root.Status.Identities = append(root.Status.Identities, api.InstanceIdentity{Name: "actor", UID: actor.UID})
	if ready, _ := CheckActorStop(context.Background(), fakeClient(t, root), actor); ready {
		t.Fatal("unknown worker disappearance allowed actor stop")
	}
}
