package foundation

import (
	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"k8s.io/utils/ptr"
)

// BootstrapPolicyCompatible compares captured public requirements, excluding mutable runtime settings.
// Protected account, credential and participant bindings remain subject to ordinary admission checks.
func BootstrapPolicyCompatible(required api.BootstrapRequirement, policy api.Configuration) bool {
	if unverified(policy) {
		return false
	}
	//nolint:exhaustive // Only these kinds carry frozen bootstrap inputs; other policies remain mutable.
	switch required.Kind {
	case api.ParticipantBitcoinNode:
		if policy.BitcoinNode == nil {
			return false
		}
		for _, binding := range required.Dependencies {
			if binding.Kind != bitcoin.KindBitcoinWallet {
				continue
			}
			found := false
			for _, wallet := range ptr.Deref(policy.BitcoinNode.WalletRefs, nil) {
				found = found || wallet.Name == binding.Name
			}
			if !found {
				return false
			}
		}
	case api.ParticipantBitcoinBlockProduction:
		p := policy.BitcoinBlockProduction
		return p != nil && required.BitcoinPayoutWallet != nil && p.PayoutWalletRef != nil &&
			p.PayoutWalletRef.Name == required.BitcoinPayoutWallet.Name &&
			equal(p.Initialization, required.BitcoinInitialization)
	case api.ParticipantStacksNode:
		p := policy.StacksNode
		return p != nil && required.MiningEnabled != nil &&
			ptr.Deref(required.MiningEnabled, false) == (p.Mining != nil && ptr.Deref(p.Mining.Enabled, false))
	case api.ParticipantStacksStacker:
		p := policy.StacksStacker
		return p != nil && equal(p.AmountMicroSTX, required.AmountMicroSTX) &&
			equal(p.LockCycles, required.LockCycles) &&
			equal(p.RenewWhenRemainingCycles, required.RenewWhenRemainingCycles)
	case api.ParticipantStacksContractSet:
		p := policy.StacksContractSet
		return p != nil && equal(p.Initialization, required.RegistryInitialization)
	}
	return true
}

// bootstrapPolicyPending retains only requirements whose last affected frozen gate is unfinished.
func bootstrapPolicyPending(completed map[api.GateName]bool, required api.BootstrapRequirement) bool {
	last := api.GatePrepareWaterfall
	//nolint:exhaustive // Only these kinds carry frozen bootstrap inputs; other policies remain mutable.
	switch required.Kind {
	case api.ParticipantBitcoinNode, api.ParticipantBitcoinBlockProduction:
		last = api.GatePrepareBitcoin
	case api.ParticipantStacksContractSet:
		last = api.GatePreparePoX5
	}
	return !completed[last]
}

// bootstrapCompletedGates verifies progress once per admission pass before releasing captured policies.
func bootstrapCompletedGates(root *api.StacksNetwork, genesis *api.StacksGenesis) map[api.GateName]bool {
	if genesis == nil {
		return nil
	}
	state, ref := root.Status.Initialization, root.Status.GenesisRef
	gates := genesis.Spec.Bootstrap.Gates
	if ref == nil || ref.UID != genesis.UID || ref.Fingerprint != Digest(genesis.Spec) ||
		root.Status.GenesisDigest != Digest(genesis.Spec.Chain) ||
		state == nil ||
		state.GenesisUID != genesis.UID ||
		state.GenesisDigest != root.Status.GenesisDigest ||
		len(gates) == 0 ||
		len(state.Gates) != len(gates) ||
		state.GateIndex < 0 ||
		int(state.GateIndex) > len(gates) ||
		state.Completed != (int(state.GateIndex) == len(gates)) {
		return nil
	}
	completed := map[api.GateName]bool{}
	for i, gate := range gates {
		if state.Gates[i].Name != gate.Name || i > 0 && gate.BitcoinCeiling <= gates[i-1].BitcoinCeiling ||
			(state.Gates[i].CompletedAt != nil) != (i < int(state.GateIndex)) {
			return nil
		}
		completed[gate.Name] = state.Gates[i].CompletedAt != nil
	}
	ceilingIndex := min(int(state.GateIndex), len(gates)-1)
	if state.AuthorizedCeiling != gates[ceilingIndex].BitcoinCeiling {
		return nil
	}
	return completed
}
