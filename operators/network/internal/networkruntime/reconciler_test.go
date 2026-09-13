package networkruntime

import (
	"context"
	"testing"

	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestStopRequiresCurrentTerminationEvidence(t *testing.T) {
	for _, tc := range []struct {
		name       string
		terminated bool
		observed   int64
		want       string
	}{
		{"live process", false, 2, "Stopping"}, {"stale termination", true, 1, "Stopping"}, {"confirmed termination", true, 2, "Stopped"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := rootFixture()
			root.Spec.Operation = "Stopped"
			p := participantFixture(root)
			p.Status.Runtime = &api.ParticipantRuntimeStatus{ObservedGeneration: tc.observed, Terminated: tc.terminated, WorkloadRefs: []common.Binding{{Kind: "StatefulSet", Name: "node", UID: "sts"}}}
			r := fixtureReconciler(t, p)
			if _, err := r.Reconcile(context.Background(), root); err != nil {
				t.Fatal(err)
			}
			if string(root.Status.Phase) != tc.want || !meta.IsStatusConditionFalse(root.Status.Conditions, "Running") {
				t.Fatalf("incorrect shutdown state: %+v", root.Status)
			}
		})
	}
}

func TestPreactivationPauseIsUninitialized(t *testing.T) {
	root := rootFixture()
	root.Spec.Operation = "Paused"
	set(root, "Resolved", metav1.ConditionTrue, "InputsResolved", "inputs resolved")
	r := fixtureReconciler(t)
	if _, err := r.Reconcile(context.Background(), root); err != nil {
		t.Fatal(err)
	}
	if root.Status.Phase != "Uninitialized" || !meta.IsStatusConditionFalse(root.Status.Conditions, "Initialized") || !meta.IsStatusConditionFalse(root.Status.Conditions, "Running") {
		t.Fatalf("pause became a runtime claim: %+v", root.Status)
	}
}

func TestStopPreservesFailureAndInitialization(t *testing.T) {
	root := rootFixture()
	root.Spec.Operation = "Stopped"
	root.Status.Phase = "Failed"
	set(root, "Initialized", metav1.ConditionTrue, "GatesComplete", "fixed gates completed")
	r := fixtureReconciler(t)
	if _, err := r.Reconcile(context.Background(), root); err != nil {
		t.Fatal(err)
	}
	if root.Status.Phase != "Stopped" || !meta.IsStatusConditionTrue(root.Status.Conditions, "Failed") || !meta.IsStatusConditionTrue(root.Status.Conditions, "Initialized") {
		t.Fatalf("historical facts lost: %+v", root.Status)
	}
}

func TestForeignParticipantDoesNotHoldShutdown(t *testing.T) {
	root := rootFixture()
	root.Spec.Operation = "Stopped"
	p := participantFixture(root)
	p.OwnerReferences[0].UID = "foreign"
	p.Status.Runtime = &api.ParticipantRuntimeStatus{WorkloadRefs: []common.Binding{{Kind: "StatefulSet", Name: "foreign", UID: "foreign"}}}
	r := fixtureReconciler(t, p)
	if _, err := r.Reconcile(context.Background(), root); err != nil {
		t.Fatal(err)
	}
	if root.Status.Phase != "Stopped" {
		t.Fatal(root.Status.Phase)
	}
}

// rootFixture provides a network incarnation without captured genesis.
func rootFixture() *api.StacksNetwork {
	return &api.StacksNetwork{ObjectMeta: metav1.ObjectMeta{Name: "network", Namespace: "test", UID: "root", Generation: 3}}
}

// participantFixture provides an owned runtime instance with a current generation.
func participantFixture(root *api.StacksNetwork) *api.StacksNetworkParticipant {
	return &api.StacksNetworkParticipant{ObjectMeta: metav1.ObjectMeta{Name: "node", Namespace: root.Namespace, UID: "participant", Generation: 2, OwnerReferences: []metav1.OwnerReference{{APIVersion: api.GroupVersion.String(), Kind: "StacksNetwork", Name: root.Name, UID: root.UID, Controller: ptr.To(true)}}}, Spec: api.StacksNetworkParticipantSpec{Kind: "BitcoinNode", NetworkUID: root.UID, ParticipantName: "btc"}, Status: api.ParticipantStatus{BitcoinControl: &api.BitcoinControlRuntimeStatus{ObservedGeneration: 2, NetworkGeneration: root.Generation, Terminated: true}}}
}

// fixtureReconciler isolates lifecycle projection from runtime allocation.
func fixtureReconciler(t *testing.T, objects ...client.Object) *Reconciler {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := api.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := bitcoin.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
	return &Reconciler{Client: c, Reader: c, Scheme: scheme}
}

func TestStoppedRequiresAcknowledgementWithoutPublishedWorkloadBinding(t *testing.T) {
	for _, ack := range []bool{false, true} {
		root := rootFixture()
		root.Spec.Operation = "Stopped"
		p := participantFixture(root)
		p.Status.Runtime = &api.ParticipantRuntimeStatus{ObservedGeneration: p.Generation, Terminated: ack}
		r := fixtureReconciler(t, p)
		if _, err := r.Reconcile(context.Background(), root); err != nil {
			t.Fatal(err)
		}
		want := "Stopping"
		if ack {
			want = "Stopped"
		}
		if string(root.Status.Phase) != want {
			t.Fatalf("termination acknowledgement=%v phase=%s", ack, root.Status.Phase)
		}
	}
}

func TestPreparationFailureLatchesWithoutClaimingFullInitialization(t *testing.T) {
	root := rootFixture()
	record := &bitcoin.BitcoinInitialization{Status: bitcoin.BitcoinInitializationStatus{Reason: "PrepareBitcoinObservationDeadline"}}
	if !projectPreparation(root, record) || root.Status.Phase != "Failed" || !meta.IsStatusConditionTrue(root.Status.Conditions, "Failed") {
		t.Fatal("initialization deadline did not latch failure")
	}
	root = rootFixture()
	now := metav1.Now()
	record.Status = bitcoin.BitcoinInitializationStatus{PreparedAt: &now, Reason: "NextGateNotImplemented"}
	if projectPreparation(root, record) || !meta.IsStatusConditionTrue(root.Status.Conditions, "BitcoinPrepared") || meta.IsStatusConditionTrue(root.Status.Conditions, "Initialized") || meta.IsStatusConditionTrue(root.Status.Conditions, "Running") {
		t.Fatal("Bitcoin preparation claimed full initialization")
	}
}

func TestStoppedRequiresControlTerminationAndAllocatedInventory(t *testing.T) {
	for _, mode := range []string{"missing-control", "live-control", "stale-control", "missing-participant"} {
		t.Run(mode, func(t *testing.T) {
			root := rootFixture()
			root.Spec.Operation = "Stopped"
			p := participantFixture(root)
			p.Status.Runtime = &api.ParticipantRuntimeStatus{ObservedGeneration: p.Generation, Terminated: true}
			switch mode {
			case "missing-control":
				p.Status.BitcoinControl = nil
			case "live-control":
				p.Status.BitcoinControl.Terminated = false
			case "stale-control":
				p.Status.BitcoinControl.NetworkGeneration--
			case "missing-participant":
				root.Status.Identities = []api.InstanceIdentity{{Name: "lost", UID: "missing"}}
			}
			if _, err := fixtureReconciler(t, p).Reconcile(context.Background(), root); err != nil {
				t.Fatal(err)
			}
			if root.Status.Phase != "Stopping" {
				t.Fatalf("missing termination evidence claimed %s", root.Status.Phase)
			}
		})
	}
}
