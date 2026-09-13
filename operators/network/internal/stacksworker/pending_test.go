package stacksworker

import (
	"context"
	"testing"
	"time"

	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// pendingRole models one accepted operation whose observation is independent of API writes.
type pendingRole struct {
	steps, sends, polls int
	available, included bool
	observed            metav1.Time
}

func (r *pendingRole) ObservePending(context.Context) error {
	if r.sends != 0 && !r.included {
		r.polls++
		if r.available {
			r.included = true
			r.observed = metav1.NewTime(time.Unix(100, 0))
		}
	}
	return nil
}

func (r *pendingRole) Step(ctx context.Context, s Snapshot) (RoleResult, error) {
	r.steps++
	if r.sends == 0 {
		if err := s.Authorize(ctx); err != nil {
			return RoleResult{}, err
		}
		r.sends++
	}
	_ = r.ObservePending(ctx)
	facts := &api.TransactionExecutionStatus{Offered: 1, Accepted: 1}
	if r.included {
		facts.Included = 1
		facts.LastInclusion = &api.TransactionInclusion{TxID: "original", ObservedAt: r.observed}
	}
	return RoleResult{Reason: "Pending", Pending: 1, Transactions: facts, RequeueAfter: time.Second}, nil
}

func (r *pendingRole) Drain(context.Context, Snapshot) (DrainResult, error) {
	return DrainResult{}, nil
}

func TestPendingObservationContinuesDuringReadAndPublicationOutages(t *testing.T) {
	for _, publication := range []bool{false, true} {
		t.Run(map[bool]string{false: "read", true: "publication"}[publication], func(t *testing.T) {
			root, p, pod, profile := fixture(t)
			ProjectSession(root, p, pod, nil, time.Now())
			c := &outageClient{Client: &statusClient{Client: fakeClient(t, root, p, pod)}}
			role := &pendingRole{}
			r := &Runtime{
				Client:          c,
				Namespace:       p.Namespace,
				ParticipantName: p.Name,
				NetworkUID:      root.UID,
				ParticipantUID:  p.UID,
				PodName:         pod.Name,
				PodUID:          pod.UID,
				Profile:         profile,
				Role:            role,
				Prerequisites:   func(context.Context, Snapshot) error { return nil },
			}
			ctx := context.Background()
			if _, err := r.Reconcile(ctx); err != nil {
				t.Fatal(err)
			}
			// A faucet-style role never opts into cached baseline authorization.
			if r.appliedSnapshot != nil {
				t.Fatal("request role acquired baseline fallback")
			}
			role.available = true
			polls := role.polls
			offline := apierrors.NewServiceUnavailable("offline")
			if publication {
				c.patchErr = offline
			} else {
				c.readErr = offline
			}
			for range 3 {
				_, _ = r.Reconcile(ctx)
			}
			if !role.included || role.polls != polls+1 || role.sends != 1 {
				t.Fatalf("pending observation repeated or stalled: %+v", role)
			}
			original := role.observed
			c.readErr = nil
			c.patchErr = nil
			for range 3 {
				if _, err := r.Reconcile(ctx); err != nil {
					t.Fatal(err)
				}
			}
			var observed api.StacksNetworkParticipant
			if err := c.Get(ctx, client.ObjectKeyFromObject(p), &observed); err != nil {
				t.Fatal(err)
			}
			if role.sends != 1 || observed.Status.Execution.Transactions.Included != 1 ||
				!observed.Status.Execution.Transactions.LastInclusion.ObservedAt.Equal(&original) {
				t.Fatal("recovery lost original inclusion or sent again")
			}
		})
	}
}
