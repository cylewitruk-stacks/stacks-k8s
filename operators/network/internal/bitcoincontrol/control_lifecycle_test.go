package bitcoincontrol

import (
	"context"
	"fmt"
	"testing"

	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

// controlFixture holds actual owner chains without simulating kubelet or SSA field ownership.
type controlFixture struct {
	root            *api.StacksNetwork
	p               *api.StacksNetworkParticipant
	deployment      *appsv1.Deployment
	replica         *appsv1.ReplicaSet
	pod             *corev1.Pod
	c               client.Client
	r               WorkloadReconciler
	writes          int
	failPublication bool
}

// newControlFixture intercepts minimal SSA payloads while fake storage models retry state only.
func newControlFixture(t *testing.T) *controlFixture {
	t.Helper()
	f := &controlFixture{}
	f.root = &api.StacksNetwork{ObjectMeta: metav1.ObjectMeta{Name: "network", Namespace: "test", UID: "root", Generation: 3}, Spec: api.StacksNetworkSpec{Operation: "Stopped"}}
	f.p = &api.StacksNetworkParticipant{ObjectMeta: metav1.ObjectMeta{Name: "participant", Namespace: "test", UID: "participant", Generation: 2, OwnerReferences: []metav1.OwnerReference{{APIVersion: api.GroupVersion.String(), Kind: "StacksNetwork", Name: "network", UID: f.root.UID, Controller: ptr.To(true)}}}, Spec: api.StacksNetworkParticipantSpec{NetworkUID: f.root.UID, ParticipantName: "core", Kind: "BitcoinNode"}}
	// The production renderer supplies the same deterministic Deployment identity.
	f.p.Status.Admission = &api.Admission{}
	f.p.Status.Runtime = &api.ParticipantRuntimeStatus{}
	scheme := runtime.NewScheme()
	for _, add := range []func(*runtime.Scheme) error{api.AddToScheme, appsv1.AddToScheme, corev1.AddToScheme} {
		if err := add(scheme); err != nil {
			t.Fatal(err)
		}
	}
	f.deployment = &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: controlDeploymentName(f.p), Namespace: "test", UID: "deployment", Generation: 4, OwnerReferences: []metav1.OwnerReference{{APIVersion: api.GroupVersion.String(), Kind: "StacksNetworkParticipant", Name: f.p.Name, UID: f.p.UID, Controller: ptr.To(true)}}}, Spec: appsv1.DeploymentSpec{Replicas: ptr.To[int32](0)}, Status: appsv1.DeploymentStatus{ObservedGeneration: 4}}
	f.replica = &appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{Name: "replica", Namespace: "test", UID: "replica", OwnerReferences: []metav1.OwnerReference{{APIVersion: "apps/v1", Kind: "Deployment", Name: f.deployment.Name, UID: f.deployment.UID, Controller: ptr.To(true)}}}}
	f.replica.Spec.Replicas = ptr.To[int32](0)
	labels := labels(f.p, "support")
	labels["network.stacks.org/worker-role"] = "bitcoin-control"
	f.pod = &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "control-pod", Namespace: "test", UID: "pod", Labels: labels, Finalizers: []string{ControlPodFinalizer}, OwnerReferences: []metav1.OwnerReference{{APIVersion: "apps/v1", Kind: "ReplicaSet", Name: f.replica.Name, UID: f.replica.UID, Controller: ptr.To(true)}}}, Spec: corev1.PodSpec{NodeName: "node", Containers: []corev1.Container{{Name: "control", Image: "control:test"}}}, Status: corev1.PodStatus{Phase: corev1.PodRunning, ContainerStatuses: []corev1.ContainerStatus{{Name: "control", State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}}}}}
	f.c = fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(f.p, f.pod, f.deployment).WithObjects(f.root, f.p, f.deployment, f.replica, f.pod).WithInterceptorFuncs(interceptor.Funcs{SubResourcePatch: func(ctx context.Context, c client.Client, sub string, obj client.Object, patch client.Patch, opts ...client.SubResourcePatchOption) error {
		f.writes++
		p := obj.(*api.StacksNetworkParticipant)
		options := &client.SubResourcePatchOptions{}
		for _, option := range opts {
			option.ApplyToSubResourcePatch(options)
		}
		if sub != "status" || patch.Type() != types.ApplyPatchType || options.FieldManager != ControlLifecycleManager || p.Status.BitcoinControl == nil || p.Status.Runtime != nil || p.Status.Admission != nil || p.Status.Execution != nil || len(p.Status.Conditions) != 0 {
			t.Fatal("control status widened SSA ownership")
		}
		if f.failPublication {
			return apierrors.NewConflict(schema.GroupResource{Group: api.GroupVersion.Group, Resource: "stacksnetworkparticipants"}, p.Name, fmt.Errorf("injected status conflict"))
		}
		var current api.StacksNetworkParticipant
		if err := c.Get(ctx, client.ObjectKeyFromObject(p), &current); err != nil {
			return err
		}
		if current.ResourceVersion != p.ResourceVersion {
			return fmt.Errorf("stale test publication")
		}
		current.Status.BitcoinControl = p.Status.BitcoinControl
		if err := c.Status().Update(ctx, &current); err != nil {
			return err
		}
		*p = current
		return nil
	}}).Build()
	f.r = WorkloadReconciler{Client: f.c, Reader: f.c}
	f.refresh(t)
	return f
}

