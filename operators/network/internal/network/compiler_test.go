package network

import (
	"reflect"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	networkv1alpha1 "github.com/cylewitruk-stacks/stacks-k8s/operators/network/api/v1alpha1"
)

func TestCompileProducesFocusedResolvedLeafResources(t *testing.T) {
	network := fixture()
	desired, err := Compile(network)
	if err != nil {
		t.Fatal(err)
	}
	if len(desired.BitcoinNodes) != 1 || len(desired.StacksNodes) != 2 || len(desired.Signers) != 1 {
		t.Fatalf("unexpected topology sizes: %#v", desired)
	}
	bitcoin := desired.BitcoinNodes[0]
	if bitcoin.Name != "testnet-bitcoin" || bitcoin.Spec.Image != "bitcoin:test" || bitcoin.Spec.NetworkRef.Name != "testnet" {
		t.Fatalf("unexpected Bitcoin child: %#v", bitcoin.Spec)
	}
	if bitcoin.Spec.DependencyImage != defaultDependencyImage || !strings.Contains(bitcoin.Spec.DependencyImage, "@sha256:") {
		t.Fatalf("dependency image is not the immutable default: %q", bitcoin.Spec.DependencyImage)
	}
	signerNode := desired.StacksNodes[1]
	if signerNode.Spec.SignerRef == nil || signerNode.Spec.SignerRef.Name != "testnet-signer-1" || signerNode.Spec.SignerIndex == nil || *signerNode.Spec.SignerIndex != 0 {
		t.Fatalf("signer node binding missing: %#v", signerNode.Spec)
	}
	if desired.Signers[0].Spec.NodeRef.Name != signerNode.Name {
		t.Fatalf("signer node ref = %q", desired.Signers[0].Spec.NodeRef.Name)
	}
	for _, object := range desired.Objects() {
		if object.value == nil {
			t.Fatalf("%s/%s is nil", object.kind, object.name)
		}
	}
}

func TestCompileRejectsAmbiguousAndUnsafeTopology(t *testing.T) {
	tests := map[string]func(*networkv1alpha1.StacksNetwork){
		"duplicate actor":   func(value *networkv1alpha1.StacksNetwork) { value.Spec.StacksNodes[0].Name = "bitcoin" },
		"unknown burnchain": func(value *networkv1alpha1.StacksNetwork) { value.Spec.StacksNodes[0].BitcoinNodeRef = "missing" },
		"shared signer node": func(value *networkv1alpha1.StacksNetwork) {
			duplicate := value.Spec.Signers[0]
			duplicate.Name = "signer-2"
			value.Spec.Signers = append(value.Spec.Signers, duplicate)
		},
		"duplicate signer index": func(value *networkv1alpha1.StacksNetwork) {
			duplicate := value.Spec.Signers[0]
			duplicate.Name = "signer-2"
			duplicate.NodeRef = "follower"
			value.Spec.StacksNodes[0].Role = networkv1alpha1.StacksNodeSigner
			value.Spec.Signers = append(value.Spec.Signers, duplicate)
		},
		"public signer config": func(value *networkv1alpha1.StacksNetwork) { value.Spec.Signers[0].Config = generatedStacks() },
		"generated miner secret": func(value *networkv1alpha1.StacksNetwork) {
			value.Spec.StacksNodes[0].Role = networkv1alpha1.StacksNodeMiner
			value.Spec.StacksNodes[0].Config = generatedStacks()
		},
		"unknown bitcoin peer": func(value *networkv1alpha1.StacksNetwork) { value.Spec.BitcoinNodes[0].PeerRefs = []string{"missing"} },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			value := fixture()
			mutate(value)
			if _, err := Compile(value); err == nil {
				t.Fatal("Compile succeeded")
			}
		})
	}
}

func TestCompileIgnoresMapListSourceOrder(t *testing.T) {
	value := fixture()
	baseline, err := Compile(value)
	if err != nil {
		t.Fatal(err)
	}
	value.Spec.StacksNodes[0], value.Spec.StacksNodes[1] = value.Spec.StacksNodes[1], value.Spec.StacksNodes[0]
	reordered, err := Compile(value)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(baseline, reordered) {
		t.Fatal("list-map source order changed the compiled topology")
	}
}

func TestCompileLimitsServiceBindingsToDirectAndExplicitDependencies(t *testing.T) {
	value := fixture()
	value.Spec.StacksNodes[0].ServiceRefs = []string{"signer-node-1"}
	baseline, err := Compile(value)
	if err != nil {
		t.Fatal(err)
	}
	if got := baseline.StacksNodes[0].Spec.ServiceMap; !reflect.DeepEqual(got, map[string]string{
		"bitcoin": "testnet-bitcoin", "signer-node-1": "testnet-signer-node-1",
	}) {
		t.Fatalf("follower service bindings = %#v", got)
	}
	if got := baseline.Signers[0].Spec.ServiceMap; !reflect.DeepEqual(got, map[string]string{"signer-node-1": "testnet-signer-node-1"}) {
		t.Fatalf("signer service bindings = %#v", got)
	}

	value.Spec.BitcoinNodes = append(value.Spec.BitcoinNodes, networkv1alpha1.BitcoinNodeTemplate{
		Name: "unrelated", Role: networkv1alpha1.BitcoinNodeFollower,
		Config: networkv1alpha1.ConfigSource{Generated: &networkv1alpha1.GeneratedConfig{Profile: "bitcoin-regtest/v1"}},
	})
	expanded, err := Compile(value)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(baseline.StacksNodes, expanded.StacksNodes) || !reflect.DeepEqual(baseline.Signers, expanded.Signers) {
		t.Fatal("adding an unrelated actor changed existing Stacks actor declarations")
	}
}

