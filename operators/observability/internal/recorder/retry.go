package recorder

import (
	"context"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/wait"
)

const healthyWatchDuration = 30 * time.Second

// sourceRetry backs off unsuccessful subscriptions independently for each source.
type sourceRetry struct{ delay time.Duration }

// reset clears failures after a successful snapshot or healthy subscription.
func (r *sourceRetry) reset() { r.delay = 0 }

// next reserves fast reconnection for clean streams with progress or a healthy lifetime.
// Merely opening a watch, or immediately closing an empty stream, is not recovery.
func (r *sourceRetry) next(err error, healthy bool) time.Duration {
	if healthy {
		r.reset()
	}
	if healthy && err == nil {
		return 250 * time.Millisecond
	}
	if r.delay == 0 {
		r.delay = time.Second
	} else {
		r.delay = min(2*r.delay, 30*time.Second)
	}
	delay := r.delay
	if apierrors.IsNotFound(err) || apierrors.IsForbidden(err) || apierrors.IsUnauthorized(err) {
		delay = time.Minute
	}
	// Equal jitter stays within the cap and avoids synchronized source retries.
	return wait.Jitter(delay/2, 1)
}

// waitSourceRetry is interruptible so backoff cannot delay shutdown.
func waitSourceRetry(ctx context.Context, delay time.Duration) {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}
