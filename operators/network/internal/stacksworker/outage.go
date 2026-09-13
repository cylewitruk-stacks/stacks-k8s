package stacksworker

import (
	"context"

	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/foundation"
)

// TransientAPIError preserves the shared availability-versus-identity classification.
func TransientAPIError(err error) bool { return foundation.TransientAPIError(err) }

// baselineKind excludes request-driven workers from cached send authority and summary coalescing.
func baselineKind(kind api.ParticipantKind) bool {
	return kind == api.ParticipantStacksTransactionProduction || kind == api.ParticipantStacksStacker || kind == api.ParticipantStacksContractSet
}

// copySnapshot preserves only public observations; callbacks are supplied for each invocation.
func copySnapshot(s Snapshot) Snapshot {
	out := s
	out.Network = s.Network.DeepCopy()
	out.Participant = s.Participant.DeepCopy()
	out.Authorize = nil
	out.AppliedFallback = nil
	out.RememberApplied = nil
	return out
}

// controlAllows reports definite currently observed eligibility independently of new policy adoption.
func controlAllows(s Snapshot) bool {
	id, err := Session(s.Network, s.Participant)
	return err == nil && id.Worker != nil && id.Worker.Shutdown == nil && id.Worker.Disposal == nil && !s.Paused && stoppedReason(s.Network, s.Participant, id) == "" && s.Network.Status.GenesisRef != nil
}

// observePartialRoot invalidates cached authority even if a later API read in this pass fails.
func (r *Runtime) observePartialRoot(root *api.StacksNetwork) {
	if root.UID != r.NetworkUID || root.DeletionTimestamp != nil || root.Spec.Operation != api.NetworkOperationRunning || failed(root) {
		r.cacheHeld = true
		return
	}
	if r.appliedSnapshot == nil {
		return
	}
	p := r.appliedSnapshot.Participant
	id, err := Session(root, p)
	if err != nil || id.Worker == nil || id.Worker.Pod.UID != r.PodUID || id.Worker.Shutdown != nil || id.Worker.Disposal != nil || stoppedReason(root, p, id) != "" || paused(root, p) {
		r.cacheHeld = true
	}
}

// cacheEligible requires in-process activation and an explicitly validated applied baseline.
func (r *Runtime) cacheEligible(digest string) bool {
	return r.activated && r.processAcknowledged && r.Prerequisites != nil && !r.cacheBlocked && !r.cacheHeld && r.appliedSnapshot != nil && r.lastControl != nil && baselineKind(r.appliedSnapshot.Participant.Spec.Kind) && r.appliedSnapshot.Participant.Status.Admission != nil && r.appliedSnapshot.Participant.Status.Admission.PolicyDigest == digest && controlAllows(*r.lastControl)
}

// decorate binds callbacks to this invocation's actual observed control and admission.
func (r *Runtime) decorate(s Snapshot) Snapshot {
	expected := copySnapshot(s)
	s.Authorize = func(ctx context.Context) error { return r.authorizeInvocation(ctx, expected, s.CachedApplied) }
	s.RememberApplied = func(digest string) {
		if s.CachedApplied || s.admissionError != nil || !r.activated || !r.processAcknowledged || !baselineKind(s.Participant.Spec.Kind) || s.Participant.Status.Admission == nil || digest != s.Participant.Status.Admission.PolicyDigest || foundation.Digest(s.Participant.Status.Admission.Configuration) != digest || foundation.AdmissionReady(s.Participant) != nil {
			return
		}
		saved := copySnapshot(s)
		r.appliedSnapshot = &saved
		// A successful live role resolution revalidates the applied dependencies.
		r.cacheHeld = !controlAllows(s)
	}
	s.AppliedFallback = func(digest string, cause error) (Snapshot, bool) {
		if !TransientAPIError(cause) {
			if cause != nil {
				r.cacheHeld = true
			}
			return Snapshot{}, false
		}
		cached, ok := r.cachedSnapshot(digest)
		if ok {
			r.stepUsedCached = true
		}
		return cached, ok
	}
	return s
}

// cachedSnapshot combines the applied policy with the latest actually observed controls.
func (r *Runtime) cachedSnapshot(digest string) (Snapshot, bool) {
	if !r.activated || !r.processAcknowledged || r.Prerequisites == nil || r.cacheBlocked || r.appliedSnapshot == nil || r.lastControl == nil || !baselineKind(r.appliedSnapshot.Participant.Spec.Kind) || r.appliedSnapshot.Participant.Status.Admission == nil || r.appliedSnapshot.Participant.Status.Admission.PolicyDigest != digest {
		return Snapshot{}, false
	}
	cached := copySnapshot(*r.appliedSnapshot)
	controls := copySnapshot(*r.lastControl)
	cached.Network = controls.Network
	cached.Participant.Generation = controls.Participant.Generation
	cached.Participant.ResourceVersion = controls.Participant.ResourceVersion
	cached.Participant.Spec.Control = controls.Participant.Spec.Control
	cached.Participant.Status.Execution = controls.Participant.Status.Execution.DeepCopy()
	cached.Paused = controls.Paused || r.cacheHeld || !controlAllows(controls)
	cached.Shutdown = controls.Shutdown
	cached.CachedApplied = true
	cached = r.decorate(cached)
	// Prepared cached bytes are still bound to the actual admission observed before preparation.
	cached.Authorize = func(ctx context.Context) error { return r.authorizeInvocation(ctx, controls, true) }
	return cached, true
}

// executionCountersAdvance rejects regressions instead of hiding them by taking maxima.
func executionCountersAdvance(previous, current *api.TransactionExecutionStatus) bool {
	if previous == nil {
		return true
	}
	if current == nil {
		return false
	}
	return current.Offered >= previous.Offered && current.Accepted >= previous.Accepted && current.Included >= previous.Included && current.Rejected >= previous.Rejected && current.Uncertain >= previous.Uncertain && current.PostconditionObserved >= previous.PostconditionObserved
}
