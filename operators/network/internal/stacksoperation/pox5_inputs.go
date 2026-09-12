package stacksoperation

import (
	"context"
	"errors"

	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/stacksworker"
)

// PoX5 resolves public administrator credentials in addition to the captured holder and signer.
func (r PublicInputs) PoX5(ctx context.Context, snapshot stacksworker.Snapshot) (PoX5Inputs, error) {
	var input PoX5Inputs
	legacy, err := r.PoX4(ctx, snapshot)
	if err != nil {
		return input, err
	}
	policy := snapshot.Participant.Status.Admission.Configuration.StacksStacker
	if policy.AdministratorAccountRef == nil {
		return input, errors.New("PoX administrator unavailable")
	}
	administrator, err := r.account(ctx, snapshot.Participant, policy.AdministratorAccountRef.Name)
	if err != nil {
		return input, err
	}
	node, ok := legacy.Node.(PoX5Node)
	if !ok {
		return input, errors.New("PoX source observation unavailable")
	}
	genesis, err := r.inputGenesis(ctx, snapshot, "StacksStacker")
	if err != nil {
		return input, err
	}
	captured := false
	for _, requirement := range genesis.Spec.Bootstrap.Requirements {
		if requirement.Kind == "StacksStacker" && requirement.Participant.UID == snapshot.Participant.UID {
			for _, account := range requirement.Accounts {
				captured = captured || account.Binding.UID == administrator.UID && account.Identity == *administrator.Status.Identity
			}
		}
	}
	if legacy.InitialCohort && !captured {
		return input, errors.New("captured administrator identity unavailable")
	}
	input = PoX5Inputs{InitialCohort: legacy.InitialCohort, Node: node, Holder: legacy.Holder, Administrator: administrator.Status.Identity.Address, SignerPublicKey: legacy.SignerPublicKey, Amount: legacy.Amount, LockCycles: legacy.LockCycles, RenewWhenRemainingCycles: legacy.RenewWhenRemainingCycles}
	for _, gate := range genesis.Spec.Bootstrap.Gates {
		if gate.Name == "EnrollPoX5" && gate.TargetCycle != nil && *gate.TargetCycle > 0 {
			if legacy.InitialCohort {
				input.TargetCycle = uint64(*gate.TargetCycle)
			}
		}
	}
	for _, epoch := range genesis.Spec.Chain.Epochs {
		if epoch.Name == "4.0" && epoch.StartHeight > 0 {
			input.Epoch4Height = uint64(epoch.StartHeight)
		}
	}
	if input.InitialCohort && input.TargetCycle == 0 || input.Epoch4Height == 0 {
		return PoX5Inputs{}, errors.New("frozen PoX5 timing unavailable")
	}
	return input, nil
}
