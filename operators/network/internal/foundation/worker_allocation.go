package foundation

import (
	"reflect"

	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ManagementKind identifies Stacks participants with non-restarting mutation workers.
func ManagementKind(kind api.ParticipantKind) bool {
	switch kind {
	case api.ParticipantStacksStacker, api.ParticipantStacksContractSet, api.ParticipantStacksFaucet, api.ParticipantStacksTransactionProduction:
		return true
	}
	return false
}

// PendingWorkerAllocation recognizes an unpublished first identity without activation history.
// It authorizes neither worker creation nor reconstruction of a missing session.
func PendingWorkerAllocation(root *api.StacksNetwork, p *api.StacksNetworkParticipant) bool {
	if root == nil || p == nil || !ManagementKind(p.Spec.Kind) || root.UID == "" || p.UID == "" || root.Namespace != p.Namespace || root.UID != p.Spec.NetworkUID || len(p.Finalizers) != 0 {
		return false
	}
	owner := metav1.GetControllerOf(p)
	if owner == nil || owner.APIVersion != api.GroupVersion.String() || owner.Kind != "StacksNetwork" || owner.Name != root.Name || owner.UID != root.UID || p.Name != ParticipantName(string(root.UID), p.Spec.ParticipantName) {
		return false
	}
	if findIdentity(root.Status.Identities, p.Spec.ParticipantName) != nil || p.Status.Admission != nil || p.Status.Execution != nil {
		return false
	}
	if p.Status.Runtime != nil {
		state := *p.Status.Runtime
		state.ObservedGeneration = 0
		if !reflect.DeepEqual(state, api.ParticipantRuntimeStatus{}) {
			return false
		}
	}
	for _, entry := range root.Spec.Participants {
		if entry.Name == p.Spec.ParticipantName && entry.Kind == p.Spec.Kind {
			return true
		}
	}
	return false
}