func TestCompileRejectsUnknownExplicitServiceReference(t *testing.T) {
	value := fixture()
	value.Spec.StacksNodes[0].ServiceRefs = []string{"missing"}
	if _, err := Compile(value); err == nil || !strings.Contains(err.Error(), "does not name a declared actor") {
		t.Fatalf("error = %v", err)
	}
}

func TestCompileRejectsNetworkNameThatCannotNameWorkloads(t *testing.T) {
	for _, name := range []string{"contains.dot", strings.Repeat("n", 64)} {
		value := fixture()
		value.Name = name
		if _, err := Compile(value); err == nil || !strings.Contains(err.Error(), "must be a DNS label") {
			t.Fatalf("name %q error = %v", name, err)
		}
	}
}

func fixture() *networkv1alpha1.StacksNetwork {
	return &networkv1alpha1.StacksNetwork{ObjectMeta: metav1.ObjectMeta{Name: "testnet", Namespace: "test", UID: "network-uid"}, Spec: networkv1alpha1.StacksNetworkSpec{
		Defaults:     networkv1alpha1.NetworkDefaults{BitcoinImage: "bitcoin:test", StacksNodeImage: "stacks:test", StacksSignerImage: "signer:test", ImagePullPolicy: corev1.PullIfNotPresent},
		BitcoinNodes: []networkv1alpha1.BitcoinNodeTemplate{{Name: "bitcoin", Role: networkv1alpha1.BitcoinNodeMiner, Config: networkv1alpha1.ConfigSource{Generated: &networkv1alpha1.GeneratedConfig{Profile: "bitcoin-regtest/v1"}}}},
		StacksNodes: []networkv1alpha1.StacksNodeTemplate{
			{Name: "follower", Role: networkv1alpha1.StacksNodeFollower, BitcoinNodeRef: "bitcoin", Config: generatedStacks()},
			{Name: "signer-node-1", Role: networkv1alpha1.StacksNodeSigner, BitcoinNodeRef: "bitcoin", Config: generatedStacks()},
		},
		Signers: []networkv1alpha1.StacksSignerTemplate{{Name: "signer-1", NodeRef: "signer-node-1", Index: 0, Weight: 1, Config: secret("signer-config")}},
	}}
}

func generatedStacks() networkv1alpha1.ConfigSource {
	return networkv1alpha1.ConfigSource{Generated: &networkv1alpha1.GeneratedConfig{Profile: "nakamoto-regtest-node/v1"}}
}
func secret(name string) networkv1alpha1.ConfigSource {
	return networkv1alpha1.ConfigSource{SecretRef: &networkv1alpha1.ConfigObjectRef{Name: name, Key: "signer.toml"}}
}

func TestCompileLongNamesRemainDistinct(t *testing.T) {
	value := fixture()
	value.Name = strings.Repeat("n", 50)
	value.Spec.BitcoinNodes = append(value.Spec.BitcoinNodes,
		networkv1alpha1.BitcoinNodeTemplate{Name: "bitcoin-long-one", Role: networkv1alpha1.BitcoinNodeFollower, Config: networkv1alpha1.ConfigSource{Generated: &networkv1alpha1.GeneratedConfig{Profile: "bitcoin-regtest/v1"}}},
		networkv1alpha1.BitcoinNodeTemplate{Name: "bitcoin-long-two", Role: networkv1alpha1.BitcoinNodeFollower, Config: networkv1alpha1.ConfigSource{Generated: &networkv1alpha1.GeneratedConfig{Profile: "bitcoin-regtest/v1"}}})
	desired, err := Compile(value)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, actor := range desired.BitcoinNodes {
		if len(actor.Name) > 63 || seen[actor.Name] {
			t.Fatalf("invalid compiled name %q", actor.Name)
		}
		seen[actor.Name] = true
	}
}

func TestCompileRequiresOnlyImagesUsedByTopology(t *testing.T) {
	value := fixture()
	value.Spec.StacksNodes = nil
	value.Spec.Signers = nil
	value.Spec.Defaults.StacksNodeImage = ""
	value.Spec.Defaults.StacksSignerImage = ""
	if _, err := Compile(value); err != nil {
		t.Fatalf("Bitcoin-only topology requires unused images: %v", err)
	}
	value.Spec.Defaults.BitcoinImage = ""
	if _, err := Compile(value); err == nil {
		t.Fatal("topology without a Bitcoin image compiled")
	}
	value.Spec.BitcoinNodes[0].Image = "bitcoin:actor-specific"
	if _, err := Compile(value); err != nil {
		t.Fatalf("actor-specific image was not accepted: %v", err)
	}
}
