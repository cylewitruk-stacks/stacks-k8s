package environment

import (
	"encoding/json"
	"fmt"

	stacks "github.com/cylewitruk-stacks/stacks-k8s/apis/network/stacks/v1alpha1"
	network "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha1"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/protocolcontracts"
	core "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// validateContractSources checks complete named artifacts before any resources are rendered.
func validateContractSources(sources []protocolcontracts.Source) error {
	required := map[string]bool{"sbtc-registry": false, "sbtc-token": false, "sbtc-bootstrap-signers": false, "sbtc-deposit": false, "sbtc-withdrawal": false}
	if len(sources) != len(required) {
		return fmt.Errorf("standard operation requires all five sBTC contract artifacts")
	}
	for _, source := range sources {
		seen, known := required[source.Name]
		if !known || seen || source.ClarityVersion != 3 || len(source.Source) == 0 || len(source.Source) > 128*1024 || Digest(source.Source) != "sha256:"+source.SHA256 {
			return fmt.Errorf("invalid or duplicate contract artifact %s", source.Name)
		}
		required[source.Name] = true
	}
	return nil
}

// managedResources binds standard protocol responsibilities without enrolling other funded accounts.
func managedResources(items []any, parent *network.StacksNetwork, setup Bootstrap, sources []protocolcontracts.Source, configDigest string) ([]any, error) {
	operation := &network.NetworkOperation{}
	participant, bridge := setup.Participants[0], setup.Bridge
	if bridge == nil {
		return nil, fmt.Errorf("standard contract deployment requires bridge identities")
	}
	secret := func(name string, key AccountKey) (stacks.ArtifactReference, error) {
		data, err := json.Marshal(key)
		if err != nil {
			return stacks.ArtifactReference{}, err
		}
		immutable := true
		items = append(items, &core.Secret{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Secret"}, ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: parent.Namespace},
			Type: core.SecretTypeOpaque, Immutable: &immutable, StringData: map[string]string{"key.json": string(data)}})
		return stacks.ArtifactReference{Name: name, Key: "key.json", Digest: Digest(string(data))}, nil
	}
	for _, entry := range []struct {
		name, kind, consumer string
		key                  AccountKey
	}{
		{"stacker", "StacksStackingParticipant", "signer", participant.Stacker},
		{"manager-admin", "StacksStackingParticipant", "signer", participant.Administrator},
		{"sbtc-deployer", "StacksContractSet", "sbtc", bridge.Deployer},
	} {
		ref, err := secret("stacks-managed-"+entry.name, entry.key)
		if err != nil {
			return nil, err
		}
		operation.Accounts = append(operation.Accounts, stacks.ManagedAccountPolicy{Name: entry.name, Address: entry.key.Address, Target: "signer-node", ConfigDigest: configDigest,
			KeySecretRef: ref, ConsumerKind: entry.kind, Consumer: entry.consumer})
	}
	consensus, err := secret("stacks-consensus-authorization", participant.Consensus)
	if err != nil {
		return nil, err
	}
	operation.Participants = []stacks.StackingPolicy{{Name: "signer", Signer: "signer", HolderAccount: "stacker", AdministratorAccount: "manager-admin", ConsensusKeySecretRef: consensus, AmountMicroSTX: 100000000000, LockCycles: 12}}
	set := stacks.ContractSetPolicy{Name: "sbtc", Account: "sbtc-deployer", Bridge: &stacks.BridgeInitialization{SignerPublicKeys: []string{bridge.Signers[0].PublicKey, bridge.Signers[1].PublicKey}, AggregatePublicKey: bridge.Aggregate.PublicKey}}
	// Dependencies describe contract references; the controller chooses a stable topological order.
	dependencies := map[string][]string{
		"sbtc-registry": {}, "sbtc-token": {"sbtc-registry"},
		"sbtc-bootstrap-signers": {"sbtc-registry", "sbtc-token"},
		"sbtc-deposit":           {"sbtc-registry", "sbtc-token", "sbtc-bootstrap-signers"},
		"sbtc-withdrawal":        {"sbtc-registry", "sbtc-token", "sbtc-bootstrap-signers", "sbtc-deposit"},
	}
	data := map[string]string{}
	for _, source := range sources {
		key := source.Name + ".clar"
		data[key] = source.Source
		set.Contracts = append(set.Contracts, stacks.ContractArtifact{Name: source.Name, ClarityVersion: int32(source.ClarityVersion), DependsOn: dependencies[source.Name],
			SourceRef: stacks.ArtifactReference{Name: "stacks-sbtc-contracts", Key: key, Digest: Digest(source.Source)}})
	}
	operation.ContractSets = []stacks.ContractSetPolicy{set}
	parent.Spec.Operation = operation
	immutable := true
	items = append(items, &core.ConfigMap{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "ConfigMap"}, ObjectMeta: metav1.ObjectMeta{Name: "stacks-sbtc-contracts", Namespace: parent.Namespace}, Immutable: &immutable, Data: data})
	return items, nil
}
