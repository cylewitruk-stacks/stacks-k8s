package network

import (
	"context"
	"fmt"
	"reflect"

	bitcoinv1alpha1 "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha1"
	networkv1alpha1 "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

// synchronizeProduction pins the ledger before a producer may authorize RPC work.
func (r *Reconciler) synchronizeProduction(ctx context.Context, parent *networkv1alpha1.StacksNetwork) error {
	if parent.Spec.BitcoinBlockProduction == nil {
		return nil
	}
	current := &bitcoinv1alpha1.BitcoinBlockProduction{}
	key := client.ObjectKeyFromObject(parent)
	err := r.APIReader.Get(ctx, key, current)
	if apierrors.IsNotFound(err) {
		if parent.Status.BitcoinProductionUID != "" {
			return fmt.Errorf("production ledger is missing; recreate the environment in a new namespace")
		}
		current = &bitcoinv1alpha1.BitcoinBlockProduction{ObjectMeta: metav1.ObjectMeta{Name: parent.Name, Namespace: parent.Namespace}}
		current.Spec = bitcoinv1alpha1.BitcoinBlockProductionSpec{NetworkName: parent.Name, NetworkUID: string(parent.UID), Policy: *parent.Spec.BitcoinBlockProduction.DeepCopy()}
		if err := controllerutil.SetControllerReference(parent, current, r.Scheme); err != nil {
			return err
		}
		if err := r.Create(ctx, current); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	if !ownedByNetwork(parent, current) || current.Spec.NetworkUID != string(parent.UID) {
		return fmt.Errorf("production resource has a different owner")
	}
	if parent.Status.BitcoinProductionUID == "" {
		base := parent.DeepCopy()
		parent.Status.BitcoinProductionUID = string(current.UID)
		if err := r.Status().Patch(ctx, parent, client.MergeFromWithOptions(base, client.MergeFromWithOptimisticLock{})); err != nil {
			parent.Status.BitcoinProductionUID = base.Status.BitcoinProductionUID
			return err
		}
	}
	if parent.Status.BitcoinProductionUID != string(current.UID) {
		return fmt.Errorf("production ledger UID changed; automatic readmission is unsupported")
	}
	if !current.DeletionTimestamp.IsZero() {
		return fmt.Errorf("production ledger is terminating; delete the owning environment to abandon it")
	}
	if current.Spec.Policy.Target != parent.Spec.BitcoinBlockProduction.Target {
		return fmt.Errorf("changing the initial production target requires a fresh environment")
	}
	if reflect.DeepEqual(current.Spec.Policy, *parent.Spec.BitcoinBlockProduction) {
		return nil
	}
	base := current.DeepCopy()
	current.Spec.Policy = *parent.Spec.BitcoinBlockProduction.DeepCopy()
	return r.Patch(ctx, current, client.MergeFromWithOptions(base, client.MergeFromWithOptimisticLock{}))
}

// productionCondition reports configuration independently of actor and RPC health.
func (r *Reconciler) productionCondition(parent *networkv1alpha1.StacksNetwork, err error) metav1.Condition {
	status, reason, message := metav1.ConditionTrue, "PolicyReady", "Production policy is compiled; inspect BitcoinBlockProduction for execution state"
	if parent.Spec.BitcoinBlockProduction == nil {
		status, reason, message = metav1.ConditionFalse, "NotRequested", "No baseline production is requested"
	} else if err != nil {
		status, reason, message = metav1.ConditionFalse, "PolicyUnavailable", err.Error()
	} else if !r.ProductionEnabled {
		status, reason, message = metav1.ConditionFalse, "ControllerDisabled", "Enable bitcoinProduction.enabled in the network chart to run the compiled policy"
	}
	return condition(parent.Generation, status, "ProductionConfigured", reason, message)
}

// productionLifecyclePredicate ignores accounting while retaining desired state and ownership events.
func productionLifecyclePredicate() predicate.Predicate {
	return predicate.Funcs{UpdateFunc: func(update event.UpdateEvent) bool {
		old, current := update.ObjectOld, update.ObjectNew
		if old == nil || current == nil {
			return false
		}
		return old.GetUID() != current.GetUID() || old.GetGeneration() != current.GetGeneration() ||
			!reflect.DeepEqual(old.GetDeletionTimestamp(), current.GetDeletionTimestamp()) ||
			!reflect.DeepEqual(old.GetOwnerReferences(), current.GetOwnerReferences()) ||
			!reflect.DeepEqual(old.GetFinalizers(), current.GetFinalizers())
	}}
}
