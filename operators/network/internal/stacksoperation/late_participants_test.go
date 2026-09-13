package stacksoperation

import (
	"context"
	"testing"

	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/stacksworker"
)

func TestLatePoX4EnrollmentUsesCurrentCycleWithoutReplayingBootstrap(t *testing.T) {
	r, n, s := newPoXFixture(t)
	resolve := r.Resolve
	r.Resolve = func(ctx context.Context, snapshot stacksworker.Snapshot) (PoX4Inputs, error) {
		in, err := resolve(ctx, snapshot)
		in.InitialCohort, in.TargetCycle = false, 0
		return in, err
	}
	n.cycle, n.burn, n.first = 13, 270, 14
	result, err := r.Step(context.Background(), s)
	if err != nil || result.Failed || result.Pending != 1 || len(n.sent) != 1 || r.goal == nil || r.goal.target != 14 {
		t.Fatalf("late enrollment inherited the frozen cycle/ceiling: %+v %v", result, err)
	}
	n.present = true
	n.nonce++
	result, err = r.Step(context.Background(), s)
	if err != nil || result.Pending != 0 || result.PoX4 == nil || result.PoX4.TargetCycle != 14 || result.Failed {
		t.Fatalf("late native enrollment did not settle: %+v %v", result, err)
	}
	_, _ = r.Step(context.Background(), s)
	if len(n.sent) != 1 {
		t.Fatal("settled late enrollment replayed")
	}
}

func TestLatePoX5UsesExistingManagerAndCurrentCycleAfterOutage(t *testing.T) {
	for _, shared := range []bool{false, true} {
		t.Run(map[bool]string{false: "separate-keys", true: "shared-administrator"}[shared], func(t *testing.T) {
			r, n, s := newPoX5Fixture(t, shared)
			resolve := r.ResolvePoX5
			r.ResolvePoX5 = func(ctx context.Context, snapshot stacksworker.Snapshot) (PoX5Inputs, error) {
				in, err := resolve(ctx, snapshot)
				in.InitialCohort, in.TargetCycle = false, 0
				return in, err
			}
			n.readyManager()
			n.cycle, n.burn, n.first = 20, 404, 21
			s.Paused = true
			paused, err := r.Step(context.Background(), s)
			if err != nil || paused.Failed || len(n.sent) != 0 {
				t.Fatalf("late observation failed or submitted while paused: %+v %v", paused, err)
			}
			// Cached public policy must not cache the eligible native enrollment cycle.
			s.Paused, s.CachedApplied = false, true
			n.cycle, n.burn, n.first = 22, 444, 23
			result, err := r.Step(context.Background(), s)
			if err != nil || result.Failed || result.Pending != 1 || len(n.sent) != 1 || r.goal == nil ||
				r.goal.kind != "PoX5Enrollment" ||
				r.goal.target != 23 {
				t.Fatalf("late worker replayed manager/bootstrap or used cached cycle: %+v %v", result, err)
			}
			n.present = true
			n.nonce++
			result, err = r.Step(context.Background(), s)
			if err != nil || result.Pending != 0 || result.PoX5 == nil || result.PoX5.TargetCycle != 23 ||
				result.Failed {
				t.Fatalf("late enrollment did not settle: %+v %v", result, err)
			}
			_, _ = r.Step(context.Background(), s)
			if len(n.sent) != 1 {
				t.Fatal("late worker repeated a completed native operation")
			}
		})
	}
}

func TestInitialPoX5CohortStillFailsMissedFrozenCycle(t *testing.T) {
	r, n, s := newPoX5Fixture(t, false)
	n.cycle, n.burn = 20, 404
	n.readyManager()
	result, err := r.Step(context.Background(), s)
	if err != nil || !result.Failed || result.Reason != "BootstrapWindowMissed" || len(n.sent) != 0 {
		t.Fatalf("initial cohort bypassed its immutable target: %+v %v", result, err)
	}
}
