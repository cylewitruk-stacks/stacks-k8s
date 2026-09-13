package foundation

import (
	"testing"

	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	stacks "github.com/cylewitruk-stacks/stacks-k8s/apis/network/stacks/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
)

func TestBoundWorkerPlacementRequiresReplacement(t *testing.T) {
	for _, kind := range []api.ParticipantKind{"StacksStacker", "StacksFaucet", "StacksContractSet", "StacksTransactionProduction"} {
		t.Run(string(kind), func(t *testing.T) {
			c := api.Configuration{}
			switch kind {
			case "StacksStacker":
				c.StacksStacker = &stacks.StacksStackerSpec{}
			case "StacksFaucet":
				c.StacksFaucet = &stacks.StacksFaucetSpec{}
			case "StacksContractSet":
				c.StacksContractSet = &stacks.StacksContractSetSpec{}
			case "StacksTransactionProduction":
				c.StacksTransactionProduction = &stacks.StacksTransactionProductionSpec{}
			}
			p := &api.StacksNetworkParticipant{Spec: api.StacksNetworkParticipantSpec{ParticipantName: "worker", Kind: kind}, Status: api.ParticipantStatus{Admission: &api.Admission{Configuration: c}}}
			root := &api.StacksNetwork{Status: api.StacksNetworkStatus{Identities: []api.InstanceIdentity{{Name: "worker"}}}}
			next := c.DeepCopy()
			place := &common.Placement{NodeSelector: map[string]string{"pool": "another"}}
			switch kind {
			case "StacksStacker":
				next.StacksStacker.WorkerPlacement = place
			case "StacksFaucet":
				next.StacksFaucet.WorkerPlacement = place
			case "StacksContractSet":
				next.StacksContractSet.WorkerPlacement = place
			case "StacksTransactionProduction":
				next.StacksTransactionProduction.WorkerPlacement = place
			}
			if !sameBoundPlacement(root, p, *next) {
				t.Fatal("unbound placement was frozen")
			}
			root.Status.Identities[0].Worker = &api.WorkerSession{}
			if sameBoundPlacement(root, p, *next) {
				t.Fatal("bound placement edit was admitted")
			}
			if !sameBoundPlacement(root, p, c) {
				t.Fatal("unchanged placement rejected")
			}
		})
	}
}
