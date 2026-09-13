package stacksworker

import (
	"context"
	"sync"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	clientfeatures "k8s.io/client-go/features"
	clientfeaturestesting "k8s.io/client-go/features/testing"
	clienttesting "k8s.io/client-go/testing"
)

// collectionRole records routing events without executing its ordinary operation.
type collectionRole struct {
	pendingRole
	mu      sync.Mutex
	events  []*unstructured.Unstructured
	deleted bool
}

func (r *collectionRole) CollectionChanged(_ CollectionWatch, object *unstructured.Unstructured, deleted bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, object)
	r.deleted = deleted
	object.SetName("role-mutated-copy")
}

func TestCollectionWatchScopesNamespaceAndOnlyNotifies(t *testing.T) {
	clientfeaturestesting.SetFeatureDuringTest(t, clientfeatures.WatchListClient, false)
	gvr := schema.GroupVersionResource{
		Group:    "stacks.stacks.org",
		Version:  "v1alpha2",
		Resource: "stacksfaucetrequests",
	}
	c := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(
		runtime.NewScheme(),
		map[schema.GroupVersionResource]string{gvr: "StacksFaucetRequestList"},
	)
	object := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": gvr.GroupVersion().String(),
			"kind":       "StacksFaucetRequest",
			"metadata": map[string]any{
				"namespace":       "test",
				"name":            "request",
				"uid":             "request-uid",
				"resourceVersion": "1",
			},
		},
	}
	c.PrependReactor("list", gvr.Resource, func(action clienttesting.Action) (bool, runtime.Object, error) {
		if action.GetNamespace() != "test" {
			t.Error("collection list escaped namespace")
		}
		return true, &unstructured.UnstructuredList{
			Object: map[string]any{"metadata": map[string]any{"resourceVersion": "1"}},
			Items:  []unstructured.Unstructured{*object.DeepCopy()},
		}, nil
	})
	stream := watch.NewRaceFreeFake()
	watching := make(chan struct{}, 1)
	c.PrependWatchReactor(gvr.Resource, func(action clienttesting.Action) (bool, watch.Interface, error) {
		if action.GetNamespace() != "test" {
			t.Error("collection watch escaped namespace")
		}
		watching <- struct{}{}
		return true, stream, nil
	})
	role := &collectionRole{}
	events := make(chan struct{}, 10)
	informer, err := collectionInformer(
		c,
		"test",
		CollectionWatch{APIVersion: gvr.GroupVersion().String(), Resource: gvr.Resource},
		role,
		func() { events <- struct{}{} },
	)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { defer close(done); informer.RunWithContext(ctx) }()
	await := func(ch <-chan struct{}) {
		t.Helper()
		select {
		case <-ch:
		case <-time.After(5 * time.Second):
			t.Fatal("collection notification timeout")
		}
	}
	await(events)
	await(watching)
	update := object.DeepCopy()
	update.SetResourceVersion("2")
	stream.Modify(update)
	await(events)
	stream.Delete(update)
	await(events)
	cancel()
	await(done)
	role.mu.Lock()
	defer role.mu.Unlock()
	if len(role.events) != 3 || !role.deleted || role.steps != 0 || role.sends != 0 || object.GetName() != "request" ||
		update.GetName() != "request" {
		t.Fatal("notification executed role or mutated informer inputs")
	}
	if _, err := collectionInformer(
		c,
		"test",
		CollectionWatch{APIVersion: "invalid/version/extra", Resource: gvr.Resource},
		role,
		func() {},
	); err == nil {
		t.Fatal("invalid collection identity accepted")
	}
}
