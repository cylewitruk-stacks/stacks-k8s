package recorder

import (
	"fmt"
	"testing"
	"time"
)

// TestSourceRetryBoundsAndRecovery rejects immediate-error reconnect storms.
func TestSourceRetryBoundsAndRecovery(t *testing.T) {
	var retry sourceRetry
	err := fmt.Errorf("unavailable")
	for i := range 12 {
		ceiling := min(time.Second<<i, 30*time.Second)
		delay := retry.next(err, false)
		if delay < ceiling/2 || delay > ceiling {
			t.Fatalf("retry %d: %s outside [%s,%s]", i, delay, ceiling/2, ceiling)
		}
	}
	// Empty short-lived clean streams do not reset accumulated backoff either.
	if d := retry.next(nil, false); d < 15*time.Second {
		t.Fatalf("empty closure bypassed backoff: %s", d)
	}
	if d := retry.next(nil, true); d != 250*time.Millisecond {
		t.Fatal("healthy closure did not reconnect promptly")
	}
	if d := retry.next(err, false); d > time.Second {
		t.Fatal("healthy subscription did not reset")
	}
	retry.next(err, false)
	retry.reset() // successful list
	if d := retry.next(err, false); d > time.Second {
		t.Fatal("successful list did not reset")
	}
}
