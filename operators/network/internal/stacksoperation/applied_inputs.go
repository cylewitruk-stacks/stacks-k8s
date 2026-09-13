package stacksoperation

import (
	"context"
	"fmt"

	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/stacksworker"
)

// appliedInputs retains one validated typed baseline in the surviving role process.
type appliedInputs[T any] struct {
	// digest identifies the locally adopted policy.
	digest string
	// value excludes private key bytes; mutable public members are cloned on store and use.
	value *T
}

// resolve uses cached data only through the framework's explicit applied-baseline authorization.
func (c *appliedInputs[T]) resolve(
	ctx context.Context,
	s stacksworker.Snapshot,
	applied string,
	resolve func(context.Context, stacksworker.Snapshot) (T, error),
	clone func(T) T,
) (T, stacksworker.Snapshot, error) {
	var zero T
	cached := func(snapshot stacksworker.Snapshot) (T, stacksworker.Snapshot, error) {
		if c.value == nil || snapshot.Participant == nil || snapshot.Participant.Status.Admission == nil ||
			c.digest != applied ||
			c.digest != snapshot.Participant.Status.Admission.PolicyDigest {
			return zero, snapshot, fmt.Errorf("applied input cache unavailable")
		}
		return clone(*c.value), snapshot, nil
	}
	if s.CachedApplied {
		return cached(s)
	}
	in, err := resolve(ctx, s)
	if err == nil {
		return in, s, nil
	}
	if s.AppliedFallback != nil {
		if fallback, ok := s.AppliedFallback(applied, err); ok {
			return cached(fallback)
		}
	}
	if !stacksworker.TransientAPIError(err) {
		c.value = nil
		c.digest = ""
	}
	return zero, s, err
}

// remember follows successful live typed validation; offline policy never enters this path.
func (c *appliedInputs[T]) remember(s stacksworker.Snapshot, in T, clone func(T) T) {
	if s.CachedApplied {
		return
	}
	copied := clone(in)
	c.value = &copied
	c.digest = s.Participant.Status.Admission.PolicyDigest
	if s.RememberApplied != nil {
		s.RememberApplied(c.digest)
	}
}

// invalidate withdraws cached send authority after a definite typed-policy rejection.
func (c *appliedInputs[T]) invalidate(s stacksworker.Snapshot, applied string) {
	c.value = nil
	c.digest = ""
	if s.AppliedFallback != nil {
		s.AppliedFallback(applied, fmt.Errorf("typed applied policy invalid"))
	}
}
