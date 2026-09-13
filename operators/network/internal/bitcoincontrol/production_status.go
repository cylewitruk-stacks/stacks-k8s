package bitcoincontrol

import (
	"context"
	"fmt"

	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/foundation"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/participantstatus"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

// ProductionFieldManager owns only production participant runtime and workload readiness.
const ProductionFieldManager = "stacks-network-domain-bitcoinblockproduction"

// ProductionStatusReconciler projects scheduler availability without writing admission.
type ProductionStatusReconciler struct {
	// Client publishes minimal participant status with a fixed field manager.
	Client client.Client
	// Reader validates current root, participant and scheduler bindings.
	Reader client.Reader
}

// SetupWithManager installs the resource-focused Bitcoin production status owner.
func (r *ProductionStatusReconciler) SetupWithManager(m ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(m).Named("bitcoin-production-status-v1alpha2").For(&api.StacksNetworkParticipant{}, builder.WithPredicates(participantWorkloadEvents())).Watches(&bitcoin.BitcoinInitialization{}, handler.EnqueueRequestsFromMapFunc(func(_ context.Context, obj client.Object) []ctrl.Request {
		record := obj.(*bitcoin.BitcoinInitialization)
		return []ctrl.Request{{NamespacedName: client.ObjectKey{Namespace: record.Namespace, Name: record.Spec.Production.Name}}, {NamespacedName: client.ObjectKey{Namespace: record.Namespace, Name: productionBinding(record).Name}}}
	}), builder.WithPredicates(predicate.Funcs{UpdateFunc: func(e event.UpdateEvent) bool {
		old, ok := e.ObjectOld.(*bitcoin.BitcoinInitialization)
		current, valid := e.ObjectNew.(*bitcoin.BitcoinInitialization)
		return !ok || !valid || old.UID != current.UID || !equality.Semantic.DeepEqual(old.DeletionTimestamp, current.DeletionTimestamp) || old.Status.Phase != current.Status.Phase || old.Status.Reason != current.Status.Reason || !equality.Semantic.DeepEqual(old.Status.Production, current.Status.Production)
	}})).Watches(&api.StacksNetwork{}, handler.EnqueueRequestsFromMapFunc(func(_ context.Context, obj client.Object) []ctrl.Request {
		root := obj.(*api.StacksNetwork)
		out := []ctrl.Request{}
		for _, entry := range root.Spec.Participants {
			if entry.Kind == "BitcoinBlockProduction" {
				out = append(out, ctrl.Request{NamespacedName: client.ObjectKey{Namespace: root.Namespace, Name: foundation.ParticipantName(string(root.UID), entry.Name)}})
			}
		}
		return out
	}), builder.WithPredicates(rootWorkloadEvents())).Complete(r)
}

// Reconcile reports scheduler/resource readiness independently of protocol completion.
func (r *ProductionStatusReconciler) Reconcile(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {
	p := &api.StacksNetworkParticipant{}
	if e := r.Reader.Get(ctx, request.NamespacedName, p); e != nil {
		return ctrl.Result{}, client.IgnoreNotFound(e)
	}
	if p.Spec.Kind != "BitcoinBlockProduction" {
		return ctrl.Result{}, nil
	}
	root := &api.StacksNetwork{}
	if e := r.Reader.Get(ctx, client.ObjectKey{Namespace: p.Namespace, Name: "network"}, root); e != nil {
		return ctrl.Result{}, e
	}
	if root.UID != p.Spec.NetworkUID || !metav1.IsControlledBy(p, root) {
		return ctrl.Result{}, fmt.Errorf("production root binding unavailable")
	}
	phase, reason := "Waiting", "SchedulerNotEnrolled"
	if stopReason(root, p) != "" {
		phase, reason = "Abandoned", "DesiredStop"
	} else if root.Status.Bitcoin != nil && root.Status.Bitcoin.InitializationRef != nil {
		ref := root.Status.Bitcoin.InitializationRef
		initial := &bitcoin.BitcoinInitialization{}
		if e := r.Reader.Get(ctx, client.ObjectKey{Namespace: p.Namespace, Name: ref.Name}, initial); e != nil {
			return ctrl.Result{}, e
		}
		if initial.UID != ref.UID || initial.Spec.NetworkUID != root.UID || productionBinding(initial).UID != p.UID || !metav1.IsControlledBy(initial, root) {
			return ctrl.Result{}, fmt.Errorf("production scheduler binding unavailable")
		}
		phase, reason = initial.Status.Phase, initial.Status.Reason
	}
	desired := productionStatus(p, phase, reason)
	existing := api.ParticipantStatus{Runtime: p.Status.Runtime, Conditions: productionConditions(p.Status.Conditions)}
	if equality.Semantic.DeepEqual(existing, desired) {
		return ctrl.Result{}, nil
	}
	return ctrl.Result{}, participantstatus.Apply(ctx, r.Client, p, desired, ProductionFieldManager)
}

// productionStatus constructs only domain-owned fields and preserves condition transitions.
func productionStatus(p *api.StacksNetworkParticipant, phase, reason string) api.ParticipantStatus {
	runtime := &api.ParticipantRuntimeStatus{ObservedGeneration: p.Generation, Terminated: phase == "Abandoned"}
	if p.Status.Admission != nil {
		runtime.PolicyDigest = p.Status.Admission.PolicyDigest
	}
	conditions := productionConditions(p.Status.Conditions)
	ready := metav1.ConditionFalse
	if phase == "Preparing" || phase == "Paused" || phase == "Held" {
		ready = metav1.ConditionTrue
	}
	if reason == "" {
		reason = "SchedulerWaiting"
	}
	meta.SetStatusCondition(&conditions, metav1.Condition{Type: "WorkloadReady", Status: ready, Reason: reason, Message: "Bitcoin production scheduler state: " + phase, ObservedGeneration: p.Generation})
	return api.ParticipantStatus{Runtime: runtime, Conditions: conditions}
}

// productionConditions selects only the production domain's fixed condition ownership.
func productionConditions(all []metav1.Condition) []metav1.Condition {
	out := []metav1.Condition{}
	for _, condition := range all {
		if condition.Type == "WorkloadReady" {
			out = append(out, condition)
		}
	}
	return out
}
