package stackssigner

import (
	"context"
	"errors"
	"reflect"
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

func TestDescribeRequiresSecretAndBindsSpec(t *testing.T) {
	actor := &networkv1alpha1.StacksSigner{ObjectMeta: metav1.ObjectMeta{Name: "network-signer"}, Spec: networkv1alpha1.StacksSignerSpec{
		NetworkRef: networkv1alpha1.LocalObjectReference{Name: "network"}, ActorName: "signer", Image: "signer:test",
		NodeRef: networkv1alpha1.LocalObjectReference{Name: "network-signer-node"}, Index: 2, Weight: 3,
		Config: networkv1alpha1.ConfigSource{SecretRef: &networkv1alpha1.ConfigObjectRef{Name: "signer-secret"}},
	}}
	descriptor, err := describe(actor)
	if err != nil {
		t.Fatal(err)
	}
	if descriptor.SpecDigest == "" || descriptor.Command[0] != "stacks-signer" {
		t.Fatalf("descriptor = %#v", descriptor)
	}
	actor.Spec.Weight++
	changed, err := describe(actor)
	if err != nil {
		t.Fatal(err)
	}
	if changed.SpecDigest == descriptor.SpecDigest {
		t.Fatal("signer weight did not change the bound specification digest")
	}

	actor.Spec.Config = networkv1alpha1.ConfigSource{ConfigMapRef: &networkv1alpha1.ConfigObjectRef{Name: "public-config"}}
	if _, err := describe(actor); err == nil {
		t.Fatal("describe admitted public signer configuration")
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
	actor := &networkv1alpha1.StacksSigner{ObjectMeta: metav1.ObjectMeta{Name: "network-signer", Namespace: "test", UID: "actor-uid", Generation: 1}, Spec: networkv1alpha1.StacksSignerSpec{
		NetworkRef: networkv1alpha1.LocalObjectReference{Name: "network"}, ActorName: "signer", Image: "signer:test",
		NodeRef: networkv1alpha1.LocalObjectReference{Name: "network-signer-node"}, Weight: 1, Config: networkv1alpha1.ConfigSource{SecretRef: &networkv1alpha1.ConfigObjectRef{Name: "signer-secret"}},
	}, Status: networkv1alpha1.ActorStatus{ObservedGeneration: 1, Phase: "Ready", Ready: true, Identity: &networkv1alpha1.ActorIdentity{PodUID: "admitted-pod"}}}
	want := actor.Status.DeepCopy()
	base := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&networkv1alpha1.StacksSigner{}).WithObjects(actor).Build()
	reconciler := &Reconciler{Client: &conflictingCreateClient{Client: base}, APIReader: base, Scheme: scheme}
	_, err := reconciler.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(actor)})
	if !apierrors.IsConflict(err) {
		t.Fatalf("reconcile error = %v", err)
	}
	current := &networkv1alpha1.StacksSigner{}
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
