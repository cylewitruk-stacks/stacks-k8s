package networkruntime

import (
	"context"
	"time"

	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/foundation"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/stacksworker"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// projectWorkers advances exact-Pod bindings through the aggregate's single status writer.
func (r *Reconciler) projectWorkers(ctx context.Context, root *api.StacksNetwork, participants []api.StacksNetworkParticipant) (bool, map[types.UID]bool, error) {
	pending := false
	unstarted := map[types.UID]bool{}
	for i := range participants {
		cached := &participants[i]
		if cached.Spec.NetworkUID != root.UID || !metav1.IsControlledBy(cached, root) || !managementKind(cached.Spec.Kind) {
			continue
		}
		p := cached.DeepCopy()
		var pod corev1.Pod
		err := r.observations().Get(ctx, client.ObjectKey{Namespace: p.Namespace, Name: stacksworker.Name(p)}, &pod)
		// Speculative projection cannot publish binding, failure or disposal facts.
		trial := root.DeepCopy()
		fact := stacksworker.ProjectSession(trial, p, &pod, err, time.Now())
		if fact.Changed || fact.Failed || fact.Unknown {
			if readErr := r.Reader.Get(ctx, client.ObjectKeyFromObject(cached), p); readErr != nil {
				return true, nil, readErr
			}
			if p.UID != cached.UID {
				return true, nil, nil
			}
			err = r.Reader.Get(ctx, client.ObjectKey{Namespace: p.Namespace, Name: stacksworker.Name(p)}, &pod)
			fact = stacksworker.ProjectSession(root, p, &pod, err, time.Now())
		}
		if foundation.PendingWorkerAllocation(root, p) && apierrors.IsNotFound(err) {
			unstarted[p.UID] = true
		}
		if fact.Failed {
			set(root, "Failed", metav1.ConditionTrue, fact.Reason, "A bound management worker cannot continue; recreate the network")
			root.Status.Phase = "Failed"
		}
		if (fact.Unknown && !unstarted[p.UID]) || fact.Changed {
			pending = true
		}
	}
	return pending, unstarted, nil
}

// managementKind identifies process-local Stacks roles with terminal crash semantics.
func managementKind(kind api.ParticipantKind) bool {
	return foundation.ManagementKind(kind)
}

// workersPaused requires current cooperative acknowledgement from every bound active process.
// Pending chain operations may remain; this is not a protocol quiescence claim.
func workersPaused(root *api.StacksNetwork, participants []api.StacksNetworkParticipant, now time.Time) bool {
	for _, id := range root.Status.Identities {
		if id.Worker == nil || id.Removing {
			continue
		}
		if id.Worker.Disposal != nil && id.Worker.Disposal.Terminated {
			continue
		}
		var p *api.StacksNetworkParticipant
		for i := range participants {
			if participants[i].UID == id.UID && participants[i].Spec.NetworkUID == root.UID && metav1.IsControlledBy(&participants[i], root) {
				p = &participants[i]
				break
			}
		}
		if p == nil || p.Status.Execution == nil {
			return false
		}
		e := p.Status.Execution
		if e.PodUID != id.Worker.Pod.UID || e.ProfileDigest != id.Worker.ProfileDigest || e.ProcessNonce == "" || e.ObservedGeneration != p.Generation || e.NetworkGeneration != root.Generation || e.Phase != "Paused" || !fresh(e.ObservedAt, now) {
			return false
		}
	}
	return true
}
