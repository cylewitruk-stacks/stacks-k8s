package network

import (
	"context"
	"fmt"
	"reflect"

	stacks "github.com/cylewitruk-stacks/stacks-k8s/apis/network/stacks/v1alpha1"
	network "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha1"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/naming"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/protocolcontracts"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// operationObjects compiles public protocol declarations without reading keys or chain state.
func operationObjects(parent *network.StacksNetwork) []client.Object {
	if parent.Spec.Operation == nil {
		return nil
	}
	objects := []client.Object{}
	metadata := func(kind, name string) metav1.ObjectMeta {
		return metav1.ObjectMeta{Name: naming.Child(parent.Name, kind+"-"+name), Namespace: parent.Namespace}
	}
	for _, p := range parent.Spec.Operation.Accounts {
		objects = append(objects, &stacks.StacksAccount{TypeMeta: metav1.TypeMeta{APIVersion: stacks.GroupVersion.String(), Kind: "StacksAccount"}, ObjectMeta: metadata("account", p.Name),
			Spec: stacks.StacksAccountSpec{NetworkName: parent.Name, NetworkUID: string(parent.UID), Policy: *p.DeepCopy()}})
	}
	for _, p := range parent.Spec.Operation.ContractSets {
		objects = append(objects, &stacks.StacksContractSet{TypeMeta: metav1.TypeMeta{APIVersion: stacks.GroupVersion.String(), Kind: "StacksContractSet"}, ObjectMeta: metadata("contracts", p.Name),
			Spec: stacks.StacksContractSetSpec{NetworkName: parent.Name, NetworkUID: string(parent.UID), Policy: *p.DeepCopy()}})
	}
	for _, p := range parent.Spec.Operation.Participants {
		objects = append(objects, &stacks.StacksStackingParticipant{TypeMeta: metav1.TypeMeta{APIVersion: stacks.GroupVersion.String(), Kind: "StacksStackingParticipant"}, ObjectMeta: metadata("stacking", p.Name),
			Spec: stacks.StacksStackingParticipantSpec{NetworkName: parent.Name, NetworkUID: string(parent.UID), Policy: *p.DeepCopy()}})
	}
	return objects
}

// synchronizeOperation retains ledger and consumer identity independently of topology reconciliation.
func (r *Reconciler) synchronizeOperation(ctx context.Context, parent *network.StacksNetwork) error {
	if parent.Spec.Operation != nil {
		for _, set := range parent.Spec.Operation.ContractSets {
			if _, err := protocolcontracts.Order(set.Contracts); err != nil {
				return fmt.Errorf("invalid contract set %s: %w", set.Name, err)
			}
		}
	}
	for _, desired := range operationObjects(parent) {
		kind := desired.GetObjectKind().GroupVersionKind().Kind
		pinned := ""
		for _, identity := range parent.Status.Capabilities {
			if identity.Kind == kind && identity.Name == desired.GetName() {
				pinned = identity.UID
			}
		}
		current := desired.DeepCopyObject().(client.Object)
		err := r.APIReader.Get(ctx, client.ObjectKeyFromObject(desired), current)
		if apierrors.IsNotFound(err) {
			if pinned != "" {
				return fmt.Errorf("managed %s %s is missing; its authority cannot be recreated", kind, desired.GetName())
			}
			if len(parent.Status.Capabilities) >= 64 {
				return fmt.Errorf("network exceeds its retained capability identity limit")
			}
			if err := controllerutil.SetControllerReference(parent, current, r.Scheme); err != nil {
				return err
			}
			if err := r.Create(ctx, current); err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
		if !ownedByNetwork(parent, current) || !current.GetDeletionTimestamp().IsZero() {
			return fmt.Errorf("managed %s %s is foreign or terminating", kind, desired.GetName())
		}
		if pinned == "" {
			base := parent.DeepCopy()
			parent.Status.Capabilities = append(parent.Status.Capabilities, network.CapabilityIdentity{Kind: kind, Name: current.GetName(), UID: string(current.GetUID())})
			if err := r.Status().Patch(ctx, parent, client.MergeFromWithOptions(base, client.MergeFromWithOptimisticLock{})); err != nil {
				parent.Status = base.Status
				return err
			}
		} else if pinned != string(current.GetUID()) {
			return fmt.Errorf("managed %s %s identity changed", kind, desired.GetName())
		}
		if reflect.DeepEqual(operationSpec(current), operationSpec(desired)) {
			continue
		}
		base := current.DeepCopyObject().(client.Object)
		switch value := current.(type) {
		case *stacks.StacksAccount:
			return fmt.Errorf("managed account %s authority is immutable", value.Name)
		case *stacks.StacksContractSet:
			value.Spec = *desired.(*stacks.StacksContractSet).Spec.DeepCopy()
		case *stacks.StacksStackingParticipant:
			value.Spec = *desired.(*stacks.StacksStackingParticipant).Spec.DeepCopy()
		}
		if err := r.Patch(ctx, current, client.MergeFromWithOptions(base, client.MergeFromWithOptimisticLock{})); err != nil {
			return err
		}
	}
	return nil
}

// operationSpec selects only a compiled child's public desired state.
func operationSpec(object client.Object) any {
	switch value := object.(type) {
	case *stacks.StacksAccount:
		return value.Spec
	case *stacks.StacksContractSet:
		return value.Spec
	case *stacks.StacksStackingParticipant:
		return value.Spec
	default:
		panic("unsupported managed operation object")
	}
}

// operationCondition reports capability convergence separately from actor readiness.
func (r *Reconciler) operationCondition(ctx context.Context, parent *network.StacksNetwork, err error) metav1.Condition {
	if parent.Spec.Operation == nil {
		return condition(parent.Generation, metav1.ConditionFalse, "Operational", "NotRequested", "No managed protocol operation is declared")
	}
	if err != nil {
		return condition(parent.Generation, metav1.ConditionFalse, "Operational", "CapabilityUnavailable", err.Error())
	}
	if parent.Spec.Suspended || parent.Spec.Operation.Paused {
		return condition(parent.Generation, metav1.ConditionFalse, "Operational", "Paused", "Managed protocol operation is paused")
	}
	if !r.OperationEnabled {
		return condition(parent.Generation, metav1.ConditionFalse, "Operational", "ProvisioningDisabled", "Worker provisioning is disabled; existing workers may continue. Use network pause policy to stop new work")
	}
	for _, object := range operationObjects(parent) {
		if _, account := object.(*stacks.StacksAccount); account {
			continue
		}
		desired := object.DeepCopyObject().(client.Object)
		if err := r.APIReader.Get(ctx, client.ObjectKeyFromObject(object), object); err != nil {
			return condition(parent.Generation, metav1.ConditionFalse, "Operational", "ObservationUnavailable", "Managed capability cannot be observed")
		}
		var status stacks.ManagedOperationStatus
		switch value := object.(type) {
		case *stacks.StacksContractSet:
			status = value.Status
		case *stacks.StacksStackingParticipant:
			status = value.Status
		}
		if status.ObservedGeneration != object.GetGeneration() || status.Phase != "Ready" || !reflect.DeepEqual(operationSpec(desired), operationSpec(object)) || !ownedByNetwork(parent, object) || !object.GetDeletionTimestamp().IsZero() {
			return condition(parent.Generation, metav1.ConditionFalse, "Operational", "PrerequisitesPending", "Inspect managed contract and stacking capabilities for unmet prerequisites")
		}
	}
	return condition(parent.Generation, metav1.ConditionTrue, "Operational", "CapabilitiesReady", "Managed prerequisites and participation are observed; sustained block production is a separate observation")
}
