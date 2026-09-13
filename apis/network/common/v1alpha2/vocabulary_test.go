package v1alpha2

import (
	"encoding/json"
	"testing"
)

// TestVocabularyWireValues pins serialized contracts independently of controller call sites.
func TestVocabularyWireValues(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value any
		wire  string
	}{
		{"CompatibilityManaged", CompatibilityManaged, `"Managed"`},
		{"CompatibilityUnverified", CompatibilityUnverified, `"Unverified"`},
		{"DiscoveryNetwork", DiscoveryNetwork, `"Network"`},
		{"ConditionResolved", ConditionResolved, `"Resolved"`},
		{"ReasonContainerStatusUnknown", ReasonContainerStatusUnknown, `"ContainerStatusUnknown"`},
		{"ReasonNodeLost", ReasonNodeLost, `"NodeLost"`},
		{"EndpointRPC", EndpointRPC, `"rpc"`},
		{"EndpointP2P", EndpointP2P, `"p2p"`},
		{"EndpointEvents", EndpointEvents, `"events"`},
		{"BitcoinActorRPCUsername", BitcoinActorRPCUsername, `"actor"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := json.Marshal(tc.value)
			if err != nil {
				t.Fatal(err)
			}
			if string(raw) != tc.wire {
				t.Fatalf("wire value %s, want %s", raw, tc.wire)
			}
		})
	}
}
