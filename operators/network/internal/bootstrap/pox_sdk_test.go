//go:build sdk

package bootstrap

import (
	"context"
	"encoding/json"
	network "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha1"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/testsupport"
	"path/filepath"
	"testing"

	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/environment"
)

func TestPoXSigningSuppliesExplicitInputs(t *testing.T) {
	directory, err := filepath.Abs("../../transactions")
	if err != nil {
		t.Fatal(err)
	}
	doc, err := environment.StacksWithContracts(environment.StacksOptions{Namespace: "sdk", Name: "sdk", Image: "test:local", SDKDirectory: directory, Interval: 10}, testsupport.ContractSources())
	if err != nil {
		t.Fatal(err)
	}
	s := &session{options: Options{SDKDirectory: directory}}
	pox := poxInfo{Contract: pox4, BurnHeight: 210, RewardCycle: 10, CycleLength: 20}
	for _, operation := range []string{"enroll", "extend"} {
		tx, err := s.sign(context.Background(), doc.Bootstrap.Participants[0], operation, pox, accountState{Nonce: 1}, 20000000000000, 6, 1)
		if err != nil || !validTransaction(tx) {
			t.Fatalf("%s failed with Go-provided fee and observations: %v", operation, err)
		}
	}
}

func TestPoX5SigningAndRoleSeparation(t *testing.T) {
	directory, err := filepath.Abs("../../transactions")
	if err != nil {
		t.Fatal(err)
	}
	doc, err := environment.StacksWithContracts(environment.StacksOptions{Namespace: "pox5", Name: "pox5", Image: "test:local", SDKDirectory: directory, Interval: 10}, testsupport.ContractSources())
	if err != nil {
		t.Fatal(err)
	}
	participant := doc.Bootstrap.Participants[0]
	s := &session{options: Options{SDKDirectory: directory}}
	for _, operation := range []string{"enroll", "extend"} {
		pox := poxInfo{Contract: pox5, BurnHeight: 241, RewardCycle: 12, CycleLength: 20}
		pox.NextCycle.BlocksUntilPrepare = 14
		tx, err := s.sign(context.Background(), participant, operation, pox, accountState{Nonce: 1}, 100000000000, 12, 1)
		if err != nil || !validTransaction(tx) {
			t.Fatalf("%s: %v", operation, err)
		}
	}
	source, err := directManager(participant.Stacker.Address)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.offline(context.Background(), "contract-sign.mjs", map[string]any{"operation": "publish", "account": participant.Administrator, "fee": "30000", "nonce": "0", "contractName": "direct-signer", "clarityVersion": 6, "source": source}); err != nil {
		t.Fatal(err)
	}
}

func TestPoX5BootstrapRejectsChangedRoleBindings(t *testing.T) {
	directory, err := filepath.Abs("../../transactions")
	if err != nil {
		t.Fatal(err)
	}
	doc, err := environment.StacksWithContracts(environment.StacksOptions{Namespace: "bindings", Name: "bindings", Image: "test:local", SDKDirectory: directory, Interval: 10}, testsupport.ContractSources())
	if err != nil {
		t.Fatal(err)
	}
	parent := doc.Items[4].(*network.StacksNetwork)
	if err = ValidateGenesis(doc.Bootstrap, parent); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*environment.Bootstrap){
		func(s *environment.Bootstrap) {
			s.Participants[0].Manager = s.Participants[0].Stacker.Address + ".manager"
		},
		func(s *environment.Bootstrap) { s.Participants[0].Signer = "unknown" },
		func(s *environment.Bootstrap) {
			s.Participants[0].Consensus.PublicKey = s.Participants[0].Stacker.PublicKey
		},
		func(s *environment.Bootstrap) { s.Bridge.Threshold = 1 },
		func(s *environment.Bootstrap) { s.Bridge.Deployer.Address = s.Participants[0].Stacker.Address },
	} {
		data, _ := json.Marshal(doc.Bootstrap)
		var setup environment.Bootstrap
		_ = json.Unmarshal(data, &setup)
		mutate(&setup)
		if ValidateGenesis(setup, parent) == nil {
			t.Fatal("changed role binding accepted")
		}
	}
	if err = environment.ValidateBootstrapKeys(doc.Bootstrap, directory); err != nil {
		t.Fatal(err)
	}
	doc.Bootstrap.Participants[0].Consensus.PrivateKey = doc.Bootstrap.Participants[0].Stacker.PrivateKey
	if environment.ValidateBootstrapKeys(doc.Bootstrap, directory) == nil {
		t.Fatal("private/public signer mismatch accepted")
	}
}
