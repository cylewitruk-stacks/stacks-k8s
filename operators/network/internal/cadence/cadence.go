// Package cadence samples bounded delays independently of execution and target selection.
package cadence

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math/rand/v2"
	"time"
)

// Seconds samples an inclusive integer range using a stable decision key.
// A retry of the same decision cannot redraw its delay. This is not an execution replay seed.
func Seconds(lower, upper int32, key string) (time.Duration, error) {
	if lower < 0 || upper < lower || upper > 86400 {
		return 0, fmt.Errorf("invalid cadence bounds")
	}
	digest := sha256.Sum256([]byte(key))
	// #nosec G404 -- Reproducible cadence sampling, not cryptographic randomness or authority.
	random := rand.New(rand.NewPCG(binary.BigEndian.Uint64(digest[:8]), binary.BigEndian.Uint64(digest[8:16])))
	return time.Duration(int(lower)+random.IntN(int(upper-lower)+1)) * time.Second, nil
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
