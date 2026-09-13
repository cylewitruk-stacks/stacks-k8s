package foundation

import (
	"testing"

	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	stacks "github.com/cylewitruk-stacks/stacks-k8s/apis/network/stacks/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

func TestBootstrapCompatibilityUsesExplicitRequirements(t *testing.T) {
	req := api.BootstrapRequirement{Kind: "StacksStacker", PolicyDigest: "original", AmountMicroSTX: ptr.To(common.Amount("100")), LockCycles: ptr.To[int32](6), RenewWhenRemainingCycles: ptr.To[int32](3)}
	policy := api.Configuration{StacksStacker: &stacks.StacksStackerSpec{AmountMicroSTX: ptr.To(common.Amount("100")), LockCycles: ptr.To[int32](6), RenewWhenRemainingCycles: ptr.To[int32](3)}}
	if !BootstrapPolicyCompatible(req, policy) {
		t.Fatal("matching captured values rejected")
	}
	policy.StacksStacker.WorkerPlacement = &common.Placement{NodeSelector: map[string]string{"node": "new"}}
	if !BootstrapPolicyCompatible(req, policy) {
		t.Fatal("compatible runtime policy rejected")
	}
	policy.StacksStacker.AmountMicroSTX = ptr.To(common.Amount("101"))
	if BootstrapPolicyCompatible(req, policy) {
		t.Fatal("conflicting stake accepted")
	}
}

func TestBootstrapWalletsPreserveOnlyInitialAttachments(t *testing.T) {
	req := api.BootstrapRequirement{Kind: "BitcoinNode", Dependencies: []common.Binding{{Kind: "BitcoinWallet", Name: "initial", UID: "initial-wallet"}, {Kind: "StacksNetworkParticipant", Name: "old-seed", UID: "old-seed"}}}
	policy := api.Configuration{BitcoinNode: &bitcoin.BitcoinNodeSpec{WalletRefs: ptr.To([]common.NameRef{{Name: "initial"}, {Name: "optional"}})}}
	if !BootstrapPolicyCompatible(req, policy) {
		t.Fatal("optional attachment or removed seed blocked bootstrap-compatible policy")
	}
	policy.BitcoinNode.WalletRefs = ptr.To([]common.NameRef{{Name: "optional"}})
	if BootstrapPolicyCompatible(req, policy) {
		t.Fatal("initial wallet removal accepted before funding gate")
	}
}

func TestBootstrapDeferralEndsAtLastAffectedVerifiedGate(t *testing.T) {
	for _, mode := range []string{"pending", "completed", "foreign-genesis", "fingerprint", "chain-digest", "wrong-name", "nonprefix", "unordered", "wrong-ceiling"} {
		t.Run(mode, func(t *testing.T) {
			g := &api.StacksGenesis{ObjectMeta: metav1.ObjectMeta{UID: "genesis"}, Spec: api.StacksGenesisSpec{Bootstrap: api.Bootstrap{Gates: []api.Gate{{Name: "PrepareBitcoin", BitcoinCeiling: 203}, {Name: "PreparePoX5", BitcoinCeiling: 281}, {Name: "PrepareWaterfall", BitcoinCeiling: 299}}}}}
			now := metav1.Now()
			root := &api.StacksNetwork{Status: api.StacksNetworkStatus{GenesisRef: &common.Binding{UID: g.UID, Fingerprint: Digest(g.Spec)}, GenesisDigest: Digest(g.Spec.Chain), Initialization: &api.InitializationStatus{GenesisUID: g.UID, GenesisDigest: Digest(g.Spec.Chain), GateIndex: 2, AuthorizedCeiling: 299, Gates: []api.GateObservation{{Name: "PrepareBitcoin", CompletedAt: &now}, {Name: "PreparePoX5", CompletedAt: &now}, {Name: "PrepareWaterfall"}}}}}
			switch mode {
			case "pending":
				root.Status.Initialization.GateIndex = 1
				root.Status.Initialization.Gates[1].CompletedAt = nil
				root.Status.Initialization.AuthorizedCeiling = 281
			case "foreign-genesis":
				root.Status.Initialization.GenesisUID = "other"
			case "fingerprint":
				root.Status.GenesisRef.Fingerprint = "other"
			case "chain-digest":
				root.Status.GenesisDigest = "other"
			case "wrong-name":
				root.Status.Initialization.Gates[1].Name = "other"
			case "nonprefix":
				root.Status.Initialization.Gates[0].CompletedAt = nil
			case "unordered":
				g.Spec.Bootstrap.Gates[1].BitcoinCeiling = 202
				root.Status.GenesisRef.Fingerprint = Digest(g.Spec)
			case "wrong-ceiling":
				root.Status.Initialization.AuthorizedCeiling = 300
			}
			if got := bootstrapPolicyPending(bootstrapCompletedGates(root, g), api.BootstrapRequirement{Kind: "StacksContractSet"}); got != (mode != "completed") {
				t.Fatalf("pending=%v for %s", got, mode)
			}
		})
	}
}
