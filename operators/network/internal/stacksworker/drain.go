package stacksworker

import (
	"context"
	"fmt"

	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// CheckActorStop preserves Stacks RPC and consensus actors until root shutdown dispositions exist.
// Reader must bypass caches. Individual actor removal and suspension remain destructive controls.
func CheckActorStop(ctx context.Context, reader client.Reader, actor *api.StacksNetworkParticipant) (bool, error) {
	if actor.Spec.Kind != api.ParticipantStacksNode && actor.Spec.Kind != api.ParticipantStacksSigner {
		return false, fmt.Errorf("unsupported Stacks actor kind")
	}
	root := &api.StacksNetwork{}
	if err := reader.Get(ctx, client.ObjectKey{Namespace: actor.Namespace, Name: "network"}, root); err != nil {
		return false, err
	}
	owner := metav1.GetControllerOf(actor)
	if root.UID == "" || owner == nil || owner.APIVersion != api.GroupVersion.String() || owner.Kind != api.KindStacksNetwork || owner.Name != root.Name {
		return false, fmt.Errorf("actor root ownership unavailable")
	}
	if _, err := Session(root, actor); err != nil {
		return false, err
	}
	if root.Spec.Operation != api.NetworkOperationStopped && root.DeletionTimestamp == nil {
		return true, nil
	}
	for _, identity := range root.Status.Identities {
		session := identity.Worker
		if session == nil {
			continue
		}
		if identity.UID == "" || identity.Name == "" || session.Pod.Kind != common.KindPod || session.Pod.Name == "" || session.Pod.UID == "" || session.ProfileDigest == "" {
			return false, fmt.Errorf("retained worker identity unavailable")
		}
		if session.Disposal == nil {
			return false, nil
		}
		if session.Shutdown == nil || session.Shutdown.NetworkGeneration < 1 || session.Shutdown.Reason != api.WorkerShutdownNetworkStopped && session.Shutdown.Reason != api.WorkerShutdownNetworkDeleting && session.Shutdown.Reason != api.WorkerShutdownParticipantRemoved || session.Shutdown.RequestedAt.IsZero() || session.Disposal.ObservedAt.IsZero() || session.Disposal.Outcome != api.WorkerDisposalSettled && session.Disposal.Outcome != api.WorkerDisposalUnsettled {
			return false, fmt.Errorf("retained worker disposition unavailable")
		}
	}
	return true, nil
}
