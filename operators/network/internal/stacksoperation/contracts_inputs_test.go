package stacksoperation

import (
	"context"
	"strings"
	"testing"

	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	stacks "github.com/cylewitruk-stacks/stacks-k8s/apis/network/stacks/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/libs/stacks/identity"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/foundation"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/stacksworker"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestContractPublicInputsRequireCapturedRegistryAndCurrentIdentities(t *testing.T) {
	for _, change := range []string{
		"valid",
		"deployer-replaced",
		"registry-key-replaced",
		"registry-key-not-captured",
		"registry-policy-changed",
		"genesis-replaced",
		"unverified-target",
		"target-removed",
		"genesis-content-changed",
		"late",
		"late-aliases",
		"late-key-changed",
		"late-deployer-changed",
		"late-removed",
	} {
		t.Run(change, func(t *testing.T) {
			scheme := runtime.NewScheme()
			_ = api.AddToScheme(scheme)
			_ = stacks.AddToScheme(scheme)
			owner := metav1.OwnerReference{
				APIVersion: api.GroupVersion.String(),
				Kind:       "StacksNetwork",
				Name:       "network",
				UID:        "root",
				Controller: ptr.To(true),
			}
			root := &api.StacksNetwork{
				ObjectMeta: metav1.ObjectMeta{Name: "network", Namespace: "test", UID: "root"},
				Spec: api.StacksNetworkSpec{
					Participants: []api.Participant{
						{Name: "contracts", Kind: "StacksContractSet"},
						{Name: "node", Kind: "StacksNode"},
					},
				},
			}
			bind := func(kind string, o client.Object, hash string) common.Binding {
				return common.Binding{Kind: kind, Name: o.GetName(), UID: o.GetUID(), Fingerprint: hash}
			}
			accounts := []*stacks.StacksAccount{}
			for i, name := range []string{"deployer", "registry-1", "registry-2", "aggregate"} {
				public, _ := identity.FromPrivate(strings.Repeat("0", 63) + string(rune('1'+i)))
				accounts = append(
					accounts,
					&stacks.StacksAccount{
						ObjectMeta: metav1.ObjectMeta{
							Name:       name,
							Namespace:  "test",
							UID:        types.UID(name),
							Generation: 1,
						},
						Status: common.ResolutionStatus{
							ObservedGeneration: 1,
							Digest:             name + "-digest",
							Identity: &common.PublicIdentity{
								Address:   public.Address,
								PublicKey: public.PublicKey,
							},
							Conditions: []metav1.Condition{{Type: "Resolved", Status: metav1.ConditionTrue}},
						},
					},
				)
			}
			participant := func(name string, kind api.ParticipantKind) *api.StacksNetworkParticipant {
				return &api.StacksNetworkParticipant{
					ObjectMeta: metav1.ObjectMeta{
						Name:            foundation.ParticipantName("root", name),
						Namespace:       "test",
						UID:             types.UID(name),
						Generation:      1,
						OwnerReferences: []metav1.OwnerReference{owner},
					},
					Spec:   api.StacksNetworkParticipantSpec{NetworkUID: "root", ParticipantName: name, Kind: kind},
					Status: api.ParticipantStatus{Admission: &api.Admission{}},
				}
			}
			p := participant("contracts", "StacksContractSet")
			node := participant("node", "StacksNode")
			initialization := &stacks.RegistryInitialization{
				Mode:                   "ExplicitTestRegistry",
				SignerAccountRefs:      []common.NameRef{{Name: "registry-1"}, {Name: "registry-2"}},
				AggregateKeyAccountRef: common.NameRef{Name: "aggregate"},
				Threshold:              2,
			}
			p.Status.Admission.Configuration.StacksContractSet = &stacks.StacksContractSetSpec{
				DeployerAccountRef: &common.NameRef{Name: "deployer"},
				TargetNodeRef:      &common.NameRef{Name: "node"},
				Bundle:             ptr.To("sbtc-regtest-v1"),
				Initialization:     initialization,
			}
			p.Status.Admission.Dependencies = []common.Binding{bind("StacksNetworkParticipant", node, "")}
			captured := api.BootstrapRequirement{
				Kind:                   "StacksContractSet",
				Participant:            bind("StacksNetworkParticipant", p, ""),
				RegistryInitialization: initialization.DeepCopy(),
			}
			for _, account := range accounts {
				b := bind("StacksAccount", account, account.Status.Digest)
				p.Status.Admission.Dependencies = append(p.Status.Admission.Dependencies, b)
				captured.Accounts = append(
					captured.Accounts,
					api.PublicAccount{Binding: b, Identity: *account.Status.Identity},
				)
			}
			node.Status.Admission.Configuration.StacksNode = &stacks.StacksNodeSpec{}
			node.Status.Admission.PolicyDigest = foundation.Digest(node.Status.Admission.Configuration)
			node.Status.Runtime = &api.ParticipantRuntimeStatus{
				ObservedGeneration: 1,
				PolicyDigest:       node.Status.Admission.PolicyDigest,
				PodRef:             &common.Binding{Kind: "Pod", Name: "node-0", UID: "pod"},
				ContainerID:        "containerd://node",
				Endpoints:          []api.RuntimeEndpoint{{Name: "rpc", Host: "node.test.svc", Port: 20443}},
			}
			node.Status.Conditions = []metav1.Condition{
				{Type: "WorkloadReady", Status: metav1.ConditionTrue, ObservedGeneration: 1},
				{Type: "ConfigVerified", Status: metav1.ConditionTrue, ObservedGeneration: 1},
			}
			root.Status.Identities = []api.InstanceIdentity{
				{Name: "contracts", UID: p.UID},
				{Name: "node", UID: node.UID},
			}
			genesis := &api.StacksGenesis{
				ObjectMeta: metav1.ObjectMeta{
					Name:            "genesis",
					Namespace:       "test",
					UID:             "genesis",
					OwnerReferences: []metav1.OwnerReference{owner},
				},
				Spec: api.StacksGenesisSpec{
					Source: api.GenesisSource{NetworkUID: "root"},
					Chain: api.Chain{
						Epochs: []api.Epoch{{Name: "3.0", StartHeight: 252}},
						Contracts: api.ContractBindings{
							Deployer:     accounts[0].Status.Identity.Address,
							Bundle:       "sbtc-regtest-v1",
							SourceHashes: map[string]string{"sbtc-registry": "source-pin"},
						},
					},
					Bootstrap: api.Bootstrap{Requirements: []api.BootstrapRequirement{captured}},
				},
			}
			root.Status.GenesisRef = ptr.To(bind("StacksGenesis", genesis, foundation.Digest(genesis.Spec)))
			root.Status.GenesisDigest = foundation.Digest(genesis.Spec.Chain)
			late := strings.HasPrefix(change, "late")
			if late {
				genesis.Spec.Bootstrap.Requirements[0].Participant.Name = "original-contracts"
				genesis.Spec.Bootstrap.Requirements[0].Participant.UID = "original-contracts"
				root.Status.GenesisRef.Fingerprint = foundation.Digest(genesis.Spec)
			}
			switch change {
			case "late-removed":
				root.Status.Identities[0].Removing = true
			case "late-key-changed":
				accounts[1].Status.Identity = accounts[2].Status.Identity.DeepCopy()
			case "late-deployer-changed":
				accounts[0].Status.Identity = accounts[2].Status.Identity.DeepCopy()
			case "late-aliases":
				for i, account := range accounts {
					account.Name = "alias-" + account.Name
					account.UID = types.UID(account.Name)
					p.Status.Admission.Dependencies[i+1] = bind("StacksAccount", account, account.Status.Digest)
				}
				p.Status.Admission.Configuration.StacksContractSet.DeployerAccountRef.Name = accounts[0].Name
				initialization.SignerAccountRefs[0].Name = accounts[1].Name
				initialization.SignerAccountRefs[1].Name = accounts[2].Name
				initialization.AggregateKeyAccountRef.Name = accounts[3].Name
			case "deployer-replaced":
				accounts[0].UID = "replacement"
			case "registry-key-replaced":
				accounts[1].UID = "replacement"
			case "registry-key-not-captured":
				genesis.Spec.Bootstrap.Requirements[0].Accounts = genesis.Spec.Bootstrap.Requirements[0].Accounts[:1]
				root.Status.GenesisRef.Fingerprint = foundation.Digest(genesis.Spec)
			case "registry-policy-changed":
				initialization.SignerAccountRefs[0].Name = "registry-2"
			case "genesis-replaced":
				genesis.UID = "replacement"
			case "unverified-target":
				node.Status.Conditions[1].Status = metav1.ConditionFalse
			case "target-removed":
				root.Status.Identities[1].Removing = true
			case "genesis-content-changed":
				genesis.Spec.Chain.Contracts.SourceHashes["sbtc-registry"] = "changed"
			}
			objects := []client.Object{root, p, node, genesis}
			for _, account := range accounts {
				objects = append(objects, account)
			}
			c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
			in, err := (PublicInputs{Reader: c, Sender: accounts[0].Status.Identity.Address}).Contracts(
				context.Background(),
				stacksworker.Snapshot{Network: root, Participant: p},
			)
			if change == "valid" || change == "late" || change == "late-aliases" {
				if err != nil || in.Node == nil || len(in.SignerPublicKeys) != 2 ||
					in.SignerPublicKeys[0] != accounts[1].Status.Identity.PublicKey ||
					in.AggregatePublicKey != accounts[3].Status.Identity.PublicKey ||
					in.Epoch3Height != 252 {
					t.Fatalf("valid contract inputs: %+v %v", in, err)
				}
			} else if err == nil {
				t.Fatal("invalid current/frozen binding accepted")
			}
		})
	}
}
