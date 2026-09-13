package faucetrequest

import (
	"context"

	stacks "github.com/cylewitruk-stacks/stacks-k8s/apis/network/stacks/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

const (
	networkIndex     = "faucetrequest.networkUID"
	faucetIndex      = "faucetrequest.faucetName"
	destinationIndex = "faucetrequest.destinationAccount"
)

// SetupWithManager registers serial request admission and indexed dependency wakeups.
func (r *Reconciler) SetupWithManager(manager ctrl.Manager) error {
	for name, extract := range map[string]client.IndexerFunc{
		networkIndex: func(o client.Object) []string {
			return []string{string(o.(*stacks.StacksFaucetRequest).Spec.NetworkUID)}
		},
		faucetIndex: func(o client.Object) []string { return []string{o.(*stacks.StacksFaucetRequest).Spec.FaucetRef.Name} },
		destinationIndex: func(o client.Object) []string {
			ref := o.(*stacks.StacksFaucetRequest).Spec.Destination.AccountRef
			if ref == nil {
				return nil
			}
			return []string{ref.Name}
		},
	} {
		if err := manager.GetFieldIndexer().IndexField(context.Background(), &stacks.StacksFaucetRequest{}, name, extract); err != nil {
			return err
		}
	}
	return ctrl.NewControllerManagedBy(manager).Named("faucet-requests-v1alpha2").WithOptions(controller.Options{MaxConcurrentReconciles: 1}).
		For(&stacks.StacksFaucetRequest{}, builder.WithPredicates(predicate.Funcs{UpdateFunc: func(e event.UpdateEvent) bool {
			old, current := e.ObjectOld.(*stacks.StacksFaucetRequest), e.ObjectNew.(*stacks.StacksFaucetRequest)
			return old.UID != current.UID || !equality.Semantic.DeepEqual(old.DeletionTimestamp, current.DeletionTimestamp) || !equality.Semantic.DeepEqual(old.Status.Admission, current.Status.Admission) || !equality.Semantic.DeepEqual(old.Status.Execution, current.Status.Execution)
		}})).
		Watches(&api.StacksNetwork{}, handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, o client.Object) []ctrl.Request {
			return r.requests(ctx, o.GetNamespace(), networkIndex, string(o.GetUID()))
		}), builder.WithPredicates(predicate.Funcs{UpdateFunc: func(e event.UpdateEvent) bool {
			old, current := e.ObjectOld.(*api.StacksNetwork), e.ObjectNew.(*api.StacksNetwork)
			return old.Generation != current.Generation || old.Status.Phase != current.Status.Phase || !equality.Semantic.DeepEqual(old.Status.Identities, current.Status.Identities) || !equality.Semantic.DeepEqual(old.DeletionTimestamp, current.DeletionTimestamp)
		}})).
		Watches(&api.StacksNetworkParticipant{}, handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, o client.Object) []ctrl.Request {
			p := o.(*api.StacksNetworkParticipant)
			if p.Spec.Kind != api.ParticipantStacksFaucet {
				return nil
			}
			return r.requests(ctx, p.Namespace, faucetIndex, p.Spec.ParticipantName)
		}), builder.WithPredicates(predicate.Funcs{UpdateFunc: func(e event.UpdateEvent) bool {
			old, current := e.ObjectOld.(*api.StacksNetworkParticipant), e.ObjectNew.(*api.StacksNetworkParticipant)
			if old.Generation != current.Generation || !equality.Semantic.DeepEqual(old.Status.Admission, current.Status.Admission) || !equality.Semantic.DeepEqual(old.DeletionTimestamp, current.DeletionTimestamp) {
				return true
			}
			a, b := old.Status.Execution, current.Status.Execution
			return a == nil || b == nil || a.Phase != b.Phase || a.PodUID != b.PodUID || a.ProcessNonce != b.ProcessNonce
		}})).
		Watches(&stacks.StacksAccount{}, handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, o client.Object) []ctrl.Request {
			return r.requests(ctx, o.GetNamespace(), destinationIndex, o.GetName())
		}), builder.WithPredicates(predicate.Funcs{UpdateFunc: func(e event.UpdateEvent) bool {
			old, current := e.ObjectOld.(*stacks.StacksAccount), e.ObjectNew.(*stacks.StacksAccount)
			return old.Generation != current.Generation || !equality.Semantic.DeepEqual(old.Status, current.Status) || !equality.Semantic.DeepEqual(old.DeletionTimestamp, current.DeletionTimestamp)
		}})).Complete(r)
}

// requests uses the manager cache for routing only; admission always fresh-reads current objects.
func (r *Reconciler) requests(ctx context.Context, namespace, index, value string) []ctrl.Request {
	var list stacks.StacksFaucetRequestList
	if err := r.Client.List(ctx, &list, client.InNamespace(namespace), client.MatchingFields{index: value}); err != nil {
		return nil
	}
	out := []ctrl.Request{}
	for _, item := range list.Items {
		if item.Status.Phase == stacks.FaucetCompleted || item.Status.Phase == stacks.FaucetRejected || item.Status.Phase == stacks.FaucetExpired {
			continue
		}
		out = append(out, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(&item)})
	}
	return out
}

// activeCount uses coherent paginated native lists only when considering new admission.
func (r *Reconciler) activeCount(ctx context.Context, namespace string, worker types.UID) (int, error) {
	count := 0
	unobserved := map[types.UID]bool{}
	for uid, reservedWorker := range r.uncertain {
		if reservedWorker == worker {
			unobserved[uid] = true
		}
	}
	continuation := ""
	for {
		var list stacks.StacksFaucetRequestList
		if err := r.Reader.List(ctx, &list, &client.ListOptions{Namespace: namespace, Limit: 500, Continue: continuation}); err != nil {
			return 0, err
		}
		for _, item := range list.Items {
			a := item.Status.Admission
			if a != nil && a.Decision != stacks.FaucetDecisionPending {
				delete(unobserved, item.UID)
				delete(r.uncertain, item.UID)
			}
			if a != nil && a.Decision == stacks.FaucetDecisionAdmitted && a.Worker != nil && a.Worker.UID == worker && !(MatchingExecution(&item) && TerminalExecution(item.Status.Execution)) {
				count++
			}
			if count >= Capacity {
				return count, nil
			}
		}
		continuation = list.Continue
		if continuation == "" {
			return count + len(unobserved), nil
		}
	}
}
