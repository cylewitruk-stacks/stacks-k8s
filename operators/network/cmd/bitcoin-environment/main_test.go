package main

import "testing"

func TestTargetWeightBounds(t *testing.T) {
	for _, invalid := range []string{"", "0", "-1", "1001", "1,", "1,1,1,1,1,1,1,1,1"} {
		if _, err := parseWeights(invalid); err == nil {
			t.Fatalf("accepted %q", invalid)
		}
	}
	weights, err := parseWeights("1, 3,1000")
	if err != nil || len(weights) != 3 || weights[1] != 3 {
		t.Fatalf("weights=%v err=%v", weights, err)
	}
}
