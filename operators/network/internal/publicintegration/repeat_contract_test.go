//go:build live

package publicintegration

import (
	"testing"

	"k8s.io/apimachinery/pkg/types"
)

// TestNetworkRepeatRejectsRetainedRuntime prevents declaration reuse from disguising data reuse.
func TestNetworkRepeatRejectsRetainedRuntime(t *testing.T) {
	epoch := func(prefix string) networkEpoch {
		return networkEpoch{Root: types.UID(prefix + "root"), Genesis: types.UID(prefix + "genesis"), Chain: "chain", Participants: map[string]types.UID{"node": types.UID(prefix + "participant")}, Pods: map[types.UID]bool{types.UID(prefix + "pod"): true}, Claims: map[types.UID]bool{types.UID(prefix + "claim"): true}, Volumes: map[string]bool{prefix + "volume": true}}
	}
	old := epoch("old-")
	if err := independentNetworkEpoch(old, epoch("new-")); err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(*networkEpoch){
		"root":                func(e *networkEpoch) { e.Root = old.Root },
		"genesis":             func(e *networkEpoch) { e.Genesis = old.Genesis },
		"chain":               func(e *networkEpoch) { e.Chain = "different" },
		"participant":         func(e *networkEpoch) { e.Participants["node"] = old.Participants["node"] },
		"missing participant": func(e *networkEpoch) { delete(e.Participants, "node") },
		"pod":                 func(e *networkEpoch) { e.Pods = map[types.UID]bool{"old-pod": true} },
		"claim":               func(e *networkEpoch) { e.Claims = map[types.UID]bool{"old-claim": true} },
		"volume":              func(e *networkEpoch) { e.Volumes = map[string]bool{"old-volume": true} },
		"no runtime":          func(e *networkEpoch) { e.Pods = nil },
	} {
		t.Run(name, func(t *testing.T) {
			next := epoch("new-")
			change(&next)
			if independentNetworkEpoch(old, next) == nil {
				t.Fatal("invalid repeated experiment accepted")
			}
		})
	}
}
