package bitcoinnode

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	networkv1alpha1 "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha1"
)

func TestDescribeGeneratedBitcoinProfile(t *testing.T) {
	actor := &networkv1alpha1.BitcoinNode{ObjectMeta: metav1.ObjectMeta{Name: "network-bitcoin"}, Spec: networkv1alpha1.BitcoinNodeSpec{
		NetworkRef: networkv1alpha1.LocalObjectReference{Name: "network"}, ActorName: "bitcoin", Role: networkv1alpha1.BitcoinNodeMiner,
		Image: "bitcoin:test", Config: networkv1alpha1.ConfigSource{Generated: &networkv1alpha1.GeneratedConfig{Profile: "bitcoin-regtest/v1"}},
		PeerRefs: []string{"network-peer"}, RPCPort: 18443, P2PPort: 18444,
	}}
	descriptor, err := describe(actor)
	if err != nil {
		t.Fatal(err)
	}
	if descriptor.Config.Inline == nil || !strings.Contains(descriptor.Config.Inline.Data, "regtest=1") {
		t.Fatal("generated Bitcoin profile was not materialized")
	}
	if !contains(descriptor.Args, "-addnode=network-peer:18444") || descriptor.SpecDigest == "" {
		t.Fatalf("descriptor = %#v", descriptor)
	}
}

func TestReconcilePreservesStatusOnTransientConflict(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := networkv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	actor := &networkv1alpha1.BitcoinNode{ObjectMeta: metav1.ObjectMeta{Name: "network-bitcoin", Namespace: "test", UID: "actor-uid", Generation: 1}, Spec: networkv1alpha1.BitcoinNodeSpec{
		NetworkRef: networkv1alpha1.LocalObjectReference{Name: "network"}, ActorName: "bitcoin", Role: networkv1alpha1.BitcoinNodeMiner,
		Image: "bitcoin:test", Config: networkv1alpha1.ConfigSource{Generated: &networkv1alpha1.GeneratedConfig{Profile: "bitcoin-regtest/v1"}}, RPCPort: 18443, P2PPort: 18444,
	}, Status: networkv1alpha1.ActorStatus{ObservedGeneration: 1, Phase: "Ready", Ready: true, Identity: &networkv1alpha1.ActorIdentity{PodUID: "admitted-pod"}}}
	want := actor.Status.DeepCopy()
	base := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&networkv1alpha1.BitcoinNode{}).WithObjects(actor).Build()
	reconciler := &Reconciler{Client: &conflictingCreateClient{Client: base}, APIReader: base, Scheme: scheme}
	_, err := reconciler.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(actor)})
	if !apierrors.IsConflict(err) {
		t.Fatalf("reconcile error = %v", err)
	}
	current := &networkv1alpha1.BitcoinNode{}
	if err := base.Get(context.Background(), client.ObjectKeyFromObject(actor), current); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(current.Status, *want) {
		t.Fatalf("status changed on transient conflict: %#v", current.Status)
	}
}

type conflictingCreateClient struct{ client.Client }

func (c *conflictingCreateClient) Create(context.Context, client.Object, ...client.CreateOption) error {
	return apierrors.NewConflict(schema.GroupResource{Resource: "managed-workloads"}, "actor", errors.New("simulated conflict"))
}

func TestDescribeRejectsWrongBitcoinProfile(t *testing.T) {
	actor := &networkv1alpha1.BitcoinNode{Spec: networkv1alpha1.BitcoinNodeSpec{Config: networkv1alpha1.ConfigSource{Generated: &networkv1alpha1.GeneratedConfig{Profile: "nakamoto-regtest-node/v1"}}}}
	if _, err := describe(actor); err == nil {
		t.Fatal("describe accepted a Stacks profile for Bitcoin")
	}
}

func contains(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}
