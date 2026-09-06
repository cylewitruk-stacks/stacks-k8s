package network

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	networkv1alpha1 "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha1"
)

// TestDeclarationWireContract uses pinned leaf vectors through the production publisher.
func TestDeclarationWireContract(t *testing.T) {
	read := func(name string, target any) {
		t.Helper()
		data, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "contracts", name))
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(data, target); err != nil {
			t.Fatal(err)
		}
	}
	var wire struct {
		Contract        string          `json:"contract"`
		LeafSpecVectors []string        `json:"leafSpecVectors"`
		Catalog         json.RawMessage `json:"catalog"`
	}
	read("target-declarations-v1.json", &wire)
	if wire.Contract != networkv1alpha1.TargetDeclarationsVersion || len(wire.LeafSpecVectors) != 3 {
		t.Fatal("unexpected declaration contract")
	}
	var leaves struct {
		Vectors []struct {
			ID   string          `json:"id"`
			Kind string          `json:"kind"`
			Spec json.RawMessage `json:"spec"`
		} `json:"vectors"`
	}
	read("leaf-spec-v1.json", &leaves)
	desired := DesiredTopology{}
	for _, id := range wire.LeafSpecVectors {
		found := false
		for _, vector := range leaves.Vectors {
			if vector.ID != id {
				continue
			}
			found = true
			switch vector.Kind {
			case "BitcoinNode":
				actor := networkv1alpha1.BitcoinNode{}
				if err := json.Unmarshal(vector.Spec, &actor.Spec); err != nil {
					t.Fatal(err)
				}
				actor.Name = "network-" + actor.Spec.ActorName
				desired.BitcoinNodes = append(desired.BitcoinNodes, actor)
			case "StacksNode":
				actor := networkv1alpha1.StacksNode{}
				if err := json.Unmarshal(vector.Spec, &actor.Spec); err != nil {
					t.Fatal(err)
				}
				actor.Name = "network-" + actor.Spec.ActorName
				desired.StacksNodes = append(desired.StacksNodes, actor)
			case "StacksSigner":
				actor := networkv1alpha1.StacksSigner{}
				if err := json.Unmarshal(vector.Spec, &actor.Spec); err != nil {
					t.Fatal(err)
				}
				actor.Name = "network-" + actor.Spec.ActorName
				desired.Signers = append(desired.Signers, actor)
			default:
				t.Fatalf("unexpected kind %s", vector.Kind)
			}
		}
		if !found {
			t.Fatalf("missing vector %s", id)
		}
	}
	actual, err := declarations(&networkv1alpha1.StacksNetwork{ObjectMeta: metav1.ObjectMeta{UID: "network-uid", Generation: 7}}, desired)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(actual)
	if err != nil {
		t.Fatal(err)
	}
	var got, want any
	if err := json.Unmarshal(encoded, &got); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(wire.Catalog, &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("declaration wire changed: got %s, want %s", encoded, wire.Catalog)
	}
}

// TestDeclarationsSurviveUnrelatedFailures checks publication before sync and observation.
func TestDeclarationsSurviveUnrelatedFailures(t *testing.T) {
	for _, failure := range []string{"unready", "foreign-stack-node", "observation-error"} {
		t.Run(failure, func(t *testing.T) {
			ctx := context.Background()
			parent := fixture()
			parent.Generation = 1
			builder := fake.NewClientBuilder().WithScheme(testScheme(t)).WithStatusSubresource(&networkv1alpha1.StacksNetwork{}).WithObjects(parent)
			if failure == "foreign-stack-node" {
				builder = builder.WithObjects(&networkv1alpha1.StacksNode{ObjectMeta: metav1.ObjectMeta{Name: "testnet-follower", Namespace: "test", UID: "foreign"}})
			}
			kube := builder.Build()
			r := &Reconciler{Client: kube, APIReader: kube, Scheme: testScheme(t), Now: time.Now}
			if failure == "observation-error" {
				r.APIReader = failingDeclarationReader{Reader: kube}
			}
			_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(parent)})
			if (failure == "unready") != (err == nil) {
				t.Fatalf("reconcile error = %v", err)
			}
			current := &networkv1alpha1.StacksNetwork{}
			mustGet(t, ctx, kube, parent.Name, current)
			desired, err := Compile(parent)
			if err != nil {
				t.Fatal(err)
			}
			want, err := declarations(parent, desired)
			if err != nil || !reflect.DeepEqual(current.Status.TargetDeclarations, want) {
				t.Fatalf("catalog lost through %s: %#v, error %v", failure, current.Status.TargetDeclarations, err)
			}
			if current.Status.InventoryReady || len(current.Status.Actors) != 0 {
				t.Fatal("declaration publication forged complete runtime inventory")
			}
		})
	}
}

