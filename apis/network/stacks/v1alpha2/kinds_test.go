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
		{KindStacksAccount, "StacksAccount", &StacksAccount{}},
		{KindStacksAccountList, "StacksAccountList", &StacksAccountList{}},
		{KindStacksContractSet, "StacksContractSet", &StacksContractSet{}},
		{KindStacksContractSetList, "StacksContractSetList", &StacksContractSetList{}},
		{KindStacksFaucet, "StacksFaucet", &StacksFaucet{}},
		{KindStacksFaucetList, "StacksFaucetList", &StacksFaucetList{}},
		{KindStacksFaucetRequest, "StacksFaucetRequest", &StacksFaucetRequest{}},
		{KindStacksFaucetRequestList, "StacksFaucetRequestList", &StacksFaucetRequestList{}},
		{KindStacksNode, "StacksNode", &StacksNode{}},
		{KindStacksNodeList, "StacksNodeList", &StacksNodeList{}},
		{KindStacksSigner, "StacksSigner", &StacksSigner{}},
		{KindStacksSignerList, "StacksSignerList", &StacksSignerList{}},
		{KindStacksStacker, "StacksStacker", &StacksStacker{}},
		{KindStacksStackerList, "StacksStackerList", &StacksStackerList{}},
		{KindStacksTransactionProduction, "StacksTransactionProduction", &StacksTransactionProduction{}},
		{KindStacksTransactionProductionList, "StacksTransactionProductionList", &StacksTransactionProductionList{}},
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
