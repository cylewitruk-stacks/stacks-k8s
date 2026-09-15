package foundation

import (
	"reflect"
	"strings"
	"testing"

	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	stacks "github.com/cylewitruk-stacks/stacks-k8s/apis/network/stacks/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"k8s.io/utils/ptr"
)

func TestBitcoinProductionScheduleDefaultAndExplicitSources(t *testing.T) {
	fixed := func(interval common.Duration) *bitcoin.BitcoinBlockScheduleSpec {
		return &bitcoin.BitcoinBlockScheduleSpec{
			Cadence: bitcoin.Cadence{Mode: bitcoin.CadenceFixed, Interval: &interval},
		}
	}
	for _, tc := range []struct {
		name  string
		input bitcoin.BitcoinBlockProductionSpec
		want  bitcoin.BitcoinBlockProductionSpec
	}{
		{name: "omitted", want: bitcoin.BitcoinBlockProductionSpec{Schedule: fixed("60s")}},
		{
			name:  "inline",
			input: bitcoin.BitcoinBlockProductionSpec{Schedule: fixed("5s")},
			want:  bitcoin.BitcoinBlockProductionSpec{Schedule: fixed("5s")},
		},
		{
			name:  "reference",
			input: bitcoin.BitcoinBlockProductionSpec{ScheduleRef: &common.NameRef{Name: "custom"}},
			want:  bitcoin.BitcoinBlockProductionSpec{ScheduleRef: &common.NameRef{Name: "custom"}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Compose(
				&api.StacksNetwork{},
				api.Participant{Kind: api.ParticipantBitcoinBlockProduction},
				tc.input,
			)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got.BitcoinBlockProduction, &tc.want) {
				t.Fatalf("production schedule: got %+v, want %+v", got.BitcoinBlockProduction, tc.want)
			}
		})
	}
}

func TestActorImageDefaultsUseCurrentOfficialReleases(t *testing.T) {
	for _, tc := range []struct {
		kind api.ParticipantKind
		want string
	}{
		{api.ParticipantBitcoinNode, "bitcoin/bitcoin:31.1"},
		{api.ParticipantStacksNode, "ghcr.io/stacks-network/stacks-core:4.0.3"},
		{api.ParticipantStacksSigner, "ghcr.io/stacks-network/stacks-signer:4.0.3"},
	} {
		got, err := Compose(&api.StacksNetwork{}, api.Participant{Kind: tc.kind}, nil)
		if err != nil {
			t.Fatal(err)
		}
		var image *string
		//nolint:exhaustive // The table deliberately contains only actor kinds with image defaults.
		switch tc.kind {
		case api.ParticipantBitcoinNode:
			image = got.BitcoinNode.Image
		case api.ParticipantStacksNode:
			image = got.StacksNode.Image
		case api.ParticipantStacksSigner:
			image = got.StacksSigner.Image
		}
		if image == nil || *image != tc.want {
			t.Fatalf("%s default image: got %v, want %s", tc.kind, image, tc.want)
		}
	}
}

func TestCompositionPrecedenceAndAlternatives(t *testing.T) {
	root := &api.StacksNetwork{
		Spec: api.StacksNetworkSpec{
			Defaults: &api.Defaults{
				Images:  &api.Images{StacksNode: ptr.To("root-image")},
				Storage: &common.Storage{Size: ptr.To("4Gi")},
			},
		},
	}
	entry := api.Participant{
		Kind: "StacksNode",
		Overrides: &api.Configuration{
			StacksNode: &stacks.StacksNodeSpec{
				ActorFields: common.ActorFields{
					Image:   ptr.To("override-image"),
					Storage: &common.Storage{Ephemeral: ptr.To(true)},
				},
			},
		},
	}
	source := stacks.StacksNodeSpec{
		ActorFields: common.ActorFields{Image: ptr.To("definition-image")},
		Peers:       &common.Peers{NodeRefs: ptr.To([]common.NameRef{{Name: "peer"}})},
	}
	result, err := Compose(root, entry, source)
	if err != nil {
		t.Fatal(err)
	}
	n := result.StacksNode
	if *n.Image != "override-image" || !*n.Storage.Ephemeral || n.Storage.Size != nil || len(*n.Peers.NodeRefs) != 1 ||
		n.Peers.Discovery != nil {
		t.Fatalf("precedence/exclusivity: %+v", n)
	}
	if *root.Spec.Defaults.Storage.Size != "4Gi" {
		t.Fatal("mutated defaults")
	}
}