// refresh obtains the latest participant revision before a reconcile retry.
func (f *controlFixture) refresh(t *testing.T) {
	t.Helper()
	if err := f.c.Get(context.Background(), client.ObjectKeyFromObject(f.p), f.p); err != nil {
		t.Fatal(err)
	}
}

// reconcile observes control evidence without provisioning any Kubernetes workload.
func (f *controlFixture) reconcile(t *testing.T) {
	t.Helper()
	f.refresh(t)
	if _, err := f.r.ReconcileControlLifecycle(context.Background(), f.p, f.root); err != nil {
		t.Fatal(err)
	}
}

// terminal supplies explicit kubelet process exit evidence.
func (f *controlFixture) terminal(t *testing.T) {
	t.Helper()
	if err := f.c.Get(context.Background(), client.ObjectKeyFromObject(f.pod), f.pod); err != nil {
		t.Fatal(err)
	}
	f.pod.Status.Phase = corev1.PodFailed
	f.pod.Status.ContainerStatuses[0].State = corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{Reason: "Error", ExitCode: 1}}
	if err := f.c.Status().Update(context.Background(), f.pod); err != nil {
		t.Fatal(err)
	}
}

func TestControlTerminationRequiresPublishedProcessEvidence(t *testing.T) {
	f := newControlFixture(t)
	f.reconcile(t)
	if f.p.Status.BitcoinControl.Terminated || len(f.p.Status.BitcoinControl.Pods) != 1 {
		t.Fatal("running process claimed terminated")
	}
	f.terminal(t)
	f.failPublication = true
	f.refresh(t)
	if _, err := f.r.ReconcileControlLifecycle(context.Background(), f.p, f.root); err == nil {
		t.Fatal("missing injected publication failure")
	}
	var pod corev1.Pod
	if err := f.c.Get(context.Background(), client.ObjectKeyFromObject(f.pod), &pod); err != nil {
		t.Fatal(err)
	}
	if !controllerutil.ContainsFinalizer(&pod, ControlPodFinalizer) {
		t.Fatal("lost publication released Pod evidence")
	}
	f.failPublication = false
	f.reconcile(t)
	if !f.p.Status.BitcoinControl.Terminated || !f.p.Status.BitcoinControl.Pods[0].Terminated {
		t.Fatal("terminal process not acknowledged")
	}
	if err := f.c.Get(context.Background(), client.ObjectKeyFromObject(f.pod), &pod); err != nil {
		t.Fatal(err)
	}
	if controllerutil.ContainsFinalizer(&pod, ControlPodFinalizer) {
		t.Fatal("published terminal evidence not released")
	}
	writes := f.writes
	f.reconcile(t)
	if f.writes != writes {
		t.Fatal("unchanged process evidence rewrote status")
	}
}