// failingDeclarationReader simulates an uncached observation failure after publication.
type failingDeclarationReader struct{ client.Reader }

// List fails observation without affecting synchronization through the client.
func (r failingDeclarationReader) List(context.Context, client.ObjectList, ...client.ListOption) error {
	return fmt.Errorf("observation unavailable")
}

// TestInvalidCompilationWithdrawsDeclarations checks invalidated current parent intent.
func TestInvalidCompilationWithdrawsDeclarations(t *testing.T) {
	ctx := context.Background()
	parent := fixture()
	parent.Generation = 1
	kube := fake.NewClientBuilder().WithScheme(testScheme(t)).WithStatusSubresource(&networkv1alpha1.StacksNetwork{}).WithObjects(parent).Build()
	r := &Reconciler{Client: kube, APIReader: kube, Scheme: testScheme(t), Now: time.Now}
	key := client.ObjectKeyFromObject(parent)
	if _, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: key}); err != nil {
		t.Fatal(err)
	}
	mustGet(t, ctx, kube, parent.Name, parent)
	if parent.Status.TargetDeclarations == nil {
		t.Fatal("initial catalog absent")
	}
	parent.Generation++
	parent.Spec.Defaults.BitcoinImage = ""
	if err := kube.Update(ctx, parent); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: key}); err == nil {
		t.Fatal("invalid compilation succeeded")
	}
	mustGet(t, ctx, kube, parent.Name, parent)
	if parent.Status.TargetDeclarations != nil {
		t.Fatal("invalid compilation retained an admission catalog")
	}
}

// staleDeclarationClient admits a parent edit after the reconciler reads its snapshot.
type staleDeclarationClient struct {
	client.Client
	changed bool
}

// Get returns the old snapshot while advancing the API object's generation/version.
func (c *staleDeclarationClient) Get(ctx context.Context, key client.ObjectKey, object client.Object, opts ...client.GetOption) error {
	if err := c.Client.Get(ctx, key, object, opts...); err != nil {
		return err
	}
	if parent, ok := object.(*networkv1alpha1.StacksNetwork); ok && !c.changed {
		c.changed = true
		current := parent.DeepCopy()
		current.Generation++
		current.Spec.Suspended = true
		return c.Client.Update(ctx, current)
	}
	return nil
}

// TestStalePublicationConflictsBeforeWorkloadWrites checks the status CAS boundary.
func TestStalePublicationConflictsBeforeWorkloadWrites(t *testing.T) {
	ctx := context.Background()
	parent := fixture()
	parent.Generation = 1
	kube := fake.NewClientBuilder().WithScheme(testScheme(t)).WithStatusSubresource(&networkv1alpha1.StacksNetwork{}).WithObjects(parent).Build()
	r := &Reconciler{Client: &staleDeclarationClient{Client: kube}, APIReader: kube, Scheme: testScheme(t), Now: time.Now}
	_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(parent)})
	if !apierrors.IsConflict(err) {
		t.Fatalf("stale publication error = %v, want conflict", err)
	}
	mustGet(t, ctx, kube, parent.Name, parent)
	if parent.Status.TargetDeclarations != nil || !parent.Spec.Suspended {
		t.Fatal("stale declaration overwrote newer intent")
	}
	children := &networkv1alpha1.BitcoinNodeList{}
	if err := kube.List(ctx, children); err != nil || len(children.Items) != 0 {
		t.Fatalf("stale reconcile wrote children: %#v, %v", children.Items, err)
	}
}
