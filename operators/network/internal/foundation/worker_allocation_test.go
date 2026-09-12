package foundation

import (
	"testing"

	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// unrecordedWorker supplies the exact observable gap after CREATE and before UID publication.
func unrecordedWorker() (*api.StacksNetwork, *api.StacksNetworkParticipant) {
	root := &api.StacksNetwork{ObjectMeta: metav1.ObjectMeta{Name: "network", Namespace: "test", UID: "root", Generation: 1, Finalizers: []string{foundationFinalizer}}, Spec: api.StacksNetworkSpec{Operation: "Running", Participants: []api.Participant{{Name: "worker", Kind: "StacksFaucet"}}}}
	p := &api.StacksNetworkParticipant{ObjectMeta: metav1.ObjectMeta{Name: ParticipantName(string(root.UID), "worker"), Namespace: root.Namespace, UID: "participant", Generation: 1, OwnerReferences: []metav1.OwnerReference{{APIVersion: api.GroupVersion.String(), Kind: "StacksNetwork", Name: root.Name, UID: root.UID, Controller: ptr.To(true)}}}, Spec: api.StacksNetworkParticipantSpec{NetworkUID: root.UID, ParticipantName: "worker", Kind: "StacksFaucet"}}
	return root, p
}

func TestPendingWorkerAllocationExcludesHistoryAndForeignIdentity(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*api.StacksNetwork, *api.StacksNetworkParticipant)
		pending bool
	}{
		{"new", func(_ *api.StacksNetwork, _ *api.StacksNetworkParticipant) {}, true},
		{"allocation observation", func(_ *api.StacksNetwork, p *api.StacksNetworkParticipant) {
			p.Status.Runtime = &api.ParticipantRuntimeStatus{ObservedGeneration: 1}
		}, true},
		{"stopped", func(root *api.StacksNetwork, _ *api.StacksNetworkParticipant) { root.Spec.Operation = "Stopped" }, true},
		{"deleting", func(root *api.StacksNetwork, p *api.StacksNetworkParticipant) {
			now := metav1.Now()
			root.DeletionTimestamp = &now
			p.DeletionTimestamp = &now
		}, true},
		{"finalizer", func(_ *api.StacksNetwork, p *api.StacksNetworkParticipant) {
			p.Finalizers = []string{"network.stacks.org/stacks-worker"}
		}, false},
		{"admission", func(_ *api.StacksNetwork, p *api.StacksNetworkParticipant) { p.Status.Admission = &api.Admission{} }, false},
		{"execution", func(_ *api.StacksNetwork, p *api.StacksNetworkParticipant) {
			p.Status.Execution = &api.WorkerExecutionStatus{Phase: "Inactive"}
		}, false},
		{"candidate", func(_ *api.StacksNetwork, p *api.StacksNetworkParticipant) {
			p.Status.Runtime = &api.ParticipantRuntimeStatus{WorkerCandidate: &api.WorkerCandidate{}}
		}, false},
		{"pod", func(_ *api.StacksNetwork, p *api.StacksNetworkParticipant) {
			p.Status.Runtime = &api.ParticipantRuntimeStatus{PodRef: &common.Binding{Kind: "Pod", Name: "worker", UID: "pod"}}
		}, false},
		{"workload", func(_ *api.StacksNetwork, p *api.StacksNetworkParticipant) {
			p.Status.Runtime = &api.ParticipantRuntimeStatus{WorkloadRefs: []common.Binding{{Kind: "Pod", Name: "worker", UID: "pod"}}}
		}, false},
		{"config", func(_ *api.StacksNetwork, p *api.StacksNetworkParticipant) {
			p.Status.Runtime = &api.ParticipantRuntimeStatus{ConfigRef: &common.Binding{Kind: "ConfigMap", Name: "profile", UID: "config"}}
		}, false},
		{"process", func(_ *api.StacksNetwork, p *api.StacksNetworkParticipant) {
			p.Status.Runtime = &api.ParticipantRuntimeStatus{ContainerID: "process"}
		}, false},
		{"terminated", func(_ *api.StacksNetwork, p *api.StacksNetworkParticipant) {
			p.Status.Runtime = &api.ParticipantRuntimeStatus{Terminated: true}
		}, false},
		{"known identity", func(root *api.StacksNetwork, p *api.StacksNetworkParticipant) {
			root.Status.Identities = []api.InstanceIdentity{{Name: p.Spec.ParticipantName, UID: p.UID}}
		}, false},
		{"different UID", func(root *api.StacksNetwork, p *api.StacksNetworkParticipant) {
			root.Status.Identities = []api.InstanceIdentity{{Name: p.Spec.ParticipantName, UID: "lost"}}
		}, false},
		{"foreign owner version", func(_ *api.StacksNetwork, p *api.StacksNetworkParticipant) {
			p.OwnerReferences[0].APIVersion = "network.stacks.org/v1alpha1"
		}, false},
		{"foreign owner kind", func(_ *api.StacksNetwork, p *api.StacksNetworkParticipant) { p.OwnerReferences[0].Kind = "Other" }, false},
		{"foreign owner name", func(_ *api.StacksNetwork, p *api.StacksNetworkParticipant) { p.OwnerReferences[0].Name = "other" }, false},
		{"foreign owner UID", func(_ *api.StacksNetwork, p *api.StacksNetworkParticipant) { p.OwnerReferences[0].UID = "other" }, false},
		{"not selected", func(root *api.StacksNetwork, _ *api.StacksNetworkParticipant) { root.Spec.Participants = nil }, false},
		{"nonworker", func(root *api.StacksNetwork, p *api.StacksNetworkParticipant) {
			p.Spec.Kind = "BitcoinNode"
			root.Spec.Participants[0].Kind = p.Spec.Kind
		}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root, p := unrecordedWorker()
			tc.mutate(root, p)
			if got := PendingWorkerAllocation(root, p); got != tc.pending {
				t.Fatalf("pending=%v, want %v", got, tc.pending)
			}
		})
	}
}

