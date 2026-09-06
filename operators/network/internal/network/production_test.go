package network

import (
	"context"
	"testing"
	"time"

	bitcoinv1alpha1 "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha1"
	networkv1alpha1 "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

// productionObjects creates a stable owned ledger without relying on fake-server UID allocation.
func productionObjects(t *testing.T) (*networkv1alpha1.StacksNetwork, *bitcoinv1alpha1.BitcoinBlockProduction) {
	t.Helper()
	parent := fixture()
	parent.Generation = 1
	parent.Spec.BitcoinBlockProduction = &bitcoinv1alpha1.ProductionPolicy{Target: "bitcoin", IntervalSeconds: 5, Address: "mipcBbFg9gMiCh81Kj8tqqdgoZub1ZJRfn"}
	parent.Status.BitcoinProductionUID = "production-uid"
	policy := &bitcoinv1alpha1.BitcoinBlockProduction{ObjectMeta: metav1.ObjectMeta{Name: parent.Name, Namespace: parent.Namespace, UID: "production-uid", Generation: 1}, Spec: bitcoinv1alpha1.BitcoinBlockProductionSpec{NetworkName: parent.Name, NetworkUID: string(parent.UID), Policy: *parent.Spec.BitcoinBlockProduction.DeepCopy()}}
	if err := controllerutil.SetControllerReference(parent, policy, testScheme(t)); err != nil {
		t.Fatal(err)
	}
	return parent, policy
}

func TestProductionFailuresDoNotBlockActorUpdatesOrPruning(t *testing.T) {
	for _, mode := range []string{"missing-ledger", "terminating-ledger", "foreign-ledger", "changed-target"} {
		t.Run(mode, func(t *testing.T) {
			parent, policy := productionObjects(t)
			objects := []client.Object{parent}
			switch mode {
			case "terminating-ledger":
				policy.Finalizers = []string{"bitcoin.stacks.org/retain-production-ledger"}
				now := metav1.Now()
				policy.DeletionTimestamp = &now
			case "foreign-ledger":
				policy.OwnerReferences = nil
			case "changed-target":
				policy.Spec.Policy.Target = "previous-target"
			}
			if mode != "missing-ledger" {
				objects = append(objects, policy)
			}
			kube := fake.NewClientBuilder().WithScheme(testScheme(t)).WithStatusSubresource(parent, policy).WithObjects(objects...).Build()
			r := &Reconciler{Client: kube, APIReader: kube, Scheme: testScheme(t), Now: time.Now, ProductionEnabled: true}
			key := client.ObjectKeyFromObject(parent)
			if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: key}); err == nil {
				t.Fatal("production failure was not reported")
			}
			actor := &networkv1alpha1.BitcoinNode{}
			mustGet(t, context.Background(), kube, "testnet-bitcoin", actor)
			mustGet(t, context.Background(), kube, "testnet-signer-1", &networkv1alpha1.StacksSigner{})
			mustGet(t, context.Background(), kube, parent.Name, parent)
			parent.Spec.BitcoinNodes[0].Image = "bitcoin:updated"
			parent.Spec.Signers = nil
			parent.Generation++
			if err := kube.Update(context.Background(), parent); err != nil {
				t.Fatal(err)
			}
			_, _ = r.Reconcile(context.Background(), ctrl.Request{NamespacedName: key})
			mustGet(t, context.Background(), kube, actor.Name, actor)
			if actor.Spec.Image != "bitcoin:updated" {
				t.Fatal("optional production failure prevented actor update")
			}
			if err := kube.Get(context.Background(), client.ObjectKey{Namespace: parent.Namespace, Name: "testnet-signer-1"}, &networkv1alpha1.StacksSigner{}); !apierrors.IsNotFound(err) {
				t.Fatalf("retired signer not pruned: %v", err)
			}
			mustGet(t, context.Background(), kube, parent.Name, parent)
			condition := meta.FindStatusCondition(parent.Status.Conditions, "ProductionConfigured")
			if condition == nil || condition.Status != metav1.ConditionFalse || condition.Reason != "PolicyUnavailable" || parent.Status.Phase == "Degraded" || parent.Status.TargetDeclarations.ObservedGeneration != parent.Generation {
				t.Fatalf("production error replaced topology state: %#v", parent.Status)
			}
		})
	}
}

