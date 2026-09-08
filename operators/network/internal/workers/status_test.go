package workers

import (
	"context"
	"testing"

	stacks "github.com/cylewitruk-stacks/stacks-k8s/apis/network/stacks/v1alpha1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestWorkerConditionPreservesReservedTransactionAndRejectsStaleIdentity(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := stacks.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	ledger := &stacks.StacksTransactionProduction{ObjectMeta: metav1.ObjectMeta{Name: "transfer", Namespace: "experiment", UID: "original", Generation: 7}, Status: stacks.StacksTransactionProductionStatus{Phase: "Ambiguous", Outstanding: true, TxID: "reserved-transaction", Nonce: 19}}
	c := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(ledger).WithObjects(ledger).Build()
	r := Reconciler{Client: c, Reader: c, Scheme: scheme}
	ctx := context.Background()
	if err := r.condition(ctx, ledger, false, "ConfigurationUnavailable", "Credential reference is missing"); err != nil {
		t.Fatal(err)
	}
	current := &stacks.StacksTransactionProduction{}
	if err := c.Get(ctx, client.ObjectKeyFromObject(ledger), current); err != nil {
		t.Fatal(err)
	}
	condition := meta.FindStatusCondition(current.Status.Conditions, "WorkerReady")
	if condition == nil || condition.Status != metav1.ConditionFalse || condition.ObservedGeneration != 7 {
		t.Fatal("worker availability was not recorded")
	}
	if !current.Status.Outstanding || current.Status.Phase != "Ambiguous" || current.Status.TxID != "reserved-transaction" || current.Status.Nonce != 19 {
		t.Fatal("workload status overwrote execution evidence")
	}
	version := current.ResourceVersion
	if err := r.condition(ctx, ledger, false, "ConfigurationUnavailable", "Credential reference is missing"); err != nil {
		t.Fatal(err)
	}
	if err := c.Get(ctx, client.ObjectKeyFromObject(ledger), current); err != nil {
		t.Fatal(err)
	}
	if current.ResourceVersion != version {
		t.Fatal("unchanged worker condition caused a write")
	}
	stale := ledger.DeepCopy()
	stale.UID = "replaced"
	if err := r.condition(ctx, stale, true, "Available", "Worker is ready"); err != nil {
		t.Fatal(err)
	}
	if err := c.Get(ctx, client.ObjectKeyFromObject(ledger), current); err != nil {
		t.Fatal(err)
	}
	if current.ResourceVersion != version {
		t.Fatal("stale capability identity changed status")
	}
}
