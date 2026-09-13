package bitcoincontrol

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

func TestWorkloadEventsIgnoreCounterTrafficAndRetainIdentityChanges(t *testing.T) {
	f := newFixture(t)
	current := f.root.DeepCopy()
	current.Status.Phase = "Initializing"
	if rootWorkloadEvents().Update(event.UpdateEvent{ObjectOld: f.root, ObjectNew: current}) {
		t.Fatal("aggregate preparation observation enqueued workload render")
	}
	current.Status.Bitcoin.ExecutionRefs[0].UID = "new-record"
	if !rootWorkloadEvents().Update(event.UpdateEvent{ObjectOld: f.root, ObjectNew: current}) {
		t.Fatal("record enrollment change ignored")
	}
	p := f.node.DeepCopy()
	p.Status.Conditions = []metav1.Condition{{Type: "Operational", Status: metav1.ConditionTrue}}
	if participantWorkloadEvents().Update(event.UpdateEvent{ObjectOld: f.node, ObjectNew: p}) {
		t.Fatal("unowned condition enqueued worker render")
	}
	p.Status.Runtime.ContainerID = "new-container"
	if !participantWorkloadEvents().Update(event.UpdateEvent{ObjectOld: f.node, ObjectNew: p}) {
		t.Fatal("actor identity change ignored")
	}
	pod := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Generation: 1}}
	updated := pod.DeepCopy()
	updated.Status.ReadyReplicas = 1
	if ownedWorkloadEvents().Update(event.UpdateEvent{ObjectOld: pod, ObjectNew: updated}) {
		t.Fatal("deployment readiness enqueued render")
	}
	updated.Generation++
	if !ownedWorkloadEvents().Update(event.UpdateEvent{ObjectOld: pod, ObjectNew: updated}) {
		t.Fatal("deployment spec drift ignored")
	}
	role := &rbacv1.Role{}
	changed := role.DeepCopy()
	changed.Rules = []rbacv1.PolicyRule{{Verbs: []string{"*"}}}
	if !ownedWorkloadEvents().Update(event.UpdateEvent{ObjectOld: role, ObjectNew: changed}) {
		t.Fatal("delegated role drift ignored")
	}
}

func TestRootWorkloadRoutingUsesCapturedNamesWithoutReader(t *testing.T) {
	f := newFixture(t)
	r := &WorkloadReconciler{}
	requests := r.enqueue(context.Background(), f.root)
	if len(requests) != 1 || requests[0].Namespace != f.root.Namespace {
		t.Fatal("root routing required an inventory read")
	}
}

func TestProductionProjectionOwnsNoAdmission(t *testing.T) {
	f := newFixture(t)
	f.production.Status.Conditions = []metav1.Condition{
		{Type: "Resolved", Status: metav1.ConditionTrue, Reason: "Admitted", LastTransitionTime: metav1.Now()},
	}
	status := productionStatus(f.production, "Held", "NextGateNotImplemented")
	if status.Admission != nil || status.Runtime == nil ||
		status.Runtime.PolicyDigest != f.production.Status.Admission.PolicyDigest ||
		len(status.Conditions) != 1 ||
		status.Conditions[0].Type != "WorkloadReady" ||
		status.Conditions[0].Status != metav1.ConditionTrue {
		t.Fatal("production projection changed admission or misreported held scheduler")
	}
	if status.Runtime.Terminated {
		t.Fatal("protocol hold treated as termination")
	}
	if state := productionStatus(
		f.production,
		"Waiting",
		"WalletsPreparing",
	); state.Conditions[0].Status != metav1.ConditionFalse {
		t.Fatal("unready targets claimed ready")
	}
}

// writeCounter detects unnecessary workload mutations on unchanged inputs.
type writeCounter struct {
	client.Client
	patches   int
	lastPatch string
}

func (c *writeCounter) Patch(
	ctx context.Context,
	obj client.Object,
	patch client.Patch,
	opts ...client.PatchOption,
) error {
	c.patches++
	raw, _ := patch.Data(obj)
	c.lastPatch = string(raw)
	return c.Client.Patch(ctx, obj, patch, opts...)
}

func TestWorkerWorkloadRepairsDriftAndAvoidsUnchangedPatches(t *testing.T) {
	f := newFixture(t)
	c := &writeCounter{Client: f.c}
	r := &WorkloadReconciler{Client: c, Reader: c, Image: "worker:test"}
	request := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(f.node)}
	if _, e := r.Reconcile(context.Background(), request); e != nil {
		t.Fatal(e)
	}
	c.patches = 0
	if _, e := r.Reconcile(context.Background(), request); e != nil {
		t.Fatal(e)
	}
	if c.patches != 0 {
		t.Fatalf("unchanged resources patched %d times", c.patches)
	}
	resources, e := WorkerResources(f.node, f.record, f.initial, r.Image, 1)
	if e != nil {
		t.Fatal(e)
	}
	deployment := resources[len(resources)-1].(*appsv1.Deployment)
	current := &appsv1.Deployment{}
	if e = f.c.Get(context.Background(), client.ObjectKeyFromObject(deployment), current); e != nil {
		t.Fatal(e)
	}
	current.Spec.Template.Spec.Containers[0].Image = "drift"
	current.Spec.Strategy.Type = appsv1.RollingUpdateDeploymentStrategyType
	if e = f.c.Update(context.Background(), current); e != nil {
		t.Fatal(e)
	}
	if _, e = r.Reconcile(context.Background(), request); e != nil {
		t.Fatal(e)
	}
	if e = f.c.Get(context.Background(), client.ObjectKeyFromObject(deployment), current); e != nil {
		t.Fatal(e)
	}
	if current.Spec.Template.Spec.Containers[0].Image != r.Image ||
		current.Spec.Strategy.Type != appsv1.RecreateDeploymentStrategyType {
		t.Fatal("worker drift not repaired")
	}
	if e = f.c.Delete(context.Background(), current); e != nil {
		t.Fatal(e)
	}
	if _, e = r.Reconcile(context.Background(), request); e != nil {
		t.Fatal(e)
	}
	if e = f.c.Get(context.Background(), client.ObjectKeyFromObject(deployment), current); e != nil {
		t.Fatal("deleted worker Deployment not recreated")
	}
}
