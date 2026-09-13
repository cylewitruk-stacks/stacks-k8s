package foundation

import (
	"context"
	"fmt"
	"time"

	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	stacks "github.com/cylewitruk-stacks/stacks-k8s/apis/network/stacks/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
)

// DefinitionReconciler validates reusable inputs without creating network workloads.
type DefinitionReconciler struct {
	// Client accesses reusable declarations and owned default identities.
	Client client.Client
	// Scheme supplies owner reference type information.
	Scheme *runtime.Scheme
	// Prototype selects the declaration type; each reconcile receives a fresh copy.
	Prototype client.Object
}

// object restricts registration to supported definitions and never returns the prototype itself.
func (r *DefinitionReconciler) object() (resolvable, error) {
	switch r.Prototype.(type) {
	case *bitcoin.BitcoinNode, *bitcoin.BitcoinBlockProduction, *bitcoin.BitcoinBlockSchedule,
		*stacks.StacksNode, *stacks.StacksSigner, *stacks.StacksStacker, *stacks.StacksFaucet,
		*stacks.StacksContractSet, *stacks.StacksTransactionProduction, *api.StacksEpochSchedule:
		if object := r.Prototype.DeepCopyObject(); object != nil {
			return object.(resolvable), nil
		}
	}
	return nil, fmt.Errorf("unsupported or nil definition prototype %T", r.Prototype)
}

// Reconcile records intrinsic validity; network-relative wiring is resolved by the root.
func (r *DefinitionReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	obj, err := r.object()
	if err != nil {
		return ctrl.Result{}, err
	}
	if err := r.Client.Get(ctx, req.NamespacedName, obj); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if obj.GetDeletionTimestamp() != nil {
		return ctrl.Result{}, nil
	}
	base := obj.DeepCopyObject().(client.Object)
	status := obj.GetResolutionStatus()
	previous := status.DeepCopy()
	input, err := sourceSpec(obj)
	if err != nil {
		return ctrl.Result{}, err
	}
	var validation error
	switch v := obj.(type) {
	case *stacks.StacksNode:
		if v.Spec.IdentityAccountRef == nil && allowsDefaultIdentity(&v.Spec) {
			validation = EnsureDefaultAccount(ctx, r.Client, r.Scheme, obj, DefaultAccountName(v.Name, "identity"))
		}
	case *stacks.StacksFaucet:
		if v.Spec.AccountRef == nil {
			validation = EnsureDefaultAccount(ctx, r.Client, r.Scheme, obj, DefaultAccountName(v.Name, "account"))
		}
	case *bitcoin.BitcoinBlockSchedule:
		validation = validateSchedule(v.Spec)
	case *api.StacksEpochSchedule:
		validation = validateEpochs(v.Spec.Epochs)
	}
	status.ObservedGeneration = obj.GetGeneration()
	status.Digest = Digest(input)
	condition := metav1.Condition{
		Type:               common.ConditionResolved,
		Status:             metav1.ConditionTrue,
		Reason:             reasonDefinitionValid,
		Message:            "Intrinsic inputs valid; instance wiring is resolved by StacksNetwork",
		ObservedGeneration: obj.GetGeneration(),
	}
	if validation != nil {
		condition.Status = metav1.ConditionFalse
		condition.Reason = reasonDefinitionUnavailable
		condition.Message = validation.Error()
	}
	meta.SetStatusCondition(&status.Conditions, condition)
	if !equal(*previous, *status) {
		if err := r.Client.Status().
			Patch(ctx, obj, client.MergeFromWithOptions(base, client.MergeFromWithOptimisticLock{})); err != nil {
			return ctrl.Result{}, err
		}
	}
	if validation != nil {
		//nolint:nilerr // Resolution errors are published as conditions and retried on the bounded polling interval.
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	}
	return ctrl.Result{}, nil
}

// SetupWithManager registers one resource-focused definition controller.
func (r *DefinitionReconciler) SetupWithManager(m ctrl.Manager) error {
	object, err := r.object()
	if err != nil {
		return err
	}
	gvk, err := apiutil.GVKForObject(object, m.GetScheme())
	if err != nil {
		return err
	}
	return ctrl.NewControllerManagedBy(m).
		Named("foundation-definition-" + gvk.Kind).
		For(object).
		Owns(&stacks.StacksAccount{}).
		Complete(r)
}

func defaultAccount(config *api.Configuration, owner client.Object) *common.NameRef {
	switch {
	case config.StacksNode != nil &&
		config.StacksNode.IdentityAccountRef == nil &&
		allowsDefaultIdentity(config.StacksNode):
		ref := &common.NameRef{Name: DefaultAccountName(owner.GetName(), "identity")}
		config.StacksNode.IdentityAccountRef = ref
		return ref
	case config.StacksFaucet != nil && config.StacksFaucet.AccountRef == nil:
		ref := &common.NameRef{Name: DefaultAccountName(owner.GetName(), "account")}
		config.StacksFaucet.AccountRef = ref
		return ref
	}
	return nil
}

func allowsDefaultIdentity(node *stacks.StacksNodeSpec) bool {
	return node.Mining == nil || (!ptr.Deref(node.Mining.Enabled, false) && node.Mining.BitcoinWalletRef == nil)
}
