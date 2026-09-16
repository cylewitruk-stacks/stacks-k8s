package main

import (
	"math"
	"reflect"
	"testing"
)

func TestSeededShapesAreIndependentOfEvaluationOrder(t *testing.T) {
	seed := uint64(42)
	r := executionRequest()
	r.Variation = &variation{
		Seed: &seed, Functions: []string{"run2", "run8", "run16"},
		Writes: &intRange{1, 16}, Reads: &intRange{4, 128}, PayloadBytes: &intRange{16, 1024},
	}
	baseline := make([]submitRequest, 100)
	for i := range baseline {
		baseline[i] = variedRequest(r, i)
	}
	for i := len(baseline) - 1; i >= 0; i-- {
		got := variedRequest(r, i)
		if !reflect.DeepEqual(baseline[i], got) || got.Writes < 1 || got.Writes > 16 || got.Reads < 4 ||
			got.Reads > 128 ||
			got.PayloadBytes < 16 ||
			got.PayloadBytes > 1024 {
			t.Fatal("shape drift or bound violation")
		}
	}
	other := uint64(43)
	r.Variation = &variation{
		Seed:         &other,
		Writes:       &intRange{1, 16},
		Reads:        &intRange{4, 128},
		PayloadBytes: &intRange{16, 1024},
	}
	changed := false
	for i := range baseline {
		got := variedRequest(r, i)
		changed = changed || got.Writes != baseline[i].Writes || got.Reads != baseline[i].Reads
	}
	if !changed {
		t.Fatal("seed has no effect")
	}
}

func TestInvalidControlsAndVariationRejected(t *testing.T) {
	seed := uint64(0)
	for name, change := range map[string]func(*submitRequest){
		"negative interval":      func(r *submitRequest) { r.IntervalMilliseconds = -1 },
		"too many readers":       func(r *submitRequest) { r.ObservationConcurrency = 17 },
		"too many outstanding":   func(r *submitRequest) { r.MaxOutstanding = 101 },
		"duration exceeds total": func(r *submitRequest) { r.DurationSeconds = r.TimeoutSeconds },
		"key overflow":           func(r *submitRequest) { r.KeyBase = math.MaxUint64 },
		"missing seed":           func(r *submitRequest) { r.Variation = &variation{} },
		"inverted range":         func(r *submitRequest) { r.Variation = &variation{Seed: &seed, Writes: &intRange{3, 2}} },
		"oversize range": func(r *submitRequest) {
			r.Variation = &variation{Seed: &seed, PayloadBytes: &intRange{0, 4097}}
		},
		"invalid function": func(r *submitRequest) {
			r.Variation = &variation{Seed: &seed, Functions: []string{"bad name"}}
		},
	} {
		t.Run(name, func(t *testing.T) {
			r := executionRequest()
			change(&r)
			if validateSubmitRequest(r) == nil {
				t.Fatal("accepted invalid controls")
			}
		})
	}
}

// The fixed vectors were calculated independently from the documented SHA-256 mapping.
func TestShapeVersionVectors(t *testing.T) {
	seed := uint64(42)
	r := executionRequest()
	r.Variation = &variation{
		Seed: &seed, Functions: []string{"run2", "run8", "run16"},
		Writes: &intRange{1, 16}, Reads: &intRange{4, 128}, PayloadBytes: &intRange{16, 1024},
	}
	expected := []callShape{{"run2", 14, 29, 797, 0}, {"run2", 9, 114, 93, 64}, {"run16", 2, 93, 739, 128}}
	for i, want := range expected {
		got := variedRequest(r, i)
		if got.Function != want.Function || got.Writes != want.Writes || got.Reads != want.Reads ||
			got.PayloadBytes != want.PayloadBytes {
			t.Fatalf("ordinal %d mapping changed: %+v", i, got)
		}
	}
}
