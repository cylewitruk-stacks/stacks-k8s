package accountledger

import (
	"context"
	stacks "github.com/cylewitruk-stacks/stacks-k8s/apis/network/stacks/v1alpha1"
	network "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"strings"
	"testing"
)

// TestAccountAdmissionRejectsWithdrawnAndReassignedAuthority exercises the real authority checks before ingress discovery.
func TestAccountAdmissionRejectsWithdrawnAndReassignedAuthority(t *testing.T) {
	for _, mode := range []string{"parent-replaced", "account-unpinned", "consumer-replaced", "paused", "withdrawn", "account-reassigned", "consumer-paused"} {
		t.Run(mode, func(t *testing.T) {
			scheme := runtime.NewScheme()
			if err := network.AddToScheme(scheme); err != nil {
				t.Fatal(err)
			}
			if err := stacks.AddToScheme(scheme); err != nil {
				t.Fatal(err)
			}
			policy := stacks.ManagedAccountPolicy{Name: "holder", ConsumerKind: "StacksContractSet", Consumer: "set", Address: "original"}
			setPolicy := stacks.ContractSetPolicy{Name: "set", Account: "holder"}
			parent := &network.StacksNetwork{ObjectMeta: metav1.ObjectMeta{Name: "n", Namespace: "test", UID: "parent"}, Spec: network.StacksNetworkSpec{Operation: &network.NetworkOperation{Accounts: []stacks.ManagedAccountPolicy{policy}, ContractSets: []stacks.ContractSetPolicy{setPolicy}}}}
			owners := []metav1.OwnerReference{*metav1.NewControllerRef(parent, network.GroupVersion.WithKind("StacksNetwork"))}
			account := &stacks.StacksAccount{ObjectMeta: metav1.ObjectMeta{Name: "n-account-holder", Namespace: "test", UID: "account", OwnerReferences: owners}, Spec: stacks.StacksAccountSpec{NetworkName: parent.Name, NetworkUID: string(parent.UID), Policy: policy}}
			consumer := &stacks.StacksContractSet{ObjectMeta: metav1.ObjectMeta{Name: "n-contracts-set", Namespace: "test", UID: "consumer", OwnerReferences: owners}, Spec: stacks.StacksContractSetSpec{NetworkName: parent.Name, NetworkUID: string(parent.UID), Policy: setPolicy}}
			parent.Status.Capabilities = []network.CapabilityIdentity{{Kind: "StacksAccount", Name: account.Name, UID: string(account.UID)}, {Kind: "StacksContractSet", Name: consumer.Name, UID: string(consumer.UID)}}
			want := ""
			switch mode {
			case "parent-replaced":
				parent.UID = "other"
				want = "not pinned"
			case "account-unpinned":
				parent.Status.Capabilities[0].UID = "other"
				want = "not pinned"
			case "consumer-replaced":
				consumer.UID = "other"
				want = "consumer identity changed"
			case "paused":
				parent.Spec.Operation.Paused = true
				want = "paused or withdrawn"
			case "withdrawn":
				parent.Spec.Operation = nil
				want = "paused or withdrawn"
			case "account-reassigned":
				parent.Spec.Operation.Accounts[0].Address = "other"
				want = "declaration is not current"
			case "consumer-paused":
				consumer.Spec.Policy.Paused = true
				want = "declaration is not current"
			}
			c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(parent, account, consumer).Build()
			if err := c.Get(context.Background(), client.ObjectKeyFromObject(account), account); err != nil {
				t.Fatal(err)
			}
			_, err := (Admitter{Reader: c}).Admit(context.Background(), account, "consumer", true)
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("authority refused at wrong boundary: %v", err)
			}
		})
	}
}
