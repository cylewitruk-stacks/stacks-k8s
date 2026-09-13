package bitcoincontrol

import (
	"context"

	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/foundation"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

// participantWorkloadEvents ignores status heartbeats unrelated to workload inputs.
func participantWorkloadEvents() predicate.Predicate {
	return predicate.Funcs{UpdateFunc: func(e event.UpdateEvent) bool {
		old, ok := e.ObjectOld.(*api.StacksNetworkParticipant)
		current, valid := e.ObjectNew.(*api.StacksNetworkParticipant)
		if !ok || !valid {
			return false
		}
		return old.UID != current.UID || old.Generation != current.Generation || !equality.Semantic.DeepEqual(old.DeletionTimestamp, current.DeletionTimestamp) || !equality.Semantic.DeepEqual(old.Status.Admission, current.Status.Admission) || !equality.Semantic.DeepEqual(old.Status.Runtime, current.Status.Runtime)
	}}
}

// rootWorkloadEvents accepts durable enrollment/control changes without runtime counters.
func rootWorkloadEvents() predicate.Predicate {
	return predicate.Funcs{UpdateFunc: func(e event.UpdateEvent) bool {
		old, ok := e.ObjectOld.(*api.StacksNetwork)
		current, valid := e.ObjectNew.(*api.StacksNetwork)
		if !ok || !valid {
			return false
		}
		if old.UID != current.UID || old.Generation != current.Generation || failed(old) != failed(current) || !equality.Semantic.DeepEqual(old.DeletionTimestamp, current.DeletionTimestamp) {
			return true
		}
		if old.Status.Bitcoin == nil || current.Status.Bitcoin == nil {
			return !equality.Semantic.DeepEqual(old.Status.Bitcoin, current.Status.Bitcoin)
		}
		return !equality.Semantic.DeepEqual(old.Status.Bitcoin.ExecutionRefs, current.Status.Bitcoin.ExecutionRefs) || !equality.Semantic.DeepEqual(old.Status.Bitcoin.InitializationRef, current.Status.Bitcoin.InitializationRef)
	}}
}

// ownedWorkloadEvents ignores Deployment readiness counters but repairs spec/RBAC drift.
func ownedWorkloadEvents() predicate.Predicate {
	return predicate.Funcs{UpdateFunc: func(e event.UpdateEvent) bool {
		if e.ObjectOld.GetUID() != e.ObjectNew.GetUID() || e.ObjectOld.GetGeneration() != e.ObjectNew.GetGeneration() || !equality.Semantic.DeepEqual(e.ObjectOld.GetDeletionTimestamp(), e.ObjectNew.GetDeletionTimestamp()) || !equality.Semantic.DeepEqual(e.ObjectOld.GetLabels(), e.ObjectNew.GetLabels()) || !equality.Semantic.DeepEqual(e.ObjectOld.GetOwnerReferences(), e.ObjectNew.GetOwnerReferences()) {
			return true
		}
		switch old := e.ObjectOld.(type) {
		case *rbacv1.Role:
			current, ok := e.ObjectNew.(*rbacv1.Role)
			return !ok || !equality.Semantic.DeepEqual(old.Rules, current.Rules)
		case *rbacv1.RoleBinding:
			current, ok := e.ObjectNew.(*rbacv1.RoleBinding)
			return !ok || old.RoleRef != current.RoleRef || !equality.Semantic.DeepEqual(old.Subjects, current.Subjects)
		case *corev1.ServiceAccount:
			current, ok := e.ObjectNew.(*corev1.ServiceAccount)
			return !ok || !equality.Semantic.DeepEqual(old.AutomountServiceAccountToken, current.AutomountServiceAccountToken)
		}
		return false
	}}
}

// registerWorkloadWatches routes owned resources and exact retained record acknowledgements.
func (r *WorkloadReconciler) registerWorkloadWatches(m ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(m).Named("bitcoin-control-workloads-v1alpha2").For(&api.StacksNetworkParticipant{}, builder.WithPredicates(participantWorkloadEvents())).Owns(&appsv1.Deployment{}, builder.WithPredicates(predicate.Or(ownedWorkloadEvents(), ControlLifecycleEvents()))).Watches(&corev1.Pod{}, handler.EnqueueRequestsFromMapFunc(r.ControlLifecycleRequests), builder.WithPredicates(ControlLifecycleEvents())).Watches(&appsv1.ReplicaSet{}, handler.EnqueueRequestsFromMapFunc(r.ControlLifecycleRequests), builder.WithPredicates(ControlLifecycleEvents())).Owns(&corev1.ServiceAccount{}, builder.WithPredicates(ownedWorkloadEvents())).Owns(&rbacv1.Role{}, builder.WithPredicates(ownedWorkloadEvents())).Owns(&rbacv1.RoleBinding{}, builder.WithPredicates(ownedWorkloadEvents())).Watches(&api.StacksNetwork{}, handler.EnqueueRequestsFromMapFunc(r.enqueue), builder.WithPredicates(rootWorkloadEvents())).Watches(&bitcoin.BitcoinExecution{}, handler.EnqueueRequestsFromMapFunc(func(_ context.Context, obj client.Object) []ctrl.Request {
		record := obj.(*bitcoin.BitcoinExecution)
		return []ctrl.Request{{NamespacedName: client.ObjectKey{Namespace: record.Namespace, Name: record.Spec.Participant.Name}}}
	}), builder.WithPredicates(predicate.Funcs{UpdateFunc: func(e event.UpdateEvent) bool {
		old, ok := e.ObjectOld.(*bitcoin.BitcoinExecution)
		current, valid := e.ObjectNew.(*bitcoin.BitcoinExecution)
		return !ok || !valid || old.UID != current.UID || !equality.Semantic.DeepEqual(old.DeletionTimestamp, current.DeletionTimestamp) || !equality.Semantic.DeepEqual(old.Status.Drain, current.Status.Drain)
	}})).Watches(&bitcoin.BitcoinInitialization{}, handler.EnqueueRequestsFromMapFunc(func(_ context.Context, obj client.Object) []ctrl.Request {
		init := obj.(*bitcoin.BitcoinInitialization)
		nodes := append([]common.Binding{}, init.Spec.Nodes...)
		if init.Status.Baseline != nil {
			for _, target := range init.Status.Baseline.Scheduling.Targets {
				nodes = append(nodes, target.Participant)
			}
		}
		requests := make([]ctrl.Request, 0, len(nodes))
		for _, node := range nodes {
			requests = append(requests, ctrl.Request{NamespacedName: client.ObjectKey{Namespace: init.Namespace, Name: node.Name}})
		}
		return requests
	}), builder.WithPredicates(predicate.Funcs{UpdateFunc: func(e event.UpdateEvent) bool {
		old, ok := e.ObjectOld.(*bitcoin.BitcoinInitialization)
		current, valid := e.ObjectNew.(*bitcoin.BitcoinInitialization)
		return !ok || !valid || !equality.Semantic.DeepEqual(old.Status.Production, current.Status.Production) || !equality.Semantic.DeepEqual(old.Status.Override, current.Status.Override) || baselineAdmissionChanged(old, current)
	}})).Complete(r)
}

// workloadRequests uses captured root identities rather than a namespace inventory query.
func workloadRequests(root *api.StacksNetwork) []ctrl.Request {
	out := []ctrl.Request{}
	seen := map[string]bool{}
	for _, entry := range root.Spec.Participants {
		if entry.Kind != api.ParticipantBitcoinNode {
			continue
		}
		name := foundation.ParticipantName(string(root.UID), entry.Name)
		seen[name] = true
		out = append(out, ctrl.Request{NamespacedName: client.ObjectKey{Namespace: root.Namespace, Name: name}})
	}
	for _, id := range root.Status.Identities {
		if !id.Removing {
			continue
		}
		name := foundation.ParticipantName(string(root.UID), id.Name)
		if !seen[name] {
			out = append(out, ctrl.Request{NamespacedName: client.ObjectKey{Namespace: root.Namespace, Name: name}})
		}
	}
	return out
}

// baselineAdmissionChanged refreshes public read grants without routing scheduling heartbeats.
func baselineAdmissionChanged(old, current *bitcoin.BitcoinInitialization) bool {
	if old.Status.Baseline == nil || current.Status.Baseline == nil {
		return (old.Status.Baseline == nil) != (current.Status.Baseline == nil)
	}
	return old.Status.Baseline.Scheduling.AdmissionDigest != current.Status.Baseline.Scheduling.AdmissionDigest
}
