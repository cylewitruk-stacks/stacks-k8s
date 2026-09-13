package cadence

import (
	"fmt"
	"testing"
	"time"
)

func TestInclusiveStableSampling(t *testing.T) {
	seen := map[time.Duration]bool{}
	for i := 0; i < 200; i++ {
		key := fmt.Sprintf("action/%d", i)
		delay, err := Seconds(1, 3, key)
		if err != nil || delay < time.Second || delay > 3*time.Second {
			t.Fatalf("out of range: %v %v", delay, err)
		}
		again, _ := Seconds(1, 3, key)
		if again != delay {
			t.Fatal("same decision redrawn")
		}
		seen[delay] = true
	}
	if len(seen) != 3 {
		t.Fatal("inclusive range endpoints not sampled")
	}
	for _, bounds := range [][2]int32{{-1, 1}, {3, 2}, {0, 86401}} {
		if _, err := Seconds(bounds[0], bounds[1], "invalid"); err == nil {
			t.Fatal("invalid range accepted")
		}
	}
}

func TestDueNeverRoundsEarlier(t *testing.T) {
	anchor := time.Unix(1000, 123456789)
	for _, delay := range []time.Duration{0, time.Second, 24 * time.Hour} {
		got := Due(anchor, delay)
		if got.Before(anchor.Add(delay)) || got.Sub(anchor.Add(delay)) >= time.Microsecond ||
			got.Nanosecond()%1000 != 0 {
			t.Fatal(got)
		}
	}
}
