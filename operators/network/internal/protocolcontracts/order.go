package protocolcontracts

import (
	"fmt"
	stacks "github.com/cylewitruk-stacks/stacks-k8s/apis/network/stacks/v1alpha1"
	"sort"
)

// Order determines a stable topological order and rejects cycles before any mutation.
func Order(contracts []stacks.ContractArtifact) ([]stacks.ContractArtifact, error) {
	if len(contracts) == 0 || len(contracts) > 16 {
		return nil, fmt.Errorf("unsupported contract set size")
	}
	byName := map[string]stacks.ContractArtifact{}
	names := []string{}
	for _, c := range contracts {
		if _, duplicate := byName[c.Name]; duplicate || c.Name == "" {
			return nil, fmt.Errorf("duplicate or empty contract name")
		}
		byName[c.Name] = c
		names = append(names, c.Name)
	}
	sort.Strings(names)
	visited, active := map[string]bool{}, map[string]bool{}
	ordered := []stacks.ContractArtifact{}
	var visit func(string) error
	visit = func(name string) error {
		if active[name] {
			return fmt.Errorf("contract dependencies contain a cycle")
		}
		if visited[name] {
			return nil
		}
		c, found := byName[name]
		if !found {
			return fmt.Errorf("contract dependency is absent")
		}
		active[name] = true
		deps := append([]string(nil), c.DependsOn...)
		sort.Strings(deps)
		for _, dep := range deps {
			if err := visit(dep); err != nil {
				return err
			}
		}
		active[name], visited[name] = false, true
		ordered = append(ordered, c)
		return nil
	}
	for _, name := range names {
		if err := visit(name); err != nil {
			return nil, err
		}
	}
	return ordered, nil
}
