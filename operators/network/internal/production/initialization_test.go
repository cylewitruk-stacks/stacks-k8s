package production

import (
	"context"
	"testing"

	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha1"
	stacks "github.com/cylewitruk-stacks/stacks-k8s/apis/network/stacks/v1alpha1"
	network "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha1"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/naming"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/profiles"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// TestProtocolStartupAcknowledgementsDoNotBecomeHealthGates pins the startup-only boundary.
func TestProtocolStartupAcknowledgementsDoNotBecomeHealthGates(t *testing.T) {
	ctx := context.Background()
	scheme := runtime.NewScheme()
	for _, add := range []func(*runtime.Scheme) error{network.AddToScheme, bitcoin.AddToScheme, stacks.AddToScheme} {
		if err := add(scheme); err != nil {
			t.Fatal(err)
		}
	}
	genesis := profiles.DefaultGenesis()
	genesis.Epochs[len(genesis.Epochs)-1].StartHeight = 244
	n := &network.StacksNetwork{ObjectMeta: metav1.ObjectMeta{Name: "n", Namespace: "test", UID: "network"}, Spec: network.StacksNetworkSpec{Genesis: &genesis, Operation: &network.NetworkOperation{Participants: []stacks.StackingPolicy{{Name: "p", HolderAccount: "holder"}}, ContractSets: []stacks.ContractSetPolicy{{Name: "sbtc"}}}}}
	target := &bitcoin.BitcoinProductionTarget{ObjectMeta: metav1.ObjectMeta{Name: "target", Namespace: n.Namespace}}
	participant := &stacks.StacksStackingParticipant{ObjectMeta: metav1.ObjectMeta{Name: naming.Child(n.Name, "stacking-p"), Namespace: n.Namespace, UID: "participant", Generation: 1}, Spec: stacks.StacksStackingParticipantSpec{NetworkUID: string(n.UID), Policy: n.Spec.Operation.Participants[0]}, Status: stacks.ManagedOperationStatus{ObservedGeneration: 1}}
	account := &stacks.StacksAccount{ObjectMeta: metav1.ObjectMeta{Name: naming.Child(n.Name, "account-holder"), Namespace: n.Namespace, UID: "account"}}
	set := &stacks.StacksContractSet{ObjectMeta: metav1.ObjectMeta{Name: naming.Child(n.Name, "contracts-sbtc"), Namespace: n.Namespace, UID: "set", Generation: 1}, Spec: stacks.StacksContractSetSpec{NetworkUID: string(n.UID), Policy: n.Spec.Operation.ContractSets[0]}, Status: stacks.ManagedOperationStatus{ObservedGeneration: 1}}
	for kind, obj := range map[string]client.Object{"StacksStackingParticipant": participant, "StacksAccount": account, "StacksContractSet": set} {
		if err := controllerutil.SetControllerReference(n, obj, scheme); err != nil {
			t.Fatal(err)
		}
		n.Status.Capabilities = append(n.Status.Capabilities, network.CapabilityIdentity{Kind: kind, Name: obj.GetName(), UID: string(obj.GetUID())})
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(target, participant, account, set).WithObjects(n, target, participant, account, set).Build()
	r := &Reconciler{Client: c, APIReader: c}
	gate := func(height int64, wantError bool) {
		t.Helper()
		if err := r.protocolGate(ctx, target, n, height); (err != nil) != wantError {
			t.Fatalf("height %d: %v", height, err)
		}
	}
	gate(209, false) // Core activates PoX-4 strictly after this height.
	gate(210, true)
	account.Status.Transaction = &stacks.AccountTransaction{ConsumerUID: string(participant.UID)}
	if err := c.Status().Update(ctx, account); err != nil {
		t.Fatal(err)
	}
	gate(210, false) // A durably authorized transaction needs a legacy confirmation block.
	if target.Status.ProtocolStage != "" {
		t.Fatal("pending transaction prematurely completed enrollment")
	}
	gate(214, true) // Do not advance an unconfirmed enrollment into the prepare phase.
	participant.Status.Phase = "Ready"
	participant.Status.Protocol = "ST000000000000000000002AMW42H.pox-4"
	participant.Status.UnlockHeight = 460
	if err := c.Status().Update(ctx, participant); err != nil {
		t.Fatal(err)
	}
	gate(214, false)
	if target.Status.ProtocolStage != "PoX4" {
		t.Fatal("enrollment was not durably acknowledged")
	}
	participant.Status.Phase = "Waiting"
	if err := c.Status().Update(ctx, participant); err != nil {
		t.Fatal(err)
	}
	gate(215, false)
	gate(230, true)
	set.Status.Phase = "Ready"
	if err := c.Status().Update(ctx, set); err != nil {
		t.Fatal(err)
	}
	gate(230, false)
	if target.Status.ProtocolStage != "Contracts" {
		t.Fatal("contract prerequisite not retained")
	}
	if err := c.Delete(ctx, set); err != nil {
		t.Fatal(err)
	}
	gate(245, false)
	gate(246, true)
	participant.Status.Phase = "Ready"
	participant.Status.Protocol = "ST000000000000000000002AMW42H.pox-5"
	participant.Status.UnlockHeight = 500
	if err := c.Status().Update(ctx, participant); err != nil {
		t.Fatal(err)
	}
	gate(246, false)
	// Restarts, degradation, removal, reorganization, and operation pause do not revoke completed startup.
	persisted := &bitcoin.BitcoinProductionTarget{}
	if err := c.Get(ctx, client.ObjectKeyFromObject(target), persisted); err != nil {
		t.Fatal(err)
	}
	target = persisted
	if err := c.Delete(ctx, participant); err != nil {
		t.Fatal(err)
	}
	n.Spec.Operation.Paused = true
	gate(600, false)
	gate(210, false)
	if target.Status.ProtocolStage != "PoX5" {
		t.Fatal("startup stage regressed")
	}
}
