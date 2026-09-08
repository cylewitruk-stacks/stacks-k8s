package managedoperation

import (
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/stacksrpc"
	"testing"
)

// TestStackingOperationSeparatesProtocolAndRenewal pins enrollment, renewal and prepare boundaries.
func TestStackingOperationSeparatesProtocolAndRenewal(t *testing.T) {
	for _, protocol := range []string{pox4, pox5} {
		t.Run(protocol, func(t *testing.T) {
			p := stacksrpc.PoX{Contract: protocol, RewardCycle: 12, CycleLength: 20, BurnHeight: 246, BlocksUntilPrepare: 9}
			a := stacksrpc.Account{}
			op, cycles, enroll, err := stackingOperation(p, a, 12)
			if err != nil || op != "enroll" || cycles != 12 {
				t.Fatalf("enrollment: %s/%d/%v", op, cycles, err)
			}
			a.Locked = 100000000000
			a.UnlockHeight = 500
			op, _, _, err = stackingOperation(p, a, 12)
			if err != nil || op != "" {
				t.Fatalf("adequate lock should need no mutation: %s/%v", op, err)
			}
			a.UnlockHeight = 366
			op, cycles, extend, err := stackingOperation(p, a, 12)
			if err != nil || op != "extend" || cycles != 6 || extend <= enroll {
				t.Fatalf("renewal: %s/%d/%d/%v", op, cycles, extend, err)
			}
			p.BlocksUntilPrepare = 1
			op, _, _, err = stackingOperation(p, a, 12)
			if err != nil || op != "" {
				t.Fatalf("locked account should wait through narrow window: %s/%v", op, err)
			}
			a.Locked = 0
			if _, _, _, err = stackingOperation(p, a, 12); err == nil {
				t.Fatal("enrollment admitted too close to prepare phase")
			}
			p.BlocksUntilPrepare = 2
			p.RewardCycle++
			op, _, next, err := stackingOperation(p, a, 12)
			if err != nil || op != "enroll" || next <= extend {
				t.Fatalf("next reward window: %s/%d/%v", op, next, err)
			}
		})
	}
	p := stacksrpc.PoX{Contract: pox4, RewardCycle: 20, CycleLength: 20, BurnHeight: 400, BlocksUntilPrepare: 5}
	_, _, old, err := stackingOperation(p, stacksrpc.Account{}, 12)
	if err != nil {
		t.Fatal(err)
	}
	p.Contract = pox5
	_, _, next, err := stackingOperation(p, stacksrpc.Account{}, 12)
	if err != nil || next <= old {
		t.Fatal("protocol transition regressed account operation identity")
	}
	for _, horizon := range []int32{0, 1, 13} {
		if _, _, _, err := stackingOperation(p, stacksrpc.Account{}, horizon); err == nil {
			t.Fatalf("accepted horizon %d", horizon)
		}
	}
	p.Contract = "unsupported"
	if _, _, _, err := stackingOperation(p, stacksrpc.Account{}, 12); err == nil {
		t.Fatal("accepted unsupported PoX")
	}
}

// TestPoX4UnlockBeforeContractSwitchDoesNotAuthorizeReenrollment pins the observed transition boundary.
func TestPoX4UnlockBeforeContractSwitchDoesNotAuthorizeReenrollment(t *testing.T) {
	for _, tc := range []struct {
		height   int64
		contract string
		waiting  bool
	}{{241, pox4, false}, {242, pox4, true}, {244, pox4, true}, {245, pox5, false}, {250, pox5, false}} {
		if got := legacyTransitionPending(stacksrpc.PoX{Contract: tc.contract, BurnHeight: tc.height}, 244); got != tc.waiting {
			t.Fatalf("height %d protocol %s: pending=%v", tc.height, tc.contract, got)
		}
	}
	if legacyTransitionPending(stacksrpc.PoX{Contract: pox4, BurnHeight: 244}, 1000005) {
		t.Fatal("transition guard ignored explicit deferred schedule")
	}
}
