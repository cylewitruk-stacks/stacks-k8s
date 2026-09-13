package stacksworker

import (
	"context"
	"testing"
	"time"

	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// contractReportRole isolates public contract observation transport from native role behavior.
type contractReportRole struct {
	testRole
	observation *api.ContractSetObservation
}

func (r *contractReportRole) Step(ctx context.Context, s Snapshot) (RoleResult, error) {
	result, err := r.testRole.Step(ctx, s)
	result.Contracts = r.observation
	return result, err
}

func TestContractObservationPublicationRetainsOriginalAndClearsStaleEvidence(t *testing.T) {
	ctx := context.Background()
	root, p, pod, profile := fixture(t)
	ProjectSession(root, p, pod, nil, time.Now())
	c := &statusClient{Client: fakeClient(t, root, p, pod)}
	role := &contractReportRole{
		observation: &api.ContractSetObservation{
			SignerPublicKeys: []string{"original"},
			Complete:         true,
			ObservedAt:       metav1.NewTime(time.Unix(100, 0)),
		},
	}
	r := Runtime{
		Client:          c,
		Namespace:       p.Namespace,
		ParticipantName: p.Name,
		NetworkUID:      root.UID,
		ParticipantUID:  p.UID,
		PodUID:          pod.UID,
		PodName:         pod.Name,
		Profile:         profile,
		Role:            role,
		Prerequisites:   func(context.Context, Snapshot) error { return nil },
		nonce:           "process",
	}
	if _, err := r.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	c.lose = true
	c.commit = true
	if _, err := r.Reconcile(ctx); err == nil || r.pendingReport == nil {
		t.Fatal("lost contract observation publication not retained")
	}
	role.observation.SignerPublicKeys[0] = "changed"
	role.observation.ObservedAt = metav1.NewTime(time.Unix(200, 0))
	if _, err := r.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	var actual api.StacksNetworkParticipant
	if err := c.Get(ctx, client.ObjectKeyFromObject(p), &actual); err != nil {
		t.Fatal(err)
	}
	if role.steps != 2 || actual.Status.Execution.Contracts.SignerPublicKeys[0] != "original" ||
		actual.Status.Execution.Contracts.ObservedAt.Unix() != 100 {
		t.Fatal("publication retry changed native contract evidence")
	}
	role.observation = nil
	if _, err := r.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	if err := c.Get(ctx, client.ObjectKeyFromObject(p), &actual); err != nil {
		t.Fatal(err)
	}
	if actual.Status.Execution.Contracts != nil {
		t.Fatal("failed fresh observation retained stale contract gate evidence")
	}
}