func TestGateWindows(t *testing.T) {
	got, err := gates(DefaultEpochs(), api.PoX{RewardCycleLength: 20, PrepareLength: 5})
	if err != nil {
		t.Fatal(err)
	}
	for i, want := range []int64{203, 234, 251, 281, 294, 299} {
		if got[i].BitcoinCeiling != want {
			t.Fatalf("gate %d: %v", i, got[i])
		}
	}
	bad := DefaultEpochs()
	bad[13].StartHeight = 294
	if _, err := gates(bad, api.PoX{RewardCycleLength: 20, PrepareLength: 5}); err == nil {
		t.Fatal("accepted late enrollment")
	}
	bad = DefaultEpochs()
	bad[5].Name = "4.0"
	if validateEpochs(bad) == nil {
		t.Fatal("accepted unordered epochs")
	}
}

func TestPoX5GateUsesEnrollmentCutoffRatherThanActivationOffset(t *testing.T) {
	for _, activation := range []int64{282, 286, 292, 293} {
		epochs := DefaultEpochs()
		epochs[13].StartHeight = activation
		got, err := gates(epochs, api.PoX{RewardCycleLength: 20, PrepareLength: 5})
		if activation == 293 {
			if err == nil {
				t.Fatal("accepted activation without the minimum confirmation window")
			}
			continue
		}
		if err != nil || got[4].BitcoinCeiling != 294 || *got[4].TargetCycle != 15 || got[5].BitcoinCeiling != 299 {
			t.Fatalf("activation %d: gates=%+v error=%v", activation, got, err)
		}
	}
}

func TestRuntimeNamesBindUIDsAndRespectBounds(t *testing.T) {
	long := strings.Repeat("a", 253)
	a := RuntimeName("network-a", "participant-a", "StacksNode", long, "actor")
	if len(a) > 52 || a == RuntimeName("network-b", "participant-a", "StacksNode", long, "actor") ||
		a == RuntimeName("network-a", "participant-b", "StacksNode", long, "actor") {
		t.Fatal("runtime identity is not isolated")
	}
	if len(DefaultAccountName(long, "identity")) > 63 {
		t.Fatal("default account name exceeds label bound")
	}
}

func TestExplicitEmptyListClearsInheritedSeeds(t *testing.T) {
	root := &api.StacksNetwork{}
	source := stacks.StacksNodeSpec{Peers: &common.Peers{NodeRefs: ptr.To([]common.NameRef{{Name: "peer"}})}}
	entry := api.Participant{
		Kind: "StacksNode",
		Overrides: &api.Configuration{
			StacksNode: &stacks.StacksNodeSpec{Peers: &common.Peers{NodeRefs: ptr.To([]common.NameRef{})}},
		},
	}
	result, err := Compose(root, entry, source)
	if err != nil {
		t.Fatal(err)
	}
	if result.StacksNode.Peers.NodeRefs == nil || len(*result.StacksNode.Peers.NodeRefs) != 0 ||
		result.StacksNode.Peers.Discovery != nil {
		t.Fatal("explicit empty peer list was lost or defaulted")
	}
}

func TestMinersRequireExplicitIdentity(t *testing.T) {
	for _, mining := range []*stacks.Mining{
		{Enabled: ptr.To(true)},
		{BitcoinWalletRef: &common.NameRef{Name: "future-wallet"}},
	} {
		if allowsDefaultIdentity(&stacks.StacksNodeSpec{Mining: mining}) {
			t.Fatal("miner received implicit identity")
		}
	}
	if !allowsDefaultIdentity(&stacks.StacksNodeSpec{}) {
		t.Fatal("follower default identity disabled")
	}
}

func TestProtectedBindingsExcludeMutableSelection(t *testing.T) {
	a := []common.Binding{{Kind: "StacksAccount", Name: "sender", UID: "sender-uid"}}
	b := append(
		append([]common.Binding{}, a...),
		common.Binding{Kind: "StacksAccount", Name: "recipient", UID: "recipient-uid"},
	)
	if !sameIdentities(a, b) {
		t.Fatal("new mutable reference rejected")
	}
	b[0].UID = "replacement"
	if sameIdentities(a, b) {
		t.Fatal("same-name replacement accepted")
	}
	old := api.Configuration{
		StacksTransactionProduction: &stacks.StacksTransactionProductionSpec{
			AccountRef:    &common.NameRef{Name: "sender"},
			TargetNodeRef: &common.NameRef{Name: "node"},
		},
	}
	next := *old.DeepCopy()
	next.StacksTransactionProduction.Recipient = &stacks.Recipient{AccountRef: &common.NameRef{Name: "recipient"}}
	if sameProtectedConfiguration("StacksTransactionProduction", old, next) {
		t.Fatal("recipient silently rebound")
	}
	next.StacksTransactionProduction.TargetNodeRef = &common.NameRef{Name: "other-node"}
	if sameProtectedConfiguration("StacksTransactionProduction", old, next) {
		t.Fatal("ingress silently rebound")
	}
}
