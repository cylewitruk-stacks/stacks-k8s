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

func TestPoX4PublicInputsRequireCurrentAndCapturedIdentities(t *testing.T) {
	for _, change := range []string{
		"valid",
		"missing-signer-ledger",
		"signer-replaced",
		"holder-replaced",
		"genesis-replaced",
		"captured-signer-differs",
		"unverified-target",
		"late",
		"late-holder-replaced",
		"late-removed",
		"late-shared-key",
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
						{Name: "stacker", Kind: "StacksStacker"},
						{Name: "signer", Kind: "StacksSigner"},
						{Name: "node", Kind: "StacksNode"},
					},
				},
			}
			account := func(name, key string) *stacks.StacksAccount {
				public, _ := identity.FromPrivate(key)
				return &stacks.StacksAccount{
					ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "test", UID: types.UID(name), Generation: 1},
					Status: common.ResolutionStatus{
						ObservedGeneration: 1,
						Digest:             name + "-digest",
						Identity: &common.PublicIdentity{
							Address:   public.Address,
							PublicKey: public.PublicKey,
						},
						Conditions: []metav1.Condition{{Type: "Resolved", Status: metav1.ConditionTrue}},
					},
				}
			}
			holder := account("holder", strings.Repeat("0", 63)+"1")
			consensus := account("consensus", strings.Repeat("0", 63)+"2")
			binding := func(kind string, o client.Object, digest string) common.Binding {
				return common.Binding{Kind: kind, Name: o.GetName(), UID: o.GetUID(), Fingerprint: digest}
			}
			participant := func(logical string, kind api.ParticipantKind) *api.StacksNetworkParticipant {
				return &api.StacksNetworkParticipant{
					ObjectMeta: metav1.ObjectMeta{
						Name:            foundation.ParticipantName("root", logical),
						Namespace:       "test",
						UID:             types.UID(logical),
						Generation:      1,
						OwnerReferences: []metav1.OwnerReference{owner},
					},
					Spec: api.StacksNetworkParticipantSpec{
						NetworkUID:      "root",
						ParticipantName: logical,
						Kind:            kind,
					},
					Status: api.ParticipantStatus{Admission: &api.Admission{}},
				}
			}
			signer := participant("signer", "StacksSigner")
			signer.Status.Admission.Configuration.StacksSigner = &stacks.StacksSignerSpec{
				AccountRef: &common.NameRef{Name: consensus.Name},
			}
			signer.Status.Admission.Dependencies = []common.Binding{
				binding("StacksAccount", consensus, consensus.Status.Digest),
			}
			node := participant("node", "StacksNode")
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
			stacker := participant("stacker", "StacksStacker")
			stacker.Status.Admission.Configuration.StacksStacker = &stacks.StacksStackerSpec{
				HolderAccountRef:         &common.NameRef{Name: holder.Name},
				AdministratorAccountRef:  &common.NameRef{Name: holder.Name},
				SignerRef:                &common.NameRef{Name: "signer"},
				TargetNodeRef:            &common.NameRef{Name: "node"},
				AmountMicroSTX:           ptr.To(common.Amount("100000")),
				LockCycles:               ptr.To[int32](2),
				RenewWhenRemainingCycles: ptr.To[int32](1),
			}
			stacker.Status.Admission.Dependencies = []common.Binding{
				binding("StacksAccount", holder, holder.Status.Digest),
				binding("StacksNetworkParticipant", signer, ""),
				binding("StacksNetworkParticipant", node, ""),
			}
			root.Status.Identities = []api.InstanceIdentity{
				{Name: "stacker", UID: stacker.UID},
				{Name: "signer", UID: signer.UID},
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
						Epochs: []api.Epoch{{Name: "3.0", StartHeight: 252}, {Name: "4.0", StartHeight: 282}},
					},
					Bootstrap: api.Bootstrap{
						Gates: []api.Gate{
							{Name: "EnrollPoX4", BitcoinCeiling: 234, TargetCycle: ptr.To[int64](12)},
							{Name: "EnrollPoX5", BitcoinCeiling: 284, TargetCycle: ptr.To[int64](15)},
						},
						Requirements: []api.BootstrapRequirement{
							{
								Kind:         "StacksStacker",
								Participant:  binding("StacksNetworkParticipant", stacker, ""),
								Dependencies: stacker.Status.Admission.Dependencies,
								Accounts: []api.PublicAccount{
									{
										Binding:  binding("StacksAccount", holder, holder.Status.Digest),
										Identity: *holder.Status.Identity,
									},
								},
							},
							{
								Kind:        "StacksSigner",
								Participant: binding("StacksNetworkParticipant", signer, ""),
								Accounts: []api.PublicAccount{
									{
										Binding:  binding("StacksAccount", consensus, consensus.Status.Digest),
										Identity: *consensus.Status.Identity,
									},
								},
							},
						},
					},
				},
			}
			ref := binding("StacksGenesis", genesis, foundation.Digest(genesis.Spec))
			root.Status.GenesisRef = &ref
			root.Status.GenesisDigest = foundation.Digest(genesis.Spec.Chain)
			late := strings.HasPrefix(change, "late")
			if late {
				genesis.Spec.Bootstrap.Requirements[0].Participant.Name = "original-stacker"
				genesis.Spec.Bootstrap.Requirements[0].Participant.UID = "original-stacker"
				genesis.Spec.Bootstrap.Requirements[1].Participant.Name = "original-signer"
				genesis.Spec.Bootstrap.Requirements[1].Participant.UID = "original-signer"
				genesis.Spec.Bootstrap.Requirements[0].Accounts = nil
				genesis.Spec.Bootstrap.Requirements[1].Accounts = nil
				root.Status.GenesisRef.Fingerprint = foundation.Digest(genesis.Spec)
			}
			switch change {
			case "late-removed":
				root.Status.Identities[0].Removing = true
			case "late-shared-key":
				consensus.Status.Identity = holder.Status.Identity.DeepCopy()
			case "missing-signer-ledger":
				root.Status.Identities = append(root.Status.Identities[:1], root.Status.Identities[2:]...)
			case "signer-replaced":
				signer.UID = "replacement"
			case "holder-replaced", "late-holder-replaced":
				holder.UID = "replacement"
			case "genesis-replaced":
				genesis.UID = "replacement"
			case "captured-signer-differs":
				genesis.Spec.Bootstrap.Requirements[1].Accounts[0].Identity = *holder.Status.Identity
			case "unverified-target":
				node.Status.Conditions[1].Status = metav1.ConditionFalse
			}
			c := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(root, stacker, signer, node, holder, consensus, genesis).
				Build()
			input, err := (PublicInputs{Reader: c, Sender: holder.Status.Identity.Address}).PoX4(
				context.Background(),
				stacksworker.Snapshot{Network: root, Participant: stacker},
			)
			if change == "late" || change == "late-shared-key" {
				pox5, e := (PublicInputs{Reader: c, Sender: holder.Status.Identity.Address}).PoX5(
					context.Background(),
					stacksworker.Snapshot{Network: root, Participant: stacker},
				)
				if err != nil || e != nil || input.InitialCohort || pox5.InitialCohort || input.TargetCycle != 0 ||
					pox5.TargetCycle != 0 ||
					pox5.Administrator != holder.Status.Identity.Address ||
					pox5.SignerPublicKey != consensus.Status.Identity.PublicKey {
					t.Fatalf("late current public inputs: %+v %+v %v %v", input, pox5, err, e)
				}
				return
			}
			if change == "valid" {
				pox5, e := (PublicInputs{Reader: c, Sender: holder.Status.Identity.Address}).PoX5(
					context.Background(),
					stacksworker.Snapshot{Network: root, Participant: stacker},
				)
				if e != nil || pox5.TargetCycle != 15 || pox5.Epoch4Height != 282 ||
					pox5.Administrator != holder.Status.Identity.Address {
					t.Fatalf("PoX5 shared administrator binding: %+v %v", pox5, e)
				}
				admin := account("administrator", strings.Repeat("0", 63)+"3")
				if e = c.Create(context.Background(), admin); e != nil {
					t.Fatal(e)
				}
				stacker.Status.Admission.Configuration.StacksStacker.AdministratorAccountRef.Name = admin.Name
				stacker.Status.Admission.Dependencies = append(
					stacker.Status.Admission.Dependencies,
					binding("StacksAccount", admin, admin.Status.Digest),
				)
				if _, e = (PublicInputs{Reader: c, Sender: holder.Status.Identity.Address}).PoX5(
					context.Background(),
					stacksworker.Snapshot{Network: root, Participant: stacker},
				); e == nil {
					t.Fatal("uncaptured administrator accepted")
				}
				if err != nil || input.Node == nil || input.TargetCycle != 12 || input.EnrollmentCeiling != 234 ||
					input.Epoch3Height != 252 ||
					input.SignerPublicKey != consensus.Status.Identity.PublicKey {
					t.Fatalf("valid public inputs: %+v %v", input, err)
				}
			} else if err == nil {
				t.Fatal("invalid dependency authorized PoX inputs")
			}
		})
	}
}
