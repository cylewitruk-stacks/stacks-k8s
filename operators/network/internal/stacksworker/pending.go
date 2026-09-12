package stacksworker

import "context"

// PendingObserver polls only already submitted local work while Kubernetes access is unavailable.
// Implementations must not discover requests, authorize sends or publish API status here.
type PendingObserver interface{ ObservePending(context.Context) error }

// observePending retains read-only local progress without refreshing failed observation timestamps.
func (r *Runtime) observePending(ctx context.Context) {
	if observer, ok := r.Role.(PendingObserver); ok {
		_ = observer.ObservePending(ctx)
	}
}
