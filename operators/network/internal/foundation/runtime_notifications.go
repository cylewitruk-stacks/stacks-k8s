package foundation

import (
	"context"
	"time"

	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
)

// runtimeNotifications coalesces observational updates through the standard delayed queue.
// Identity/spec/deletion events remain immediate; queue timers also evaluate freshness without events.
func runtimeNotifications(c client.Client) handler.TypedEventHandler[client.Object, networkRequest] {
	mapping := runtimeRequests(c)
	enqueue := func(
		ctx context.Context,
		obj client.Object,
		q workqueue.TypedRateLimitingInterface[networkRequest],
		delay time.Duration,
	) {
		for _, request := range mapping(ctx, obj) {
			if delay == 0 {
				q.Add(request)
			} else {
				q.AddAfter(request, delay)
			}
		}
	}
	return handler.TypedFuncs[client.Object, networkRequest]{
		CreateFunc: func(
			ctx context.Context,
			e event.CreateEvent,
			q workqueue.TypedRateLimitingInterface[networkRequest],
		) {
			enqueue(ctx, e.Object, q, 0)
		},
		DeleteFunc: func(
			ctx context.Context,
			e event.DeleteEvent,
			q workqueue.TypedRateLimitingInterface[networkRequest],
		) {
			enqueue(ctx, e.Object, q, 0)
		},
		GenericFunc: func(
			ctx context.Context,
			e event.GenericEvent,
			q workqueue.TypedRateLimitingInterface[networkRequest],
		) {
			enqueue(ctx, e.Object, q, 0)
		},
		UpdateFunc: func(
			ctx context.Context,
			e event.UpdateEvent,
			q workqueue.TypedRateLimitingInterface[networkRequest],
		) {
			a, b := e.ObjectOld, e.ObjectNew
			if a == nil || b == nil {
				return
			}
			delay := time.Duration(ObservationPolicy().PollIntervalSeconds) * time.Second
			if a.GetUID() != b.GetUID() || a.GetGeneration() != b.GetGeneration() ||
				!equal(a.GetDeletionTimestamp(), b.GetDeletionTimestamp()) ||
				!equal(a.GetOwnerReferences(), b.GetOwnerReferences()) ||
				!equal(a.GetFinalizers(), b.GetFinalizers()) {
				delay = 0
			}
			enqueue(ctx, a, q, delay)
			enqueue(ctx, b, q, delay)
		},
	}
}
