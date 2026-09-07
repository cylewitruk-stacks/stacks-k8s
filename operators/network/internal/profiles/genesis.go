package profiles

import (
	"fmt"
	"sort"

	network "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha1"
)

// DefaultGenesis returns an independent copy of the qualified PoX-4 regtest schedule.
// Epoch 4.0 activation remains deferred pending sBTC contract provisioning.
func DefaultGenesis() network.GenesisSpec {
	names := []string{"1.0", "2.0", "2.05", "2.1", "2.2", "2.3", "2.4", "2.5", "3.0", "3.1", "3.2", "3.3", "3.4", "4.0"}
	heights := []int64{0, 0, 203, 204, 206, 207, 208, 209, 223, 224, 225, 226, 227, 1000005}
	testGenesis := true
	result := network.GenesisSpec{UseTestGenesisChainstate: &testGenesis, PoX: &network.GenesisPoX{PrepareLength: 5, RewardCycleLength: 20}}
	for i, name := range names {
		result.Epochs = append(result.Epochs, network.GenesisEpoch{Name: name, StartHeight: heights[i]})
	}
	return result
}

// ResolveGenesis fills omitted protocol defaults, validates shared values and sorts balances.
func ResolveGenesis(value *network.GenesisSpec) (network.GenesisSpec, error) {
	result := DefaultGenesis()
	if value != nil {
		if value.UseTestGenesisChainstate != nil {
			enabled := *value.UseTestGenesisChainstate
			result.UseTestGenesisChainstate = &enabled
		}
		result.Balances = append([]network.GenesisBalance(nil), value.Balances...)
		if len(value.Epochs) > 0 {
			result.Epochs = append([]network.GenesisEpoch(nil), value.Epochs...)
		}
		if value.PoX != nil {
			pox := *value.PoX
			result.PoX = &pox
		}
	}
	expected := DefaultGenesis().Epochs
	if len(result.Epochs) != len(expected) {
		return result, fmt.Errorf("genesis requires the complete supported epoch schedule")
	}
	for i, epoch := range result.Epochs {
		if epoch.Name != expected[i].Name || epoch.StartHeight < 0 || (i > 0 && epoch.StartHeight < result.Epochs[i-1].StartHeight) {
			return result, fmt.Errorf("epoch schedule must contain ordered supported epochs and nondecreasing heights")
		}
	}
	if result.Epochs[0].StartHeight != 0 || result.Epochs[1].StartHeight != 0 {
		return result, fmt.Errorf("regtest epochs 1.0 and 2.0 must start at height zero")
	}
	if result.PoX.PrepareLength < 1 || result.PoX.RewardCycleLength <= result.PoX.PrepareLength || result.PoX.RewardCycleLength > 100000 {
		return result, fmt.Errorf("invalid PoX cycle lengths")
	}
	if len(result.Balances) > 1000 {
		return result, fmt.Errorf("genesis supports at most 1000 allocations")
	}
	names, addresses := map[string]bool{}, map[string]bool{}
	for _, account := range result.Balances {
		if account.Address == "" || len(account.Address) > 64 || account.Amount < 1 || addresses[account.Address] || (account.Name != "" && names[account.Name]) {
			return result, fmt.Errorf("genesis requires unique addresses/account names and positive balances")
		}
		addresses[account.Address] = true
		names[account.Name] = true
	}
	sort.Slice(result.Balances, func(i, j int) bool { return result.Balances[i].Address < result.Balances[j].Address })
	return result, nil
}
