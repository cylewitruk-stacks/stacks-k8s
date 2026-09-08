package network

import (
	"testing"

	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/workload"
)

// TestBitcoinDeclarationDigestTracksTargetInputs documents the existing compiler boundary for R1.
func TestBitcoinDeclarationDigestTracksTargetInputs(t *testing.T) {
	value := fixture()
	original, err := Compile(value)
	if err != nil {
		t.Fatal(err)
	}
	baseline, err := workload.SpecDigest(original.BitcoinNodes[0].Spec)
	if err != nil {
		t.Fatal(err)
	}
	value.Generation++
	value.Spec.StacksNodes[0].Image = "stacks:unrelated-change"
	unrelated, err := Compile(value)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := workload.SpecDigest(unrelated.BitcoinNodes[0].Spec)
	if err != nil {
		t.Fatal(err)
	}
	if digest != baseline {
		t.Fatal("an unrelated Stacks image change changed the Bitcoin declaration digest")
	}
	value.Generation++
	value.Spec.BitcoinNodes[0].Image = "bitcoin:target-change"
	changed, err := Compile(value)
	if err != nil {
		t.Fatal(err)
	}
	digest, err = workload.SpecDigest(changed.BitcoinNodes[0].Spec)
	if err != nil {
		t.Fatal(err)
	}
	if digest == baseline {
		t.Fatal("a changed target still matches the earlier leaf declaration")
	}
}
