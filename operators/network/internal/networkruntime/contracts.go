package networkruntime

import (
	"slices"
	"time"

	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/libs/stacks/identity"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/foundation"
)

// contractCohortSatisfied accepts only the exact frozen bundle and native registry postconditions.
func contractCohortSatisfied(root *api.StacksNetwork, g *api.StacksGenesis, participants []api.StacksNetworkParticipant, now time.Time) bool {
	found := false
	for _, requirement := range g.Spec.Bootstrap.Requirements {
		if requirement.Kind != api.ParticipantStacksContractSet {
			continue
		}
		found = true
		p := requiredWorker(root, requirement, participants)
		if p == nil || requirement.RegistryInitialization == nil {
			return false
		}
		o := p.Status.Execution.Contracts
		if !contractObservationMatches(o, g, requirement, now) {
			return false
		}
	}
	return found
}

// contractObservationMatches checks public inputs; the worker owns native source/registry reads.
func contractObservationMatches(o *api.ContractSetObservation, g *api.StacksGenesis, requirement api.BootstrapRequirement, now time.Time) bool {
	if o == nil || !o.Complete || !fresh(o.ObservedAt, now) || o.Deployer != g.Spec.Chain.Contracts.Deployer || o.Bundle != g.Spec.Chain.Contracts.Bundle || o.SourceDigest != foundation.Digest(g.Spec.Chain.Contracts.SourceHashes) {
		return false
	}
	registry := requirement.RegistryInitialization
	if registry == nil || registry.Mode != "ExplicitTestRegistry" || o.Threshold != uint64(registry.Threshold) {
		return false
	}
	keys := make([]string, 0, len(registry.SignerAccountRefs))
	for _, ref := range registry.SignerAccountRefs {
		_, key := capturedAccount(requirement, ref.Name)
		if key == "" {
			return false
		}
		keys = append(keys, key)
	}
	_, aggregate := capturedAccount(requirement, registry.AggregateKeyAccountRef.Name)
	version, _, err := identity.DecodeAddress(o.SignerPrincipal)
	return aggregate != "" && aggregate == o.AggregatePublicKey && slices.Equal(keys, o.SignerPublicKeys) && err == nil && version == 21
}
