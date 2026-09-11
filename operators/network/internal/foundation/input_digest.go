package foundation

import (
	"sort"
	"strings"

	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
)

// semanticBinding excludes Kubernetes allocation identity from reviewed inputs.
type semanticBinding struct {
	Kind        string `json:"kind"`
	Name        string `json:"name"`
	Fingerprint string `json:"fingerprint,omitempty"`
}

// semanticParticipant identifies compiled public policy by logical participant name.
type semanticParticipant struct {
	Name          string              `json:"name"`
	Kind          api.ParticipantKind `json:"kind"`
	Definition    string              `json:"definition,omitempty"`
	Configuration map[string]any      `json:"configuration"`
	Dependencies  []semanticBinding   `json:"dependencies"`
}

// semanticInputDigest keeps runtime UIDs in provenance, outside the input contract.
func semanticInputDigest(spec api.StacksGenesisSpec, all map[string]*candidate) string {
	names := make([]string, 0, len(all))
	participantNames := map[string]string{}
	accountNames := map[string]string{}
	for name, c := range all {
		names = append(names, name)
		participantNames[c.instance.Name] = name
		for _, a := range c.accounts {
			if ownedUID(a, c.instance.UID) {
				// Inline default accounts inherit an allocated participant name. Their
				// semantic name instead identifies the logical owner and default role.
				accountNames[a.Name] = "participant:" + name + ":default-account"
			}
		}
	}
	sort.Strings(names)
	logicalName := func(kind, name string) string {
		aliases := accountNames
		if kind == "StacksNetworkParticipant" {
			aliases = participantNames
		} else if kind != "StacksAccount" {
			return name
		}
		if logical, ok := aliases[name]; ok {
			return logical
		}
		return name
	}
	bindings := func(inputs []common.Binding) []semanticBinding {
		result := make([]semanticBinding, 0, len(inputs))
		for _, b := range inputs {
			result = append(result, semanticBinding{Kind: b.Kind, Name: logicalName(b.Kind, b.Name), Fingerprint: b.Fingerprint})
		}
		sort.Slice(result, func(i, j int) bool { return result[i].Kind+"/"+result[i].Name < result[j].Kind+"/"+result[j].Name })
		return result
	}
	// Only account references can contain generated names in compiled configuration.
	// Literal image, placement and other strings retain their exact meaning.
	normalizeRef := func(value any) {
		if ref, ok := value.(map[string]any); ok {
			if name, ok := ref["name"].(string); ok {
				ref["name"] = logicalName("StacksAccount", name)
			}
		}
	}
	var normalize func(any)
	normalize = func(value any) {
		switch v := value.(type) {
		case map[string]any:
			for key, child := range v {
				switch {
				case key == "accountRef" || strings.HasSuffix(key, "AccountRef"):
					normalizeRef(child)
				case strings.HasSuffix(key, "AccountRefs"):
					if refs, ok := child.([]any); ok {
						for _, ref := range refs {
							normalizeRef(ref)
						}
					}
				default:
					normalize(child)
				}
			}
		case []any:
			for _, child := range v {
				normalize(child)
			}
		}
	}
	participants := make([]semanticParticipant, 0, len(names))
	for _, name := range names {
		c := all[name]
		configuration, _ := objectMap(c.configuration)
		normalize(configuration)
		participants = append(participants, semanticParticipant{Name: name, Kind: c.instance.Spec.Kind, Definition: c.source.Name, Configuration: configuration, Dependencies: bindings(c.dependencies)})
	}
	return Digest(struct {
		Chain        api.Chain             `json:"chain"`
		Participants []semanticParticipant `json:"participants"`
		Dependencies []semanticBinding     `json:"dependencies"`
	}{spec.Chain, participants, bindings(spec.Source.Dependencies)})
}
