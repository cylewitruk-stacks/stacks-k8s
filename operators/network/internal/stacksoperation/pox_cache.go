package stacksoperation

import (
	"context"
	"errors"
	"math/big"

	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/stacksworker"
)

// clonePoX4Inputs isolates mutable amount values from resolver and pending-goal ownership.
func clonePoX4Inputs(input PoX4Inputs) PoX4Inputs {
	if input.Amount != nil {
		input.Amount = new(big.Int).Set(input.Amount)
	}
	return input
}

// clonePoX5Inputs isolates mutable amount values from resolver and pending-goal ownership.
func clonePoX5Inputs(input PoX5Inputs) PoX5Inputs {
	if input.Amount != nil {
		input.Amount = new(big.Int).Set(input.Amount)
	}
	return input
}

// cachedPolicyMatches binds a typed input to the runtime's actually applied snapshot.
func cachedPolicyMatches(snapshot stacksworker.Snapshot, digest, applied string) bool {
	return snapshot.CachedApplied && digest != "" && digest == applied && snapshot.Participant != nil && snapshot.Participant.Status.Admission != nil && snapshot.Participant.Status.Admission.PolicyDigest == digest
}

// inputs uses a retained complete policy only under the runtime's explicit surviving-session authority.
func (r *PoX4Role) inputs(ctx context.Context, snapshot stacksworker.Snapshot) (PoX4Inputs, stacksworker.Snapshot, error) {
	if snapshot.CachedApplied {
		if r.cached != nil && cachedPolicyMatches(snapshot, r.cachedDigest, r.applied) {
			return clonePoX4Inputs(*r.cached), snapshot, nil
		}
		return PoX4Inputs{}, snapshot, errors.New("applied PoX4 policy unavailable")
	}
	input, err := r.Resolve(ctx, snapshot)
	if err == nil {
		return input, snapshot, nil
	}
	if r.cached != nil && snapshot.AppliedFallback != nil {
		fallback, ok := snapshot.AppliedFallback(r.applied, err)
		if ok && cachedPolicyMatches(fallback, r.cachedDigest, r.applied) {
			return clonePoX4Inputs(*r.cached), fallback, nil
		}
	}
	r.cached = nil
	return PoX4Inputs{}, snapshot, err
}

// inputs retains administrator and holder bindings together when the Kubernetes API is transiently unavailable.
func (r *StackerRole) inputs(ctx context.Context, snapshot stacksworker.Snapshot) (PoX5Inputs, stacksworker.Snapshot, error) {
	if snapshot.CachedApplied {
		if r.cached != nil && cachedPolicyMatches(snapshot, r.cachedDigest, r.applied) {
			return clonePoX5Inputs(*r.cached), snapshot, nil
		}
		return PoX5Inputs{}, snapshot, errors.New("applied PoX5 policy unavailable")
	}
	input, err := r.ResolvePoX5(ctx, snapshot)
	if err == nil {
		return input, snapshot, nil
	}
	if r.cached != nil && snapshot.AppliedFallback != nil {
		fallback, ok := snapshot.AppliedFallback(r.applied, err)
		if ok && cachedPolicyMatches(fallback, r.cachedDigest, r.applied) {
			return clonePoX5Inputs(*r.cached), fallback, nil
		}
	}
	r.cached = nil
	return PoX5Inputs{}, snapshot, err
}

// legacyStep preserves the policy actually applied by the retained PoX4 stage.
func (r *StackerRole) legacyStep(ctx context.Context, snapshot stacksworker.Snapshot) (stacksworker.RoleResult, error) {
	result, err := r.legacy.Step(ctx, snapshot)
	if result.AppliedPolicyDigest != "" {
		r.applied = result.AppliedPolicyDigest
	}
	return result, err
}

// poxBlocked distinguishes waiting for a valid submission outcome from unavailable authority.
func poxBlocked(reason string, pending int32) bool {
	if pending > 0 {
		return false
	}
	switch reason {
	case "Paused", "StateObserved", "Included", "Idle", "PoX4EnrollmentObserved", "PoX5EnrollmentObserved", "PoX4InclusionObservedAtTransition", "PoX5PostconditionObserved", "AwaitingMaintenanceWindow":
		return false
	}
	return true
}
