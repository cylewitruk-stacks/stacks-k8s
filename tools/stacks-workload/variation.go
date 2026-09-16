package main

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"math"
	"strconv"

	"github.com/cylewitruk-stacks/stacks-k8s/libs/stacks/clarity"
)

// planVersion pins the public seed-to-shape mapping, not distributed execution.
const planVersion = "sha256-shapes-v1"

// intRange is an inclusive variation range; an omitted range uses the fixed request field.
type intRange struct {
	Min int `json:"min"`
	Max int `json:"max"`
}

// variation varies call shapes without deriving anything from runtime timing.
type variation struct {
	Seed         *uint64   `json:"seed"`
	Functions    []string  `json:"functions,omitempty"`
	Writes       *intRange `json:"writes,omitempty"`
	Reads        *intRange `json:"reads,omitempty"`
	PayloadBytes *intRange `json:"payloadBytes,omitempty"`
}

// callShape identifies public workload inputs for one ordinal.
type callShape struct {
	Function     string `json:"function"`
	Writes       int    `json:"writes"`
	Reads        int    `json:"reads"`
	PayloadBytes int    `json:"payloadBytes"`
	KeyBase      uint64 `json:"keyBase"`
}

// validateControls bounds runtime work and rejects arithmetic overflow before sending.
func validateControls(r submitRequest) error {
	if r.IntervalMilliseconds < 0 || r.IntervalMilliseconds > 60000 ||
		r.MaxOutstanding < 0 || r.MaxOutstanding > 100 ||
		r.ObservationConcurrency < 0 || r.ObservationConcurrency > 16 ||
		r.DurationSeconds < 0 || r.DurationSeconds >= r.TimeoutSeconds {
		return errors.New("workload controls are outside their bounds")
	}
	if r.Count > 0 && r.KeyBase > math.MaxUint64-uint64(r.Count)*64 {
		return errors.New("workload key range overflows")
	}
	return nil
}

// validateVariation rejects unsupported ranges and names before nonce discovery.
func validateVariation(r submitRequest) error {
	if !clarity.ValidName(r.Function) {
		return errors.New("invalid call function")
	}
	v := r.Variation
	if v == nil {
		return nil
	}
	if v.Seed == nil || len(v.Functions) > 16 {
		return errors.New("variation requires a seed and at most 16 functions")
	}
	for _, name := range v.Functions {
		if !clarity.ValidName(name) {
			return errors.New("invalid variation function")
		}
	}
	for _, field := range []struct {
		value *intRange
		max   int
	}{
		{v.Writes, 64}, {v.Reads, 1024}, {v.PayloadBytes, 4096},
	} {
		if field.value != nil &&
			(field.value.Min < 0 || field.value.Max < field.value.Min || field.value.Max > field.max) {
			return errors.New("invalid variation range")
		}
	}
	return nil
}

// variedRequest derives each shape independently, so backpressure never redraws a sample.
func variedRequest(r submitRequest, ordinal int) submitRequest {
	v := r.Variation
	if v == nil {
		return r
	}
	sample := func(field string, minValue, maxValue int) int {
		input := planVersion + ":" + strconv.FormatUint(*v.Seed, 10) + ":" + strconv.Itoa(ordinal) + ":" + field
		digest := sha256.Sum256([]byte(input))
		// #nosec G115 -- The modulo result is bounded by validated ranges of at most 4097.
		return minValue + int(binary.BigEndian.Uint64(digest[:8])%uint64(maxValue-minValue+1))
	}
	if len(v.Functions) > 0 {
		r.Function = v.Functions[sample("function", 0, len(v.Functions)-1)]
	}
	for _, field := range []struct {
		name  string
		value *intRange
		dest  *int
	}{
		{"writes", v.Writes, &r.Writes}, {"reads", v.Reads, &r.Reads}, {"payloadBytes", v.PayloadBytes, &r.PayloadBytes},
	} {
		if field.value != nil {
			*field.dest = sample(field.name, field.value.Min, field.value.Max)
		}
	}
	return r
}
