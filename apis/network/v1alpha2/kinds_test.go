package v1alpha2

import (
	"k8s.io/apimachinery/pkg/runtime"
	"testing"
)

// TestKindWireAndSchemeIdentity checks names independently against registered concrete types.
func TestKindWireAndSchemeIdentity(t *testing.T) {
	s := runtime.NewScheme()
	if err := AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		kind, wire string
		object     runtime.Object
	}{
		{KindStacksEpochSchedule, "StacksEpochSchedule", &StacksEpochSchedule{}},
		{KindStacksEpochScheduleList, "StacksEpochScheduleList", &StacksEpochScheduleList{}},
		{KindStacksGenesis, "StacksGenesis", &StacksGenesis{}},
		{KindStacksGenesisList, "StacksGenesisList", &StacksGenesisList{}},
		{KindStacksNetwork, "StacksNetwork", &StacksNetwork{}},
		{KindStacksNetworkList, "StacksNetworkList", &StacksNetworkList{}},
		{KindStacksNetworkParticipant, "StacksNetworkParticipant", &StacksNetworkParticipant{}},
		{KindStacksNetworkParticipantList, "StacksNetworkParticipantList", &StacksNetworkParticipantList{}},
	} {
		t.Run(tc.wire, func(t *testing.T) {
			if tc.kind != tc.wire {
				t.Fatalf("kind %q, want %q", tc.kind, tc.wire)
			}
			gvks, _, err := s.ObjectKinds(tc.object)
			if err != nil {
				t.Fatal(err)
			}
			if len(gvks) != 1 || gvks[0].Kind != tc.kind {
				t.Fatalf("%T registered as %v, want %s", tc.object, gvks, tc.kind)
			}
		})
	}
}
