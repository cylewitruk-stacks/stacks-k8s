package cadence

import (
	"fmt"
	"testing"
	"time"

	actionv1 "github.com/cylewitruk-stacks/stacks-k8s/apis/network/actions/v1alpha1"
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

func TestGenerationBoundsAndSequence(t *testing.T) {
	for _, c := range []actionv1.GenerationCadence{
		{Mode: "Fixed"}, {Mode: "Fixed", IntervalSeconds: 61}, {Mode: "Uniform", MinSeconds: 2, MaxSeconds: 1},
		{Mode: "Explicit", DelaysSeconds: []int32{-1}}, {Mode: "Explicit", DelaysSeconds: []int32{61}}, {Mode: "unknown"},
	} {
		if _, err := Generation(c, 2, 1, "test"); err == nil {
			t.Fatalf("invalid cadence accepted: %#v", c)
		}
	}
	sequence := actionv1.GenerationCadence{Mode: "Explicit", DelaysSeconds: []int32{0, 60, 2}}
	for i, expected := range []time.Duration{0, 60 * time.Second, 2 * time.Second} {
		actual, err := Generation(sequence, 4, int32(i+1), "test")
		if err != nil || actual != expected {
			t.Fatalf("sequence[%d]: %v %v", i, actual, err)
		}
	}
	for _, count := range []int32{0, 4} {
		if _, err := Generation(sequence, 4, count, "test"); err == nil {
			t.Fatal("sequence overrun accepted")
		}
	}
}

func TestDueNeverRoundsEarlier(t *testing.T) {
	anchor := time.Unix(1000, 123456789)
	for _, delay := range []time.Duration{0, time.Second, 24 * time.Hour} {
		got := Due(anchor, delay)
		if got.Before(anchor.Add(delay)) || got.Sub(anchor.Add(delay)) >= time.Microsecond || got.Nanosecond()%1000 != 0 {
			t.Fatal(got)
		}
	}
}

func TestValidateCompleteCadence(t *testing.T) {
	for _, tc := range []struct {
		name    string
		cadence actionv1.GenerationCadence
		count   int32
		valid   bool
	}{
		{"immediate-one", actionv1.GenerationCadence{Mode: "Immediate"}, 1, true},
		{"fixed-one", actionv1.GenerationCadence{Mode: "Fixed", IntervalSeconds: 60}, 1, true},
		{"uniform-one", actionv1.GenerationCadence{Mode: "Uniform", MinSeconds: 1, MaxSeconds: 60}, 1, true},
		{"explicit-one", actionv1.GenerationCadence{Mode: "Explicit"}, 1, true},
		{"explicit-max", actionv1.GenerationCadence{Mode: "Explicit", DelaysSeconds: make([]int32, 99)}, 100, true},
		{"missing", actionv1.GenerationCadence{}, 1, false},
		{"unknown-one", actionv1.GenerationCadence{Mode: "Future"}, 1, false},
		{"zero-count", actionv1.GenerationCadence{Mode: "Immediate"}, 0, false},
		{"over-count", actionv1.GenerationCadence{Mode: "Immediate"}, 101, false},
		{"mixed-interval", actionv1.GenerationCadence{Mode: "Immediate", IntervalSeconds: 1}, 2, false},
		{"mixed-uniform", actionv1.GenerationCadence{Mode: "Fixed", IntervalSeconds: 1, MinSeconds: 1}, 2, false},
		{"mixed-sequence", actionv1.GenerationCadence{Mode: "Uniform", MinSeconds: 1, MaxSeconds: 2, DelaysSeconds: []int32{1}}, 2, false},
		{"missing-fixed", actionv1.GenerationCadence{Mode: "Fixed"}, 1, false},
		{"unbounded-fixed", actionv1.GenerationCadence{Mode: "Fixed", IntervalSeconds: 61}, 1, false},
		{"zero-uniform", actionv1.GenerationCadence{Mode: "Uniform", MaxSeconds: 2}, 1, false},
		{"reversed-uniform", actionv1.GenerationCadence{Mode: "Uniform", MinSeconds: 2, MaxSeconds: 1}, 1, false},
		{"unbounded-uniform", actionv1.GenerationCadence{Mode: "Uniform", MinSeconds: 1, MaxSeconds: 61}, 1, false},
		{"short-sequence", actionv1.GenerationCadence{Mode: "Explicit", DelaysSeconds: []int32{0}}, 3, false},
		{"long-sequence", actionv1.GenerationCadence{Mode: "Explicit", DelaysSeconds: []int32{0, 1}}, 2, false},
		{"later-unbounded-delay", actionv1.GenerationCadence{Mode: "Explicit", DelaysSeconds: []int32{0, 61}}, 3, false},
		{"later-negative-delay", actionv1.GenerationCadence{Mode: "Explicit", DelaysSeconds: []int32{0, -1}}, 3, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := Validate(tc.cadence, tc.count); (err == nil) != tc.valid {
				t.Fatalf("valid=%t: %v", tc.valid, err)
			}
		})
	}
}
