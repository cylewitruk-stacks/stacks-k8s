package stacksworker

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"time"

	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/cache"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// namedWatch scopes ordinary reflector list/relist/watch to one resource name.
func namedWatch(c dynamic.Interface, namespace string, ref ReadBinding) *cache.ListWatch {
	gv, _ := schema.ParseGroupVersion(ref.APIVersion)
	resource := c.Resource(gv.WithResource(ref.Resource)).Namespace(namespace)
	selector := fields.OneTermEqualSelector("metadata.name", ref.Name).String()
	return &cache.ListWatch{
		ListWithContextFunc: func(ctx context.Context, options metav1.ListOptions) (runtime.Object, error) {
			options.FieldSelector = selector
			return resource.List(ctx, options)
		},
		WatchFuncWithContext: func(ctx context.Context, options metav1.ListOptions) (watch.Interface, error) {
			options.FieldSelector = selector
			return resource.Watch(ctx, options)
		},
	}
}

// Run keeps the role instance alive across standard informer reconnects and duplicate notifications.
// Kubernetes caches provide notifications only; every execution pass uses fresh direct reads.
func (r *Runtime) Run(ctx context.Context) error {
	if r.Dynamic == nil || r.Client == nil || r.Role == nil {
		return fmt.Errorf("worker clients and role are required")
	}
	profile, err := r.Profile.Normalize()
	if err != nil {
		return err
	}
	r.Profile = profile
	watchCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	queue := make(chan struct{}, 1)
	enqueue := func() {
		select {
		case queue <- struct{}{}:
		default:
		}
	}
	refs := append(
		[]ReadBinding{
			{APIVersion: api.GroupVersion.String(), Resource: api.ResourceStacksNetwork, Name: "network"},
			{
				APIVersion: api.GroupVersion.String(),
				Resource:   api.ResourceStacksNetworkParticipant,
				Name:       r.ParticipantName,
			},
		},
		profile.Reads...)
	seen := map[ReadBinding]bool{}
	var workers sync.WaitGroup
	defer func() { cancel(); workers.Wait() }()
	for _, ref := range refs {
		if seen[ref] {
			continue
		}
		seen[ref] = true
		informer := cache.NewSharedIndexInformer(
			namedWatch(r.Dynamic, r.Namespace, ref),
			&unstructured.Unstructured{},
			0,
			cache.Indexers{},
		)
		if _, err := informer.AddEventHandler(
			cache.ResourceEventHandlerFuncs{AddFunc: func(any) { enqueue() }, UpdateFunc: func(old, current any) {
				if namedUpdateRelevant(old, current) {
					enqueue()
				}
			}, DeleteFunc: func(any) { enqueue() }},
		); err != nil {
			return err
		}
		workers.Add(1)
		go func() { defer workers.Done(); informer.RunWithContext(watchCtx) }()
	}
	if collections, ok := r.Role.(CollectionWatcher); ok {
		seenCollections := map[CollectionWatch]bool{}
		for _, ref := range collections.CollectionWatches() {
			if seenCollections[ref] {
				continue
			}
			seenCollections[ref] = true
			informer, err := collectionInformer(r.Dynamic, r.Namespace, ref, r.Role, enqueue)
			if err != nil {
				return err
			}
			workers.Add(1)
			go func() { defer workers.Done(); informer.RunWithContext(watchCtx) }()
		}
	}
	timer := time.NewTimer(0)
	defer timer.Stop()
	var lastErrorAt time.Time
	for {
		select {
		case <-ctx.Done():
			cancel()
			return ctx.Err()
		case <-queue:
		case <-timer.C:
		}
		result, err := r.Reconcile(ctx)
		if err != nil && time.Since(lastErrorAt) >= 30*time.Second {
			log.FromContext(ctx).Error(err, "Worker reconciliation failed", "participant", r.ParticipantName)
			lastErrorAt = time.Now()
		}
		if result.Exit {
			cancel()
			return err
		}
		delay := result.RequeueAfter
		if err != nil || delay <= 0 {
			delay = time.Second
		}
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timer.Reset(delay)
	}
}

// namedUpdateRelevant excludes observation feedback; the timer still reconciles all current facts.
// Intent, admission, runtime identity and lifecycle changes remain immediate notifications.
func namedUpdateRelevant(old, current any) bool {
	before, okBefore := old.(*unstructured.Unstructured)
	after, okAfter := current.(*unstructured.Unstructured)
	if !okBefore || !okAfter {
		return true
	}
	return !reflect.DeepEqual(namedWatchProjection(before), namedWatchProjection(after))
}

// namedWatchProjection copies informer data before removing fields owned by observation writers.
func namedWatchProjection(object *unstructured.Unstructured) map[string]any {
	snapshot := object.DeepCopy()
	unstructured.RemoveNestedField(snapshot.Object, "metadata", "resourceVersion")
	unstructured.RemoveNestedField(snapshot.Object, "metadata", "managedFields")
	if snapshot.GetAPIVersion() == api.GroupVersion.String() && snapshot.GetKind() == api.KindStacksNetworkParticipant {
		unstructured.RemoveNestedField(snapshot.Object, "status", "execution")
		unstructured.RemoveNestedField(snapshot.Object, "status", "runtime", "protocol")
	}
	if snapshot.GetAPIVersion() == api.GroupVersion.String() {
		conditions, found, _ := unstructured.NestedSlice(snapshot.Object, "status", "conditions")
		if found {
			for _, value := range conditions {
				if condition, ok := value.(map[string]any); ok {
					delete(condition, "lastTransitionTime")
				}
			}
			_ = unstructured.SetNestedSlice(snapshot.Object, conditions, "status", "conditions")
		}
	}
	return snapshot.Object
}
