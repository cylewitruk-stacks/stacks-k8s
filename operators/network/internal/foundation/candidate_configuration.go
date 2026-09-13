package foundation

import (
	"context"
	"fmt"
	"sort"

	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/participantworkload/bitcoinconfig"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/participantworkload/stacksconfig"
	"k8s.io/utils/ptr"
)

// CandidateConfiguration contains public desired inputs, never activation authority.
type CandidateConfiguration struct {
	// Root supplies the selected participant UID inventory and desired controls.
	Root *api.StacksNetwork
	// Genesis is the candidate or previously frozen chain, without a fabricated object UID.
	Genesis api.StacksGenesisSpec
	// Participants contains candidate policies in local Admission values for renderer reuse only.
	Participants map[string]*api.StacksNetworkParticipant
}

// CandidateConfigurationValidator observes scoped rendering and selected-image validation.
type CandidateConfigurationValidator interface {
	// ValidateConfiguration may prepare support resources but must never activate actors.
	ValidateConfiguration(context.Context, CandidateConfiguration, string) (bool, error)
}

// customization selects the actor's explicitly declared escape hatch.
func customization(configuration api.Configuration) *common.Config {
	if configuration.StacksNode != nil {
		return configuration.StacksNode.Config
	}
	if configuration.StacksSigner != nil {
		return configuration.StacksSigner.Config
	}
	if configuration.BitcoinNode != nil {
		return configuration.BitcoinNode.Config
	}
	return nil
}

// unverified excludes intentionally divergent actors from managed prerequisites.
func unverified(configuration api.Configuration) bool {
	config := customization(configuration)
	return config != nil && ptr.Deref(config.Compatibility, common.CompatibilityManaged) == common.CompatibilityUnverified
}

// validateCustomization checks each actor's native public override paths.
func validateCustomization(kind api.ParticipantKind, config *common.Config) error {
	if config == nil {
		return nil
	}
	if kind == api.ParticipantBitcoinNode {
		return bitcoinconfig.ValidateCustomization(config)
	}
	return stacksconfig.ValidateCustomization(kind, config)
}

// candidateConfiguration snapshots exact public inputs without publishing candidate admission.
func candidateConfiguration(root *api.StacksNetwork, genesis api.StacksGenesisSpec, all map[string]*candidate) CandidateConfiguration {
	in := CandidateConfiguration{Root: root.DeepCopy(), Genesis: *genesis.DeepCopy(), Participants: map[string]*api.StacksNetworkParticipant{}}
	for name, c := range all {
		p := c.instance.DeepCopy()
		p.Spec.Configuration = c.configuration
		p.Spec.Source = c.source
		p.Status.Admission = &api.Admission{Source: c.source, Configuration: c.configuration, PolicyDigest: Digest(c.configuration), Dependencies: c.dependencies}
		in.Participants[name] = p
	}
	return in
}

// configurationResults validates customized candidates independently of their admitted predecessors.
func (r *Reconciler) configurationResults(ctx context.Context, root *api.StacksNetwork, all map[string]*candidate, frozen *api.StacksGenesis, complete bool) map[string]error {
	results := map[string]error{}
	names := []string{}
	for name, c := range all {
		if customization(c.configuration) != nil {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return results
	}
	sort.Strings(names)
	var genesis api.StacksGenesisSpec
	var err error
	if !complete {
		err = fmt.Errorf("complete public candidate inputs are required for configuration validation")
	} else if r.Configurations == nil {
		err = fmt.Errorf("native configuration validation is unavailable")
	} else if frozen != nil {
		genesis = frozen.Spec
	} else {
		genesis, err = compileGenesis(ctx, r.Reader, root, all)
	}
	if err != nil {
		for _, name := range names {
			results[name] = err
		}
		return results
	}
	in := candidateConfiguration(root, genesis, all)
	for _, name := range names {
		ready, err := r.Configurations.ValidateConfiguration(ctx, in, name)
		if err != nil {
			results[name] = err
		} else if !ready {
			results[name] = fmt.Errorf("scoped configuration validation is pending")
		}
	}
	return results
}
