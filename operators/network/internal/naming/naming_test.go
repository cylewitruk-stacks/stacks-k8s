package naming

import "testing"

func TestChildIsStableBoundedAndCollisionResistant(t *testing.T) {
	short := Child("network", "miner")
	if short != "network-miner" {
		t.Fatalf("short name = %q", short)
	}
	left := Child("network-with-an-extremely-long-name-that-needs-truncation", "actor-with-a-long-name-one")
	right := Child("network-with-an-extremely-long-name-that-needs-truncation", "actor-with-a-long-name-two")
	if len(left) > 63 || len(right) > 63 {
		t.Fatalf("names exceed DNS limit: %q %q", left, right)
	}
	if left == right {
		t.Fatalf("distinct inputs collided at %q", left)
	}
	if left != Child("network-with-an-extremely-long-name-that-needs-truncation", "actor-with-a-long-name-one") {
		t.Fatal("name is not stable")
	}
}