func TestControlMissingBoundProcessRemainsUnknown(t *testing.T) {
	f := newControlFixture(t)
	f.reconcile(t)
	if err := f.c.Get(context.Background(), client.ObjectKeyFromObject(f.pod), f.pod); err != nil {
		t.Fatal(err)
	}
	f.pod.Finalizers = nil
	if err := f.c.Update(context.Background(), f.pod); err != nil {
		t.Fatal(err)
	}
	if err := f.c.Delete(context.Background(), f.pod); err != nil {
		t.Fatal(err)
	}
	f.reconcile(t)
	if state := f.p.Status.BitcoinControl; state.Terminated || state.Reason != "TerminationUnknown" || len(state.Pods) != 1 {
		t.Fatal("missing process became termination evidence", state)
	}
}

func TestControlRejectsForeignOwnerChain(t *testing.T) {
	f := newControlFixture(t)
	f.replica.OwnerReferences[0].UID = "foreign"
	if err := f.c.Update(context.Background(), f.replica); err != nil {
		t.Fatal(err)
	}
	f.refresh(t)
	if _, err := f.r.ReconcileControlLifecycle(context.Background(), f.p, f.root); err == nil {
		t.Fatal("foreign ReplicaSet accepted")
	}
	if f.p.Status.BitcoinControl.Terminated || f.p.Status.BitcoinControl.Reason != "OwnershipConflict" {
		t.Fatal("foreign chain claimed stopped")
	}
}

func TestControlTerminationRefreshesControlGenerationWithoutRPC(t *testing.T) {
	f := newControlFixture(t)
	f.terminal(t)
	f.reconcile(t)
	f.root.Generation++
	f.reconcile(t)
	if state := f.p.Status.BitcoinControl; !state.Terminated || state.NetworkGeneration != f.root.Generation || state.ObservedGeneration != f.p.Generation {
		t.Fatal("terminal evidence did not acknowledge current controls")
	}
}

func TestControlAllDeclaredProcessesMustTerminate(t *testing.T) {
	f := newControlFixture(t)
	f.terminal(t)
	pod := f.pod.DeepCopy()
	pod.Spec.InitContainers = []corev1.Container{{Name: "init"}}
	if controlPodTerminated(pod) {
		t.Fatal("missing init termination accepted")
	}
	pod.Status.InitContainerStatuses = []corev1.ContainerStatus{{Name: "init", State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{Reason: "Completed"}}}}
	if !controlPodTerminated(pod) {
		t.Fatal("complete process exits rejected")
	}
	pod.Status.ContainerStatuses[0].State.Terminated.Reason = "ContainerStatusUnknown"
	if controlPodTerminated(pod) {
		t.Fatal("unknown process state accepted")
	}
	pod.Spec.NodeName = ""
	pod.DeletionTimestamp = ptr.To(metav1.Now())
	pod.Status.ContainerStatuses = nil
	pod.Status.InitContainerStatuses = nil
	if !controlPodTerminated(pod) {
		t.Fatal("never-scheduled deleting Pod did not prove nonactivation")
	}
}

func TestControlLifecycleRoutingFollowsOwnerChain(t *testing.T) {
	f := newControlFixture(t)
	requests := f.r.ControlLifecycleRequests(context.Background(), f.pod)
	if len(requests) != 1 || requests[0].Name != f.p.Name {
		t.Fatal("Pod owner chain did not route participant", requests)
	}
	old := f.deployment.DeepCopy()
	current := old.DeepCopy()
	current.Status.ObservedGeneration++
	if !ControlLifecycleEvents().Update(event.UpdateEvent{ObjectOld: old, ObjectNew: current}) {
		t.Fatal("observed scale-down was filtered")
	}
	current = old.DeepCopy()
	current.ResourceVersion = "new"
	if ControlLifecycleEvents().Update(event.UpdateEvent{ObjectOld: old, ObjectNew: current}) {
		t.Fatal("identical heartbeat routed")
	}
}

