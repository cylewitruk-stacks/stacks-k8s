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
)

// DefinitionReconciler validates reusable inputs without creating network workloads.
type DefinitionReconciler struct {
	// Client accesses reusable declarations and owned default identities.
	Client client.Client
	// Scheme supplies owner reference type information.
	Scheme *runtime.Scheme
	// Kind selects the independent definition controller.
	Kind string
}

func (r *DefinitionReconciler) object() resolvable {
	switch r.Kind {
	case "StacksEpochSchedule":
		return &api.StacksEpochSchedule{}
	case "BitcoinBlockSchedule":
		return &bitcoin.BitcoinBlockSchedule{}
	default:
		return DefinitionObject(api.ParticipantKind(r.Kind)).(resolvable)
	}
}

// Reconcile records intrinsic validity; network-relative wiring is resolved by the root.
func (r *DefinitionReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	obj := r.object()
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
	condition := metav1.Condition{Type: "Resolved", Status: metav1.ConditionTrue, Reason: "DefinitionValid", Message: "Intrinsic inputs valid; instance wiring is resolved by StacksNetwork", ObservedGeneration: obj.GetGeneration()}
	if validation != nil {
		condition.Status = metav1.ConditionFalse
		condition.Reason = "DefinitionUnavailable"
		condition.Message = validation.Error()
	}
	meta.SetStatusCondition(&status.Conditions, condition)
	if !equal(*previous, *status) {
		if err := r.Client.Status().Patch(ctx, obj, client.MergeFromWithOptions(base, client.MergeFromWithOptimisticLock{})); err != nil {
			return ctrl.Result{}, err
		}
	}
	if validation != nil {
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	}
	return ctrl.Result{}, nil
}

// SetupWithManager registers one resource-focused definition controller.
func (r *DefinitionReconciler) SetupWithManager(m ctrl.Manager) error {
	if r.object() == nil {
		return fmt.Errorf("unsupported definition kind")
	}
	return ctrl.NewControllerManagedBy(m).Named("foundation-definition-" + r.Kind).For(r.object()).Owns(&stacks.StacksAccount{}).Complete(r)
}

func defaultAccount(config *api.Configuration, owner client.Object) *common.NameRef {
	switch {
	case config.StacksNode != nil && config.StacksNode.IdentityAccountRef == nil && allowsDefaultIdentity(config.StacksNode):
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
