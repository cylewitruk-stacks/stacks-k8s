//go:build integration

package bitcoincontrol

import (
	"testing"

	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// TestBootstrapReceiptAPIOrdering exercises independent selection/receipt status writes.
func TestBootstrapReceiptAPIOrdering(t *testing.T) {
	c := controlIntegrationClient(t)
	ctx := t.Context()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	f := bootstrapReceiptFixture(t)
	must(c.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "test"}}))
	root := f.root.DeepCopy()
	root.UID, root.ResourceVersion = "", ""
	root.Status = api.StacksNetworkStatus{}
	for i := range root.Spec.Participants {
		root.Spec.Participants[i].Definition = api.Definition{Ref: &common.NameRef{Name: "definition"}}
	}
	must(c.Create(ctx, root))
	initial := f.readInitial(t)
	initial.UID, initial.ResourceVersion = "", ""
	initial.Spec.NetworkUID = root.UID
	initial.OwnerReferences[0].UID = root.UID
	initial.Status = bitcoin.BitcoinInitializationStatus{}
	must(c.Create(ctx, initial))
	execution := f.readRecord(t)
	receipt := execution.Status.LastReceipt.DeepCopy()
	saved := execution.Status.DeepCopy()
	execution.UID, execution.ResourceVersion = "", ""
	execution.Spec.NetworkUID = root.UID
	execution.OwnerReferences[0].UID = root.UID
	execution.Spec.Offer = nil
	execution.Status = bitcoin.BitcoinExecutionStatus{}
	must(c.Create(ctx, execution))
	receipt.Request.Reservation = binding("BitcoinInitialization", initial)
	receipt.Request.Offer.Initialization = receipt.Request.Reservation
	// Selection expires/replaces independently, before the older receipt arrives.
	initial.Status.Offer = receipt.Request.Offer.DeepCopy()
	initial.Status.Offer.Number = 9
	must(c.Status().Update(ctx, initial))
	saved.LastReceipt = receipt
	execution.Status = *saved
	must(c.Status().Update(ctx, execution))
	must(c.Get(ctx, client.ObjectKeyFromObject(initial), initial))
	must(c.Get(ctx, client.ObjectKeyFromObject(execution), execution))
	if !accountBootstrapReceipt(root, initial, execution) {
		t.Fatal("late receipt ignored")
	}
	must(c.Status().Update(ctx, initial))
	must(c.Get(ctx, client.ObjectKeyFromObject(initial), initial))
	if initial.Status.LastAccountedOffer != 1 || initial.Status.Offer.Number != 9 || len(initial.Status.Funded) != 1 ||
		initial.Status.Funded[0].Outputs != 1 ||
		!generationAccounted(initial, execution) ||
		accountBootstrapReceipt(root, initial, execution) {
		t.Fatal("persisted receipt/cursor differ", initial.Status)
	}
}
