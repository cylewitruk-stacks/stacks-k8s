package foundation

import (
	"fmt"
	"testing"

	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	stacks "github.com/cylewitruk-stacks/stacks-k8s/apis/network/stacks/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// TestDefinitionPrototypeIsolation prevents fetched status from becoming later input.
func TestDefinitionPrototypeIsolation(t *testing.T) {
	for _, prototype := range []client.Object{
		&bitcoin.BitcoinNode{}, &bitcoin.BitcoinBlockProduction{}, &bitcoin.BitcoinBlockSchedule{},
		&stacks.StacksNode{}, &stacks.StacksSigner{}, &stacks.StacksStacker{}, &stacks.StacksFaucet{},
		&stacks.StacksContractSet{}, &stacks.StacksTransactionProduction{}, &api.StacksEpochSchedule{},
	} {
		t.Run(fmt.Sprintf("%T", prototype), func(t *testing.T) {
			r := &DefinitionReconciler{Prototype: prototype}
			first, err := r.object()
			if err != nil {
				t.Fatal(err)
			}
			first.SetName("fetched")
			first.SetLabels(map[string]string{"old": "value"})
			first.GetResolutionStatus().Digest = "old-digest"
			next, err := r.object()
			if err != nil {
				t.Fatal(err)
			}
			if next == first || next == prototype || next.GetName() != "" || len(next.GetLabels()) != 0 ||
				next.GetResolutionStatus().Digest != "" {
				t.Fatalf("prototype reused or contaminated: %#v", next)
			}
		})
	}
}

// TestDefinitionPrototypeRejectsUnsupportedResources preserves the registration allowlist.
func TestDefinitionPrototypeRejectsUnsupportedResources(t *testing.T) {
	for _, prototype := range []client.Object{
		nil,
		(*stacks.StacksNode)(nil),
		&corev1.Pod{},
		&stacks.StacksAccount{},
		&api.StacksNetworkParticipant{},
	} {
		r := &DefinitionReconciler{Prototype: prototype}
		if object, err := r.object(); err == nil || object != nil {
			t.Fatalf("accepted %T: %T, %v", prototype, object, err)
		}
	}
}
