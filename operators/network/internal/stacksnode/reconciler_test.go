package stacksnode

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

	networkv1alpha1 "github.com/cylewitruk-stacks/stacks-k8s/operators/network/api/v1alpha1"
)

func TestDescribeGeneratedStacksFollower(t *testing.T) {
	actor := &networkv1alpha1.StacksNode{ObjectMeta: metav1.ObjectMeta{Name: "network-follower"}, Spec: networkv1alpha1.StacksNodeSpec{
		NetworkRef: networkv1alpha1.LocalObjectReference{Name: "network"}, ActorName: "follower", Role: networkv1alpha1.StacksNodeFollower,
		Image: "stacks:test", BitcoinNodeRef: networkv1alpha1.LocalObjectReference{Name: "network-bitcoin"},
		Config: networkv1alpha1.ConfigSource{Generated: &networkv1alpha1.GeneratedConfig{Profile: "nakamoto-regtest-node/v1"}},
	}}
	descriptor, err := describe(actor)
	if err != nil {
		t.Fatal(err)
	}
	if descriptor.Config.Inline == nil || !strings.Contains(descriptor.Config.Inline.Data, `peer_host = "network-bitcoin"`) {
		t.Fatal("generated Stacks profile does not bind its Bitcoin service")
	}
	if len(descriptor.Dependencies) != 1 || descriptor.Dependencies[0].Host != "network-bitcoin" || descriptor.SpecDigest == "" {
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
	actor := &networkv1alpha1.StacksNode{ObjectMeta: metav1.ObjectMeta{Name: "network-follower", Namespace: "test", UID: "actor-uid", Generation: 1}, Spec: networkv1alpha1.StacksNodeSpec{
		NetworkRef: networkv1alpha1.LocalObjectReference{Name: "network"}, ActorName: "follower", Role: networkv1alpha1.StacksNodeFollower,
		Image: "stacks:test", BitcoinNodeRef: networkv1alpha1.LocalObjectReference{Name: "network-bitcoin"}, Config: networkv1alpha1.ConfigSource{Generated: &networkv1alpha1.GeneratedConfig{Profile: "nakamoto-regtest-node/v1"}},
	}, Status: networkv1alpha1.ActorStatus{ObservedGeneration: 1, Phase: "Ready", Ready: true, Identity: &networkv1alpha1.ActorIdentity{PodUID: "admitted-pod"}}}
	want := actor.Status.DeepCopy()
	base := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&networkv1alpha1.StacksNode{}).WithObjects(actor).Build()
	reconciler := &Reconciler{Client: &conflictingCreateClient{Client: base}, APIReader: base, Scheme: scheme}
	_, err := reconciler.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(actor)})
	if !apierrors.IsConflict(err) {
		t.Fatalf("reconcile error = %v", err)
	}
	current := &networkv1alpha1.StacksNode{}
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

func TestDescribeRequiresSecretForMiner(t *testing.T) {
	actor := &networkv1alpha1.StacksNode{Spec: networkv1alpha1.StacksNodeSpec{
		Role:   networkv1alpha1.StacksNodeMiner,
		Config: networkv1alpha1.ConfigSource{ConfigMapRef: &networkv1alpha1.ConfigObjectRef{Name: "public-config"}},
	}}
	if _, err := describe(actor); err == nil {
		t.Fatal("describe admitted public miner configuration")
	}
}