func TestFirstWorkerLedgerPublicationCannotReconstructHistory(t *testing.T) {
	for _, mode := range []string{"new", "observed", "admitted", "candidate", "execution", "pod", "workload", "finalizer", "actor-finalizer"} {
		t.Run(mode, func(t *testing.T) {
			root, p := unrecordedWorker()
			switch mode {
			case "observed":
				p.Status.Runtime = &api.ParticipantRuntimeStatus{ObservedGeneration: 1}
			case "admitted":
				p.Status.Admission = &api.Admission{}
			case "candidate":
				p.Status.Runtime = &api.ParticipantRuntimeStatus{WorkerCandidate: &api.WorkerCandidate{}}
			case "execution":
				p.Status.Execution = &api.WorkerExecutionStatus{}
			case "pod":
				p.Status.Runtime = &api.ParticipantRuntimeStatus{PodRef: &common.Binding{Kind: "Pod", Name: "worker", UID: "old"}}
			case "workload":
				p.Status.Runtime = &api.ParticipantRuntimeStatus{WorkloadRefs: []common.Binding{{Kind: "Pod", Name: "worker", UID: "old"}}}
			case "finalizer":
				p.Finalizers = []string{"network.stacks.org/stacks-worker"}
			case "actor-finalizer":
				p.Spec.Kind = "BitcoinNode"
				root.Spec.Participants[0].Kind = p.Spec.Kind
				p.Finalizers = []string{"network.stacks.org/bitcoin-workload"}
			}
			scheme := runtime.NewScheme()
			_ = api.AddToScheme(scheme)
			c := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&api.StacksNetwork{}, &api.StacksNetworkParticipant{}).WithObjects(root, p).Build()
			r := Reconciler{Client: c, Reader: c, Scheme: scheme}
			if _, err := r.reconcileTopology(t.Context(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(root)}); err != nil {
				t.Fatal(err)
			}
			actual := &api.StacksNetwork{}
			if err := c.Get(t.Context(), client.ObjectKeyFromObject(root), actual); err != nil {
				t.Fatal(err)
			}
			admitted := mode == "new" || mode == "observed" || mode == "actor-finalizer"
			if admitted {
				if len(actual.Status.Identities) != 1 || actual.Status.Identities[0].UID != p.UID || actual.Status.Identities[0].Worker != nil || meta.IsStatusConditionTrue(actual.Status.Conditions, "Failed") {
					t.Fatalf("first publication: %+v", actual.Status)
				}
			} else if len(actual.Status.Identities) != 0 || !meta.IsStatusConditionTrue(actual.Status.Conditions, "Failed") {
				t.Fatalf("lost history reconstructed: %+v", actual.Status)
			}
			current := &api.StacksNetworkParticipant{}
			if err := c.Get(t.Context(), client.ObjectKeyFromObject(p), current); err != nil {
				t.Fatal(err)
			}
			if mode != "admitted" && current.Status.Admission != nil {
				t.Fatal("policy admitted before ledger publication completed")
			}
		})
	}
}

func TestDeletingRootRemovesUnrecordedUnstartedWorker(t *testing.T) {
	root, p := unrecordedWorker()
	now := metav1.Now()
	root.DeletionTimestamp = &now
	scheme := runtime.NewScheme()
	_ = api.AddToScheme(scheme)
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(root, p).Build()
	r := Reconciler{Client: c, Reader: c, Scheme: scheme}
	request := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(root)}
	if _, err := r.reconcileTopology(t.Context(), request); err != nil {
		t.Fatal(err)
	}
	current := &api.StacksNetworkParticipant{}
	if err := c.Get(t.Context(), client.ObjectKeyFromObject(p), current); !apierrors.IsNotFound(err) {
		t.Fatalf("unrecorded participant was not deleted: %v", err)
	}
	if _, err := r.reconcileTopology(t.Context(), request); err != nil {
		t.Fatal(err)
	}
	actual := &api.StacksNetwork{}
	if err := c.Get(t.Context(), client.ObjectKeyFromObject(root), actual); err == nil && len(actual.Finalizers) > 0 {
		t.Fatal("root cleanup retained finalizer after unstarted participant removal")
	} else if err != nil && !apierrors.IsNotFound(err) {
		t.Fatal(err)
	}
}
