package main

import (
	"io"
	"strings"
	"testing"
)

func TestRejectsInvalidCommandAndInput(t *testing.T) {
	for _, args := range [][]string{nil, {"event"}, {"submit", "extra"}} {
		if run(t.Context(), args, strings.NewReader("{}"), io.Discard) == nil {
			t.Fatal("accepted invalid command")
		}
	}
	for _, input := range []string{`{"unknown":true}`, `{} {}`} {
		var request struct{}
		if decode(strings.NewReader(input), &request) == nil {
			t.Fatal("accepted invalid input")
		}
	}
}
