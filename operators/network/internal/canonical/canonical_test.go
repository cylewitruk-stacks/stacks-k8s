package canonical

import "testing"

func TestDigestIsKeyOrderIndependent(t *testing.T) {
	left, err := Digest(map[string]any{"z": 2, "a": map[string]any{"b": "Malmö", "a": 1}})
	if err != nil {
		t.Fatal(err)
	}
	right, err := Digest(map[string]any{"a": map[string]any{"a": 1, "b": "Malmö"}, "z": 2})
	if err != nil {
		t.Fatal(err)
	}
	if left != right {
		t.Fatalf("digests differ: %s != %s", left, right)
	}
}

func TestDigestRejectsFloatsAndUnsafeIntegers(t *testing.T) {
	for _, value := range []any{1.25, int64(9_007_199_254_740_992)} {
		if _, err := Digest(value); err == nil {
			t.Fatalf("Digest(%v) succeeded", value)
		}
	}
}
