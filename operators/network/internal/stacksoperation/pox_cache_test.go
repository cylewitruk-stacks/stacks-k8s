package stacksoperation

import (
	"context"
	"errors"
	"math/big"
	"testing"

	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/stacksworker"
)

func TestPoX4AppliedCacheIsExplicitAndCopiesAmount(t *testing.T) {
	r, n, s := newPoXFixture(t)
	input, _ := r.Resolve(context.Background(), s)
	r.Resolve = func(context.Context, stacksworker.Snapshot) (PoX4Inputs, error) { return input, nil }
	remembered := 0
	s.RememberApplied = func(digest string) {
		if digest != "policy" {
			t.Fatal("wrong applied digest")
		}
		remembered++
	}
	s.Paused = true
	_, _ = r.Step(context.Background(), s)
	if remembered != 1 || r.cached == nil {
		t.Fatal("validated live policy not remembered")
	}
	input.Amount.SetInt64(1)
	r.Resolve = func(context.Context, stacksworker.Snapshot) (PoX4Inputs, error) {
		t.Fatal("cached step reread Kubernetes dependencies")
		return PoX4Inputs{}, nil
	}
	s.CachedApplied = true
	s.Paused = false
	got, _ := r.Step(context.Background(), s)
	if got.Pending != 1 || len(n.sent) != 1 || r.goal.input.Amount.Cmp(big.NewInt(100000)) != 0 || remembered != 1 {
		t.Fatalf("cached policy changed or was not used: %+v", got)
	}
}

func TestPoXAppliedCacheCannotActivateOrAdoptDifferentDigest(t *testing.T) {
	for _, kind := range []string{"PoX4", "PoX5"} {
		t.Run(kind, func(t *testing.T) {
			ctx := context.Background()
			if kind == "PoX4" {
				r, n, s := newPoXFixture(t)
				s.CachedApplied = true
				got, _ := r.Step(ctx, s)
				if got.Pending != 0 || !got.Blocked || len(n.sent) != 0 {
					t.Fatal("cold cache activated")
				}
				s.CachedApplied = false
				s.Paused = true
				_, _ = r.Step(ctx, s)
				s.Participant = s.Participant.DeepCopy()
				s.Participant.Status.Admission.PolicyDigest = "new-policy"
				s.CachedApplied = true
				s.Paused = false
				got, _ = r.Step(ctx, s)
				if got.Pending != 0 || !got.Blocked || len(n.sent) != 0 {
					t.Fatal("different cached policy activated")
				}
			} else {
				r, n, s := newPoX5Fixture(t, false)
				s.CachedApplied = true
				got, _ := r.Step(ctx, s)
				if got.Pending != 0 || !got.Blocked || len(n.sent) != 0 {
					t.Fatal("cold cache activated")
				}
				s.CachedApplied = false
				s.Paused = true
				_, _ = r.Step(ctx, s)
				s.Participant = s.Participant.DeepCopy()
				s.Participant.Status.Admission.PolicyDigest = "new-policy"
				s.CachedApplied = true
				s.Paused = false
				got, _ = r.Step(ctx, s)
				if got.Pending != 0 || !got.Blocked || len(n.sent) != 0 {
					t.Fatal("different cached policy activated")
				}
			}
		})
	}
}

func TestPoX5TransientFallbackUsesOnlyPriorValidatedBindings(t *testing.T) {
	r, n, s := newPoX5Fixture(t, false)
	ctx := context.Background()
	s.Paused = true
	remembered := 0
	s.RememberApplied = func(string) { remembered++ }
	_, _ = r.Step(ctx, s)
	fallback := s
	fallback.Paused = false
	fallback.CachedApplied = true
	authorized := 0
	fallback.Authorize = func(context.Context) error { authorized++; return nil }
	transient := errors.New("API temporarily unavailable")
	r.ResolvePoX5 = func(context.Context, stacksworker.Snapshot) (PoX5Inputs, error) { return PoX5Inputs{}, transient }
	s.Participant = s.Participant.DeepCopy()
	s.Participant.Status.Admission.PolicyDigest = "unvalidated-update"
	s.AppliedFallback = func(digest string, err error) (stacksworker.Snapshot, bool) {
		if digest != "policy" || err != transient {
			t.Fatal("fallback used wrong policy/error")
		}
		return fallback, true
	}
	s.Authorize = func(context.Context) error { t.Fatal("new unvalidated authority was used"); return nil }
	got, _ := r.Step(ctx, s)
	if got.Pending != 1 || len(n.sent) != 1 || authorized != 1 || remembered != 1 || got.AppliedPolicyDigest != "policy" {
		t.Fatalf("fallback did not retain actually applied policy: %+v", got)
	}
}

func TestPoXDefiniteDependencyFailureInvalidatesTypedCache(t *testing.T) {
	r, n, s := newPoX5Fixture(t, false)
	ctx := context.Background()
	s.Paused = true
	_, _ = r.Step(ctx, s)
	r.ResolvePoX5 = func(context.Context, stacksworker.Snapshot) (PoX5Inputs, error) {
		return PoX5Inputs{}, errors.New("account replaced")
	}
	s.AppliedFallback = func(string, error) (stacksworker.Snapshot, bool) { return stacksworker.Snapshot{}, false }
	got, _ := r.Step(ctx, s)
	if !got.Blocked || r.cached != nil {
		t.Fatal("definite identity failure retained typed authority")
	}
	s.CachedApplied = true
	s.Paused = false
	got, _ = r.Step(ctx, s)
	if !got.Blocked || len(n.sent) != 0 {
		t.Fatal("definite identity loss resumed from cache")
	}
}

func TestCompositeCachedBindingsCrossNativeEpochWithoutNewAPIReads(t *testing.T) {
	r, n, s := newPoX5Fixture(t, true)
	n.legacy = true
	n.burn, n.cycle = 210, 10
	s.Paused = true
	ctx := context.Background()
	_, _ = r.Step(ctx, s)
	if r.applied != "policy" || r.cached == nil || r.legacy.cached == nil {
		t.Fatal("composite did not retain fully resolved inputs")
	}
	r.ResolvePoX4 = func(context.Context, stacksworker.Snapshot) (PoX4Inputs, error) {
		t.Fatal("cached PoX4 read public dependencies")
		return PoX4Inputs{}, nil
	}
	r.ResolvePoX5 = func(context.Context, stacksworker.Snapshot) (PoX5Inputs, error) {
		t.Fatal("cached PoX5 read public dependencies")
		return PoX5Inputs{}, nil
	}
	n.legacy = false
	n.burn, n.cycle = 284, 14
	s.CachedApplied = true
	s.Paused = false
	got, _ := r.Step(ctx, s)
	if got.Pending != 1 || len(n.sent) != 1 || !r.transitioned {
		t.Fatalf("native transition from cached applied inputs failed: %+v", got)
	}
}