func TestProductionSynchronizationSurvivesUnrelatedTopologyFailure(t *testing.T) {
	parent, policy := productionObjects(t)
	parent.Spec.BitcoinBlockProduction.IntervalSeconds = 7
	foreign := &networkv1alpha1.StacksNode{ObjectMeta: metav1.ObjectMeta{Name: "testnet-follower", Namespace: parent.Namespace, UID: "foreign"}}
	kube := fake.NewClientBuilder().WithScheme(testScheme(t)).WithStatusSubresource(parent, policy).WithObjects(parent, policy, foreign).Build()
	r := &Reconciler{Client: kube, APIReader: kube, Scheme: testScheme(t), Now: time.Now, ProductionEnabled: true}
	if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(parent)}); err == nil {
		t.Fatal("expected unrelated ownership conflict")
	}
	mustGet(t, context.Background(), kube, policy.Name, policy)
	if policy.Spec.Policy.IntervalSeconds != 7 {
		t.Fatal("unrelated topology failure prevented production update")
	}
}

func TestConfiguredProductionReportsDisabledController(t *testing.T) {
	parent, policy := productionObjects(t)
	kube := fake.NewClientBuilder().WithScheme(testScheme(t)).WithStatusSubresource(parent, policy).WithObjects(parent, policy).Build()
	r := &Reconciler{Client: kube, APIReader: kube, Scheme: testScheme(t), Now: time.Now}
	if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(parent)}); err != nil {
		t.Fatal(err)
	}
	mustGet(t, context.Background(), kube, parent.Name, parent)
	condition := meta.FindStatusCondition(parent.Status.Conditions, "ProductionConfigured")
	if condition == nil || condition.Status != metav1.ConditionFalse || condition.Reason != "ControllerDisabled" {
		t.Fatalf("disabled producer was not explained: %#v", condition)
	}
}

func TestProductionWatchFiltersAccountingButKeepsLifecycle(t *testing.T) {
	_, base := productionObjects(t)
	filter := productionLifecyclePredicate()
	for name, change := range map[string]func(*bitcoinv1alpha1.BitcoinBlockProduction){
		"uid":        func(p *bitcoinv1alpha1.BitcoinBlockProduction) { p.UID = "replacement" },
		"generation": func(p *bitcoinv1alpha1.BitcoinBlockProduction) { p.Generation++ },
		"deletion":   func(p *bitcoinv1alpha1.BitcoinBlockProduction) { now := metav1.Now(); p.DeletionTimestamp = &now },
		"owner":      func(p *bitcoinv1alpha1.BitcoinBlockProduction) { p.OwnerReferences = nil },
		"finalizer":  func(p *bitcoinv1alpha1.BitcoinBlockProduction) { p.Finalizers = []string{"retain"} },
	} {
		t.Run(name, func(t *testing.T) {
			updated := base.DeepCopy()
			change(updated)
			if !filter.Update(event.UpdateEvent{ObjectOld: base, ObjectNew: updated}) {
				t.Fatal("lifecycle update was filtered")
			}
		})
	}
	updated := base.DeepCopy()
	updated.Status.BlocksProduced++
	updated.Status.Phase = "Accounting"
	if filter.Update(event.UpdateEvent{ObjectOld: base, ObjectNew: updated}) {
		t.Fatal("receipt accounting triggered aggregate reconciliation")
	}
	if !filter.Create(event.CreateEvent{Object: base}) || !filter.Delete(event.DeleteEvent{Object: base}) {
		t.Fatal("creation or deletion was filtered")
	}
}
