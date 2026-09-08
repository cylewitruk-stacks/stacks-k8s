package network

import (
	"context"
	"fmt"
	"reflect"

	stacksv1alpha1 "github.com/cylewitruk-stacks/stacks-k8s/apis/network/stacks/v1alpha1"
	networkv1alpha1 "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// synchronizeTransactions pins the ledger before a producer may authorize RPC work.
func (r *Reconciler) synchronizeTransactions(ctx context.Context, parent *networkv1alpha1.StacksNetwork) error {
	if parent.Spec.StacksTransactionProduction == nil {
		return nil
	}
	current := &stacksv1alpha1.StacksTransactionProduction{}
	key := client.ObjectKeyFromObject(parent)
	err := r.APIReader.Get(ctx, key, current)
	if apierrors.IsNotFound(err) {
		if parent.Status.TransactionProductionUID != "" {
			return fmt.Errorf("production ledger is missing; recreate the environment in a new namespace")
		}
		current = &stacksv1alpha1.StacksTransactionProduction{ObjectMeta: metav1.ObjectMeta{Name: parent.Name, Namespace: parent.Namespace}}
		current.Spec = stacksv1alpha1.StacksTransactionProductionSpec{NetworkName: parent.Name, NetworkUID: string(parent.UID), Policy: *parent.Spec.StacksTransactionProduction.DeepCopy()}
		if err := controllerutil.SetControllerReference(parent, current, r.Scheme); err != nil {
			return err
		}
		if err := r.Create(ctx, current); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	if !ownedByNetwork(parent, current) || current.Spec.NetworkUID != string(parent.UID) || current.Spec.NetworkName != parent.Name {
		return fmt.Errorf("production resource has a different owner")
	}
	if parent.Status.TransactionProductionUID == "" {
		base := parent.DeepCopy()
		parent.Status.TransactionProductionUID = string(current.UID)
		if err := r.Status().Patch(ctx, parent, client.MergeFromWithOptions(base, client.MergeFromWithOptimisticLock{})); err != nil {
			parent.Status.TransactionProductionUID = base.Status.TransactionProductionUID
			return err
		}
	}
	if parent.Status.TransactionProductionUID != string(current.UID) {
		return fmt.Errorf("production ledger UID changed; automatic readmission is unsupported")
	}
	if !current.DeletionTimestamp.IsZero() {
		return fmt.Errorf("production ledger is terminating; delete the owning environment to abandon it")
	}
	if current.Spec.Policy.Target != parent.Spec.StacksTransactionProduction.Target || current.Spec.Policy.Sender != parent.Spec.StacksTransactionProduction.Sender {
		return fmt.Errorf("changing transaction ingress or sender requires a fresh environment")
	}
	if reflect.DeepEqual(current.Spec.Policy, *parent.Spec.StacksTransactionProduction) {
		return nil
	}
	base := current.DeepCopy()
	current.Spec.Policy = *parent.Spec.StacksTransactionProduction.DeepCopy()
	return r.Patch(ctx, current, client.MergeFromWithOptions(base, client.MergeFromWithOptimisticLock{}))
}

// transactionCondition reports configuration independently of actor and RPC health.
func (r *Reconciler) transactionCondition(parent *networkv1alpha1.StacksNetwork, err error) metav1.Condition {
	status, reason, message := metav1.ConditionTrue, "PolicyReady", "Production policy is compiled; inspect StacksTransactionProduction for execution state"
	if parent.Spec.StacksTransactionProduction == nil {
		status, reason, message = metav1.ConditionFalse, "NotRequested", "No baseline production is requested"
	} else if err != nil {
		status, reason, message = metav1.ConditionFalse, "PolicyUnavailable", err.Error()
	} else if !r.TransactionsEnabled {
		status, reason, message = metav1.ConditionFalse, "ProvisioningDisabled", "Worker provisioning is disabled; existing workers may continue. Use network pause policy to stop new work"
	}
	return condition(parent.Generation, status, "TransactionsConfigured", reason, message)
}
