package ledgerlifecycle

import (
	"context"
	"errors"
	"testing"

	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha1"
	stacks "github.com/cylewitruk-stacks/stacks-k8s/apis/network/stacks/v1alpha1"
	network "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// failedParentReader simulates unavailable identity evidence rather than parent absence.
type failedParentReader struct{ client.Reader }

func (r failedParentReader) Get(ctx context.Context, key client.ObjectKey, o client.Object, opts ...client.GetOption) error {
	if _, ok := o.(*network.StacksNetwork); ok {
		return errors.New("temporary API failure")
	}
	return r.Reader.Get(ctx, key, o, opts...)
}

func TestRetirementRequiresCurrentParentEvidence(t *testing.T) {
	for _, kind := range []Kind{Policy, Target, Transfer} {
		for _, state := range []string{"live", "missing", "replacement", "unavailable"} {
			t.Run(string(kind)+"/"+state, func(t *testing.T) {
				scheme := runtime.NewScheme()
				_ = bitcoin.AddToScheme(scheme)
				_ = stacks.AddToScheme(scheme)
				_ = network.AddToScheme(scheme)
				parent := &network.StacksNetwork{ObjectMeta: metav1.ObjectMeta{Name: "network", Namespace: "ns", UID: "parent"}}
				finalizer := TransferFinalizer
				var object client.Object = &stacks.StacksTransactionProduction{ObjectMeta: metav1.ObjectMeta{Name: "ledger", Namespace: "ns", UID: "ledger"}, Spec: stacks.StacksTransactionProductionSpec{NetworkName: "network", NetworkUID: "parent"}}
				if kind == Target {
					finalizer = BitcoinFinalizer
					object = &bitcoin.BitcoinProductionTarget{ObjectMeta: metav1.ObjectMeta{Name: "ledger", Namespace: "ns", UID: "ledger"}, Spec: bitcoin.BitcoinProductionTargetSpec{NetworkName: "network", NetworkUID: "parent"}}
				}
				if kind == Policy {
					finalizer = PolicyFinalizer
					object = &bitcoin.BitcoinBlockProduction{ObjectMeta: metav1.ObjectMeta{Name: "ledger", Namespace: "ns", UID: "ledger"}, Spec: bitcoin.BitcoinBlockProductionSpec{NetworkName: "network", NetworkUID: "parent"}}
				}
				object.SetFinalizers([]string{finalizer})
				objects := []client.Object{object}
				if state != "missing" {
					if state == "replacement" {
						parent.UID = "new-parent"
					}
					objects = append(objects, parent)
				}
				c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).WithStatusSubresource(object).Build()
				var reader client.Reader = c
				if state == "unavailable" {
					reader = failedParentReader{c}
				}
				r := &Reconciler{Client: c, Reader: reader, Kind: kind}
				_, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(object)})
				if (err != nil) != (state == "unavailable") {
					t.Fatalf("unexpected error: %v", err)
				}
				if err := c.Get(context.Background(), client.ObjectKeyFromObject(object), object); err != nil {
					t.Fatal(err)
				}
				retained := state == "live" || state == "unavailable"
				if (len(object.GetFinalizers()) == 1) != retained {
					t.Fatal("retention did not follow parent evidence")
				}
			})
		}
	}
}
