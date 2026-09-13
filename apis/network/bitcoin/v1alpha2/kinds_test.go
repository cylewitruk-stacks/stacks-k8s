package v1alpha2

import (
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
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
		{KindBitcoinBlockProduction, "BitcoinBlockProduction", &BitcoinBlockProduction{}},
		{KindBitcoinBlockProductionList, "BitcoinBlockProductionList", &BitcoinBlockProductionList{}},
		{KindBitcoinBlockSchedule, "BitcoinBlockSchedule", &BitcoinBlockSchedule{}},
		{KindBitcoinBlockScheduleList, "BitcoinBlockScheduleList", &BitcoinBlockScheduleList{}},
		{KindBitcoinBlockScheduleOverride, "BitcoinBlockScheduleOverride", &BitcoinBlockScheduleOverride{}},
		{KindBitcoinBlockScheduleOverrideList, "BitcoinBlockScheduleOverrideList", &BitcoinBlockScheduleOverrideList{}},
		{KindBitcoinExecution, "BitcoinExecution", &BitcoinExecution{}},
		{KindBitcoinExecutionList, "BitcoinExecutionList", &BitcoinExecutionList{}},
		{KindBitcoinInitialization, "BitcoinInitialization", &BitcoinInitialization{}},
		{KindBitcoinInitializationList, "BitcoinInitializationList", &BitcoinInitializationList{}},
		{KindBitcoinNode, "BitcoinNode", &BitcoinNode{}},
		{KindBitcoinNodeList, "BitcoinNodeList", &BitcoinNodeList{}},
		{KindBitcoinWallet, "BitcoinWallet", &BitcoinWallet{}},
		{KindBitcoinWalletList, "BitcoinWalletList", &BitcoinWalletList{}},
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
