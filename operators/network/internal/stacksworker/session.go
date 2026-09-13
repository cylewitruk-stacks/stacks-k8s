// Package stacksworker implements exact-Pod lifecycle for scoped Stacks mutation workers.
package stacksworker

import (
	"errors"
	"fmt"
	"time"

	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/foundation"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

const (
	// ShutdownBound limits settlement, never the time required to prove process termination.
	ShutdownBound = 30 * time.Second
	// PodFinalizer preserves kubelet termination evidence before object disappearance.
	PodFinalizer = "network.stacks.org/stacks-worker-termination"
	// Finalizer orders participant disposal after the retained worker session.
	Finalizer    = "network.stacks.org/stacks-worker"
	profileLabel = "network.stacks.org/worker-profile"
)

// SessionFact is a projection result for the aggregate's sole conflict-checked root writer.
type SessionFact struct {
	// Changed requires persisting the updated root ledger before dependent actions.
	Changed bool
	// Failed requires latching root failure without authorizing a replacement worker.
	Failed bool
	// Unknown means current process identity or termination could not be established.
	Unknown bool
	// Reason supplies a bounded public classification.
	Reason string
}

// errParticipantAllocating holds activation until the aggregate publishes the new identity.
var errParticipantAllocating = errors.New("participant identity publication pending")

// Session returns the retained exact participant identity without reconstructing missing state.
func Session(root *api.StacksNetwork, p *api.StacksNetworkParticipant) (*api.InstanceIdentity, error) {
	if root.UID != p.Spec.NetworkUID || root.Namespace != p.Namespace || !metav1.IsControlledBy(p, root) {
		return nil, fmt.Errorf("network identity changed")
	}
	for i := range root.Status.Identities {
		identity := &root.Status.Identities[i]
		if identity.Name == p.Spec.ParticipantName {
			if identity.UID != p.UID {
				return nil, fmt.Errorf("participant identity changed")
			}
			return identity, nil
		}
	}
	if foundation.PendingWorkerAllocation(root, p) {
		return nil, errParticipantAllocating
	}
	return nil, fmt.Errorf("participant identity ledger missing")
}

// ProjectSession updates only the supplied root ledger; its caller persists root status.
// Pod and participant inputs must be fresh direct reads. API errors remain Unknown.
func ProjectSession(root *api.StacksNetwork, p *api.StacksNetworkParticipant, pod *corev1.Pod, readErr error, now time.Time) SessionFact {
	id, err := Session(root, p)
	if errors.Is(err, errParticipantAllocating) {
		return SessionFact{Unknown: true, Reason: "AllocationPending"}
	}
	if err != nil {
		return SessionFact{Failed: true, Reason: "WorkerIdentityLost"}
	}
	if readErr != nil && !apierrors.IsNotFound(readErr) {
		return SessionFact{Unknown: true, Reason: "WorkerObservationUnavailable"}
	}
	if id.Worker == nil {
		if p.Status.Execution != nil && p.Status.Execution.Phase != api.WorkerPhaseInactive {
			return SessionFact{Failed: true, Reason: "WorkerBindingLost"}
		}
		if stoppedReason(root, p, id) != "" || root.Spec.Operation != api.NetworkOperationRunning || failed(root) {
			return SessionFact{Reason: "ActivationHeld"}
		}
		if p.Status.Runtime == nil || p.Status.Runtime.WorkerCandidate == nil {
			return SessionFact{Reason: "CandidatePending"}
		}
		candidate := p.Status.Runtime.WorkerCandidate
		if readErr != nil || pod == nil {
			return SessionFact{Unknown: true, Reason: "CandidateUnavailable"}
		}
		if pod.Name != Name(p) || candidate.Pod.Kind != "Pod" || candidate.Pod.Name != pod.Name || candidate.Pod.UID == "" || candidate.Pod.UID != pod.UID || !ownedPod(pod, p) || pod.Spec.RestartPolicy != corev1.RestartPolicyNever || candidate.ProfileDigest == "" || pod.Annotations[profileLabel] != candidate.ProfileDigest {
			return SessionFact{Failed: true, Reason: "CandidateConflict"}
		}
		if pod.DeletionTimestamp != nil || terminal(pod) {
			return SessionFact{Reason: "InactiveCandidateExited"}
		}
		if _, err := profileFromPod(pod); err != nil {
			return SessionFact{Failed: true, Reason: "CandidateProfileChanged"}
		}
		id.Worker = &api.WorkerSession{Pod: candidate.Pod, ProfileDigest: candidate.ProfileDigest}
		return SessionFact{Changed: true, Reason: "WorkerBound"}
	}
	session := id.Worker
	if session.Pod.UID == "" || session.Pod.Name != Name(p) || session.ProfileDigest == "" {
		return SessionFact{Failed: true, Reason: "WorkerBindingCorrupt"}
	}
	if readErr != nil || pod == nil {
		if session.Disposal != nil && session.Disposal.Terminated {
			return SessionFact{Reason: "WorkerDisposed"}
		}
		return SessionFact{Failed: true, Unknown: true, Reason: "BoundWorkerLost"}
	}
	if pod.UID != session.Pod.UID || !ownedPod(pod, p) || pod.Annotations[profileLabel] != session.ProfileDigest {
		return SessionFact{Failed: true, Unknown: true, Reason: "BoundWorkerReplaced"}
	}
	if session.Disposal != nil {
		if Terminated(pod) && !session.Disposal.Terminated {
			session.Disposal.Terminated = true
			return SessionFact{Changed: true, Failed: session.Disposal.Outcome == api.WorkerDisposalUnsettled, Reason: "WorkerTerminationConfirmed"}
		}
		return SessionFact{Failed: session.Disposal.Outcome == api.WorkerDisposalUnsettled, Unknown: !session.Disposal.Terminated, Reason: "WorkerDisposing"}
	}
	if session.Shutdown == nil {
		if pod.DeletionTimestamp != nil || terminal(pod) {
			if reason := stoppedReason(root, p, id); reason != "" {
				session.Shutdown = &api.WorkerShutdown{NetworkGeneration: root.Generation, Reason: reason, RequestedAt: metav1.NewTime(now)}
				session.Disposal = &api.WorkerDisposal{Outcome: api.WorkerDisposalUnsettled, ObservedAt: metav1.NewTime(now), Terminated: Terminated(pod)}
				return SessionFact{Changed: true, Failed: true, Unknown: !session.Disposal.Terminated, Reason: "PriorWorkerExitUnsettled"}
			}
			return SessionFact{Failed: true, Unknown: !Terminated(pod), Reason: "BoundWorkerExited"}
		}
		if reason := stoppedReason(root, p, id); reason != "" {
			session.Shutdown = &api.WorkerShutdown{NetworkGeneration: root.Generation, Reason: reason, RequestedAt: metav1.NewTime(now)}
			return SessionFact{Changed: true, Reason: "WorkerShutdownRequested"}
		}
	}
	if execution := p.Status.Execution; session.Shutdown == nil && execution != nil && execution.PodUID == session.Pod.UID && execution.ProfileDigest == session.ProfileDigest && execution.Phase == api.WorkerPhaseFailed {
		return SessionFact{Failed: true, Reason: "WorkerProtocolFailed"}
	}
	if session.Shutdown == nil {
		if _, err := profileFromPod(pod); err != nil {
			return SessionFact{Failed: true, Reason: "WorkerProfileChanged"}
		}
		return SessionFact{Reason: "WorkerBound"}
	}
	if execution := p.Status.Execution; execution != nil && execution.PodUID == session.Pod.UID && execution.ProcessNonce != "" && execution.ProfileDigest == session.ProfileDigest && execution.NetworkGeneration == session.Shutdown.NetworkGeneration && execution.Reason == string(session.Shutdown.Reason) && (execution.Phase == api.WorkerPhaseSettled || execution.Phase == api.WorkerPhaseUnsettled) {
		outcome := api.WorkerDisposalOutcome(execution.Phase)
		if execution.Pending > 0 {
			outcome = api.WorkerDisposalUnsettled
		}
		session.Disposal = &api.WorkerDisposal{Outcome: outcome, ProcessNonce: execution.ProcessNonce, ObservedAt: execution.ObservedAt, Terminated: Terminated(pod)}
		return SessionFact{Changed: true, Failed: outcome == api.WorkerDisposalUnsettled, Reason: "WorkerDisposalAcknowledged"}
	}
	if terminal(pod) || pod.DeletionTimestamp != nil || now.Sub(session.Shutdown.RequestedAt.Time) >= ShutdownBound {
		session.Disposal = &api.WorkerDisposal{Outcome: api.WorkerDisposalUnsettled, ObservedAt: metav1.NewTime(now), Terminated: Terminated(pod)}
		return SessionFact{Changed: true, Failed: true, Unknown: !session.Disposal.Terminated, Reason: "WorkerSettlementUnconfirmed"}
	}
	return SessionFact{Reason: "WorkerDraining"}
}

// failed preserves the aggregate's durable experiment failure latch.
func failed(root *api.StacksNetwork) bool {
	return root.Status.Phase == api.NetworkPhaseFailed || meta.IsStatusConditionTrue(root.Status.Conditions, "Failed")
}

// stoppedReason evaluates current desired controls independently of admitted policy.
func stoppedReason(root *api.StacksNetwork, p *api.StacksNetworkParticipant, id *api.InstanceIdentity) api.WorkerShutdownReason {
	if root.DeletionTimestamp != nil {
		return api.WorkerShutdownNetworkDeleting
	}
	if root.Spec.Operation == api.NetworkOperationStopped {
		return api.WorkerShutdownNetworkStopped
	}
	if p.DeletionTimestamp != nil || id.Removing {
		return api.WorkerShutdownParticipantRemoved
	}
	for _, entry := range root.Spec.Participants {
		if entry.Name == p.Spec.ParticipantName && entry.Kind == p.Spec.Kind {
			return ""
		}
	}
	return api.WorkerShutdownParticipantRemoved
}

// paused includes root failure and current per-entry controls without adopting rejected policy.
func paused(root *api.StacksNetwork, p *api.StacksNetworkParticipant) bool {
	if root.Spec.Operation != api.NetworkOperationRunning || failed(root) {
		return true
	}
	for _, entry := range root.Spec.Participants {
		if entry.Name == p.Spec.ParticipantName && entry.Control != nil {
			return ptr.Deref(entry.Control.Paused, false)
		}
	}
	return false
}

// terminal recognizes an exited Pod without claiming its process termination is known.
func terminal(pod *corev1.Pod) bool {
	return pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed
}

// Terminated requires exact kubelet exit evidence or proof no process was scheduled.
func Terminated(pod *corev1.Pod) bool {
	if pod.DeletionTimestamp != nil && pod.Spec.NodeName == "" && len(pod.Status.ContainerStatuses) == 0 && len(pod.Status.InitContainerStatuses) == 0 {
		return true
	}
	if !terminal(pod) || len(pod.Spec.Containers) != 1 || len(pod.Spec.InitContainers) != 0 || len(pod.Spec.EphemeralContainers) != 0 {
		return false
	}
	for _, status := range pod.Status.ContainerStatuses {
		if status.Name == "worker" && status.State.Terminated != nil && status.State.Terminated.Reason != "ContainerStatusUnknown" {
			return true
		}
	}
	return false
}
