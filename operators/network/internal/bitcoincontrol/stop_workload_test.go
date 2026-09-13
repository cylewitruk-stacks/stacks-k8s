package bitcoincontrol

import (
	"context"
	"reflect"
	"testing"

	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// TestControlStopNeedsNoGenesisOrPolicyInputs verifies terminal scale-down uses retained drain evidence only.
func TestControlStopNeedsNoGenesisOrPolicyInputs(t *testing.T) {
	ctx := context.Background()
	scheme := runtime.NewScheme()
	for _, add := range []func(*runtime.Scheme) error{api.AddToScheme, bitcoin.AddToScheme, appsv1.AddToScheme} {
		if err := add(scheme); err != nil {
			t.Fatal(err)
		}
	}
	root := &api.StacksNetwork{
		ObjectMeta: metav1.ObjectMeta{Name: "network", Namespace: "test", UID: "root", Generation: 2},
		Spec:       api.StacksNetworkSpec{Operation: "Stopped"},
		Status: api.StacksNetworkStatus{
			Bitcoin: &api.BitcoinRuntimeStatus{ExecutionRefs: []common.Binding{{Name: "record", UID: "record"}}},
		},
	}
	p := &api.StacksNetworkParticipant{
		ObjectMeta: metav1.ObjectMeta{Name: "p", Namespace: "test", UID: "p"},
		Spec: api.StacksNetworkParticipantSpec{
			NetworkUID:      root.UID,
			ParticipantName: "btc",
			Kind:            "BitcoinNode",
		},
	}
	d := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      controlDeploymentName(p),
			Namespace: p.Namespace,
			UID:       "d",
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: api.GroupVersion.String(),
					Kind:       "StacksNetworkParticipant",
					Name:       p.Name,
					UID:        p.UID,
					Controller: ptr.To(true),
				},
			},
		},
		Spec: appsv1.DeploymentSpec{Replicas: ptr.To[int32](1)},
	}
	record := &bitcoin.BitcoinExecution{
		ObjectMeta: metav1.ObjectMeta{Name: "record", Namespace: "test", UID: "record"},
		Spec:       bitcoin.BitcoinExecutionSpec{Participant: common.Binding{UID: p.UID}},
		Status:     bitcoin.BitcoinExecutionStatus{Observation: &bitcoin.BitcoinObservation{}},
	}
	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(record).
		WithObjects(root, p, d, record).
		Build()
	r := WorkloadReconciler{Client: c, Reader: c}
	if _, err := r.stopControlWorkload(ctx, root, p); err != nil {
		t.Fatal(err)
	}
	if err := c.Get(ctx, client.ObjectKeyFromObject(d), d); err != nil {
		t.Fatal(err)
	}
	if *d.Spec.Replicas != 1 {
		t.Fatal("stopped before drain acknowledgement")
	}
	record.Status.Drain = &bitcoin.BitcoinDrainStatus{
		ProcessNonce:      "process",
		NetworkGeneration: root.Generation,
		Reason:            "NetworkStopped",
		Outcome:           "Drained",
	}
	if err := c.Status().Update(ctx, record); err != nil {
		t.Fatal(err)
	}
	if _, err := r.stopControlWorkload(ctx, root, p); err != nil {
		t.Fatal(err)
	}
	if err := c.Get(ctx, client.ObjectKeyFromObject(d), d); err != nil {
		t.Fatal(err)
	}
	if *d.Spec.Replicas != 0 {
		t.Fatal("acknowledged worker did not scale down")
	}
}

// TestDestructionMayDisposeAfterControlTermination keeps unresolved RPC facts intact.
func TestDestructionMayDisposeAfterControlTermination(t *testing.T) {
	for _, mode := range []string{
		"confirmed",
		"not-deleting",
		"stale-control",
		"stale-root",
		"running-control",
		"foreign-owner",
		"replaced-participant",
	} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			f := newControlFixture(t)
			if err := bitcoin.AddToScheme(f.c.Scheme()); err != nil {
				t.Fatal(err)
			}
			f.root.Finalizers = []string{"test/root"}
			if err := f.c.Update(ctx, f.root); err != nil {
				t.Fatal(err)
			}
			if mode != "not-deleting" {
				if err := f.c.Delete(ctx, f.root); err != nil {
					t.Fatal(err)
				}
			}
			if err := f.c.Get(ctx, client.ObjectKeyFromObject(f.root), f.root); err != nil {
				t.Fatal(err)
			}
			record := &bitcoin.BitcoinExecution{
				ObjectMeta: metav1.ObjectMeta{Name: "record", Namespace: f.p.Namespace, UID: "record"},
				Spec: bitcoin.BitcoinExecutionSpec{
					NetworkUID:  f.root.UID,
					Participant: common.Binding{UID: f.p.UID},
				},
				Status: bitcoin.BitcoinExecutionStatus{Armed: &bitcoin.BitcoinArmedRPC{}},
			}
			if err := f.c.Create(ctx, record); err != nil {
				t.Fatal(err)
			}
			// No execution pointer can substitute for the exact termination evidence.
			f.p.Status.BitcoinControl = &api.BitcoinControlRuntimeStatus{
				DeploymentRef:      &common.Binding{UID: f.deployment.UID},
				Terminated:         true,
				ObservedGeneration: f.p.Generation,
				NetworkGeneration:  f.root.Generation,
			}
			switch mode {
			case "stale-control":
				f.p.Status.BitcoinControl.ObservedGeneration--
			case "stale-root":
				f.p.Status.BitcoinControl.NetworkGeneration--
			case "running-control":
				f.p.Status.BitcoinControl.Terminated = false
			case "foreign-owner":
				f.p.OwnerReferences[0].UID = "foreign"
				if err := f.c.Update(ctx, f.p); err != nil {
					t.Fatal(err)
				}
			}
			if err := f.c.Status().Update(ctx, f.p); err != nil {
				t.Fatal(err)
			}
			caller := f.p.DeepCopy()
			if mode == "replaced-participant" {
				caller.UID = "previous"
			}
			ready, err := CheckDrained(ctx, f.c, caller)
			if err != nil || ready != (mode == "confirmed") {
				t.Fatalf("ready=%v error=%v for %s", ready, err, mode)
			}
			var retained bitcoin.BitcoinExecution
			if err := f.c.Get(ctx, client.ObjectKeyFromObject(record), &retained); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(retained.Status, record.Status) {
				t.Fatal("disposal rewrote unresolved execution evidence")
			}
		})
	}
}