func TestControlDeploymentReplacementRetainsPriorPodIdentity(t *testing.T) {
	f := newControlFixture(t)
	f.reconcile(t)
	if err := f.c.Delete(context.Background(), f.deployment); err != nil {
		t.Fatal(err)
	}
	f.deployment.UID = "replacement"
	f.deployment.ResourceVersion = ""
	if err := f.c.Create(context.Background(), f.deployment); err != nil {
		t.Fatal(err)
	}
	f.reconcile(t)
	state := f.p.Status.BitcoinControl
	if state.DeploymentRef.UID != "replacement" || len(state.Pods) != 1 || state.Pods[0].Deployment.UID != "deployment" || state.Terminated {
		t.Fatal("replacement erased predecessor evidence", state)
	}
}

func TestControlNeverCreatedAcknowledgesNonactivation(t *testing.T) {
	f := newControlFixture(t)
	f.pod.Finalizers = nil
	if err := f.c.Update(context.Background(), f.pod); err != nil {
		t.Fatal(err)
	}
	for _, object := range []client.Object{f.pod, f.replica, f.deployment} {
		if err := f.c.Delete(context.Background(), object); err != nil {
			t.Fatal(err)
		}
	}
	f.reconcile(t)
	if state := f.p.Status.BitcoinControl; !state.Terminated || state.DeploymentRef != nil || len(state.Pods) != 0 {
		t.Fatal("never-created control did not acknowledge absence", state)
	}
}

func TestControlRetainedReplicaCannotCreateAfterTermination(t *testing.T) {
	f := newControlFixture(t)
	f.terminal(t)
	f.replica.Spec.Replicas = ptr.To[int32](1)
	if err := f.c.Update(context.Background(), f.replica); err != nil {
		t.Fatal(err)
	}
	f.reconcile(t)
	if f.p.Status.BitcoinControl.Terminated {
		t.Fatal("retained ReplicaSet could create a new process")
	}
}

func TestControlUnreachableProcessRemainsUnknown(t *testing.T) {
	f := newControlFixture(t)
	f.pod.Status.Conditions = []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionUnknown, Reason: "NodeNotReady"}}
	if err := f.c.Status().Update(context.Background(), f.pod); err != nil {
		t.Fatal(err)
	}
	f.reconcile(t)
	if state := f.p.Status.BitcoinControl; state.Terminated || state.Reason != "TerminationUnknown" {
		t.Fatal("unreachable process claimed observed shutdown", state)
	}
}

func TestControlForegroundDestructionRequiresConfirmedTermination(t *testing.T) {
	for _, mode := range []string{"destroying", "stopped", "running-pod", "missing-pod", "remaining-controller"} {
		t.Run(mode, func(t *testing.T) {
			f := newControlFixture(t)
			f.replica.Labels = f.pod.Labels
			if err := f.c.Update(context.Background(), f.replica); err != nil {
				t.Fatal(err)
			}
			f.reconcile(t)
			if mode != "stopped" {
				f.root.DeletionTimestamp = ptr.To(metav1.Now())
			}
			if mode != "running-pod" && mode != "missing-pod" {
				f.terminal(t)
			}
			if err := f.c.Delete(context.Background(), f.deployment); err != nil {
				t.Fatal(err)
			}
			if mode != "remaining-controller" {
				if err := f.c.Delete(context.Background(), f.replica); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "missing-pod" {
				if err := f.c.Get(context.Background(), client.ObjectKeyFromObject(f.pod), f.pod); err != nil {
					t.Fatal(err)
				}
				f.pod.Finalizers = nil
				if err := f.c.Update(context.Background(), f.pod); err != nil {
					t.Fatal(err)
				}
				if err := f.c.Delete(context.Background(), f.pod); err != nil {
					t.Fatal(err)
				}
			}
			f.refresh(t)
			_, err := f.r.ReconcileControlLifecycle(context.Background(), f.p, f.root)
			if err != nil && mode != "running-pod" {
				t.Fatal(err)
			}
			state := f.p.Status.BitcoinControl
			if state.Terminated != (mode == "destroying") {
				t.Fatalf("unexpected termination for %s: %+v", mode, state)
			}
			if mode == "destroying" && (state.Reason != "Terminated" || len(state.Pods) != 1 || !state.Pods[0].Terminated) {
				t.Fatalf("destruction lost exact process evidence: %+v", state)
			}
		})
	}
}
