package stacksworker

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/cache"
)

// CollectionWatch names a namespace-scoped collection used only for change notification.
type CollectionWatch struct {
	// APIVersion and Resource identify the versioned Kubernetes collection.
	APIVersion, Resource string
}

// CollectionWatcher opts a role into request collection notifications without granting dispatch authority.
type CollectionWatcher interface{ CollectionWatches() []CollectionWatch }

// CollectionObserver receives untrusted routing observations; fresh API reads must precede every send.
type CollectionObserver interface {
	CollectionChanged(CollectionWatch, *unstructured.Unstructured, bool)
}

// collectionInformer reconnects and relists through client-go's ordinary reflector.
func collectionInformer(
	c dynamic.Interface,
	namespace string,
	ref CollectionWatch,
	role Role,
	enqueue func(),
) (cache.SharedIndexInformer, error) {
	gv, err := schema.ParseGroupVersion(ref.APIVersion)
	if err != nil || ref.Resource == "" {
		return nil, fmt.Errorf("invalid collection watch identity")
	}
	resource := c.Resource(gv.WithResource(ref.Resource)).Namespace(namespace)
	listWatch := &cache.ListWatch{
		ListWithContextFunc: func(ctx context.Context, options metav1.ListOptions) (runtime.Object, error) {
			return resource.List(ctx, options)
		},
		WatchFuncWithContext: func(ctx context.Context, options metav1.ListOptions) (watch.Interface, error) {
			return resource.Watch(ctx, options)
		},
	}
	informer := cache.NewSharedIndexInformer(listWatch, &unstructured.Unstructured{}, 0, cache.Indexers{})
	changed := func(value any, deleted bool) {
		if tombstone, ok := value.(cache.DeletedFinalStateUnknown); ok {
			value = tombstone.Obj
		}
		if observer, ok := role.(CollectionObserver); ok {
			if object, ok := value.(*unstructured.Unstructured); ok {
				observer.CollectionChanged(ref, object.DeepCopy(), deleted)
			}
		}
		enqueue()
	}
	_, err = informer.AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc:    func(obj any) { changed(obj, false) },
			UpdateFunc: func(_, obj any) { changed(obj, false) },
			DeleteFunc: func(obj any) { changed(obj, true) },
		},
	)
	return informer, err
}
