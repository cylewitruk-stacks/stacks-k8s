package foundation

import (
	"fmt"
	"sort"

	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
)

// topologyConflicts applies permanent attachment constraints independently of genesis.
// Existing admissions keep their attachment; new contenders are considered by name.
func topologyConflicts(
	all map[string]*candidate,
	instances map[string]*api.StacksNetworkParticipant,
	invalid map[string]bool,
) map[string]string {
	claims := map[string]string{}
	for name, p := range instances {
		if prior := p.Status.Admission; prior != nil && prior.Configuration.StacksSigner != nil {
			if ref := prior.Configuration.StacksSigner.NodeRef; ref != nil {
				claims[ref.Name] = name
			}
		}
	}
	names := make([]string, 0, len(all))
	for name := range all {
		names = append(names, name)
	}
	sort.Strings(names)
	conflicts := map[string]string{}
	for _, name := range names {
		c := all[name]
		signer := c.configuration.StacksSigner
		if invalid[name] || signer == nil || signer.NodeRef == nil || c.instance.Status.Admission != nil {
			continue
		}
		if owner, ok := claims[signer.NodeRef.Name]; ok {
			conflicts[name] = fmt.Sprintf("node %s already has signer participant %s", signer.NodeRef.Name, owner)
		} else {
			claims[signer.NodeRef.Name] = name
		}
	}
	return conflicts
}
