package foundation

import (
	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
)

// sameBoundPlacement preserves the support Pod's placement after exact session binding.
func sameBoundPlacement(root *api.StacksNetwork, p *api.StacksNetworkParticipant, next api.Configuration) bool {
	id := findIdentity(root.Status.Identities, p.Spec.ParticipantName)
	if id == nil || id.Worker == nil || p.Status.Admission == nil {
		return true
	}
	return equal(workerPlacement(p.Status.Admission.Configuration), workerPlacement(next))
}

// workerPlacement selects the effective compiled placement for a management role.
func workerPlacement(c api.Configuration) *common.Placement {
	switch {
	case c.StacksStacker != nil:
		return c.StacksStacker.WorkerPlacement
	case c.StacksFaucet != nil:
		return c.StacksFaucet.WorkerPlacement
	case c.StacksContractSet != nil:
		return c.StacksContractSet.WorkerPlacement
	case c.StacksTransactionProduction != nil:
		return c.StacksTransactionProduction.WorkerPlacement
	default:
		return nil
	}
}
