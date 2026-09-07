//go:build sdk

package bootstrap

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/environment"
)

func TestPoXSigningSuppliesExplicitInputs(t *testing.T) {
	directory, err := filepath.Abs("../../transactions")
	if err != nil {
		t.Fatal(err)
	}
	doc, err := environment.Stacks(environment.StacksOptions{Namespace: "sdk", Name: "sdk", Image: "test:local", SDKDirectory: directory, Interval: 10})
	if err != nil {
		t.Fatal(err)
	}
	s := &session{options: Options{SDKDirectory: directory}}
	pox := poxInfo{Contract: pox4, BurnHeight: 210, RewardCycle: 10, CycleLength: 20}
	for _, operation := range []string{"enroll", "extend"} {
		tx, err := s.sign(context.Background(), doc.Bootstrap, operation, pox, accountState{Nonce: 1}, 20000000000000, 6, 1)
		if err != nil || !validTransaction(tx) {
			t.Fatalf("%s failed with Go-provided fee and observations: %v", operation, err)
		}
	}
}
