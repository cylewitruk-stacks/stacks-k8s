// Package cadence samples bounded delays independently of execution and target selection.
package cadence

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math/rand/v2"
	"time"

	actionv1 "github.com/cylewitruk-stacks/stacks-k8s/apis/network/actions/v1alpha1"
)

// Seconds samples an inclusive integer range using a stable decision key.
// A retry of the same decision cannot redraw its delay. This is not an execution replay seed.
func Seconds(lower, upper int32, key string) (time.Duration, error) {
	if lower < 0 || upper < lower || upper > 86400 {
		return 0, fmt.Errorf("invalid cadence bounds")
	}
	digest := sha256.Sum256([]byte(key))
	random := rand.New(rand.NewPCG(binary.BigEndian.Uint64(digest[:8]), binary.BigEndian.Uint64(digest[8:16])))
	return time.Duration(int(lower)+random.IntN(int(upper-lower)+1)) * time.Second, nil
}

// Validate checks that the executor supports the complete decoded finite cadence.
func Validate(c actionv1.GenerationCadence, count int32) error {
	if count < 1 || count > 100 {
		return fmt.Errorf("generation count must be 1..100")
	}
	if (c.Mode != "Fixed" && c.IntervalSeconds != 0) ||
		(c.Mode != "Uniform" && (c.MinSeconds != 0 || c.MaxSeconds != 0)) ||
		(c.Mode != "Explicit" && len(c.DelaysSeconds) != 0) {
		return fmt.Errorf("cadence contains fields for another mode")
	}
	switch c.Mode {
	case "Immediate":
		return nil
	case "Fixed":
		if c.IntervalSeconds >= 1 && c.IntervalSeconds <= 60 {
			return nil
		}
	case "Uniform":
		if c.MinSeconds >= 1 && c.MinSeconds <= c.MaxSeconds && c.MaxSeconds <= 60 {
			return nil
		}
	case "Explicit":
		if len(c.DelaysSeconds) != int(count-1) {
			break
		}
		for _, delay := range c.DelaysSeconds {
			if delay < 0 || delay > 60 {
				return fmt.Errorf("explicit delay must be 0..60 seconds")
			}
		}
		return nil
	}
	return fmt.Errorf("unsupported generation cadence or bounds")
}

// Generation validates the full request and returns the delay after a non-final receipt.
func Generation(c actionv1.GenerationCadence, count, completed int32, key string) (time.Duration, error) {
	if err := Validate(c, count); err != nil {
		return 0, err
	}
	if completed < 1 || completed >= count {
		return 0, fmt.Errorf("delay requires a non-final receipt")
	}
	switch c.Mode {
	case "Immediate":
		return 0, nil
	case "Fixed":
		return Seconds(c.IntervalSeconds, c.IntervalSeconds, key)
	case "Uniform":
		return Seconds(c.MinSeconds, c.MaxSeconds, key)
	case "Explicit":
		delay := c.DelaysSeconds[completed-1]
		return Seconds(delay, delay, key)
	}
	return 0, fmt.Errorf("unsupported generation cadence")
}

// Due rounds up to API timestamp precision so serialization cannot shorten a delay.
func Due(anchor time.Time, delay time.Duration) time.Time {
	value := anchor.Add(delay)
	rounded := value.Truncate(time.Microsecond)
	if rounded.Before(value) {
		rounded = rounded.Add(time.Microsecond)
	}
	return rounded
}
