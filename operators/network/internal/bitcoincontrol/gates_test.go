package bitcoincontrol

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	stacks "github.com/cylewitruk-stacks/stacks-k8s/apis/network/stacks/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/foundation"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// nextGateFixture captures a ready actor and a bound pending stacker after Bitcoin preparation.
func nextGateFixture(t *testing.T) (*testFixture, *api.StacksNetworkParticipant, *api.StacksNetworkParticipant) {
	t.Helper()
	f := newFixture(t)
	ctx := context.Background()
	f.worker.Now = func() time.Time { return f.now }
	f.rpc.height = 203
	actor := f.node.DeepCopy()
	actor.Name = "stacks"
	actor.UID = "stacks-uid"
	actor.ResourceVersion = ""
	actor.Spec.Kind = "StacksNode"
	actor.Spec.ParticipantName = "stacks"
	actor.Status.Admission.Configuration = api.Configuration{StacksNode: &stacks.StacksNodeSpec{}}
	actor.Status.Admission.PolicyDigest = foundation.Digest(actor.Status.Admission.Configuration)
	actor.Status.Runtime.PolicyDigest = actor.Status.Admission.PolicyDigest
	actor.Status.Runtime.ObservedGeneration = 1
	actor.Status.Conditions = []metav1.Condition{
		{Type: "WorkloadReady", Status: metav1.ConditionTrue, ObservedGeneration: 1},
		{Type: "ConfigVerified", Status: metav1.ConditionTrue, ObservedGeneration: 1},
	}
	stacker := f.node.DeepCopy()
	stacker.Name = "stacker"
	stacker.UID = "stacker-uid"
	stacker.ResourceVersion = ""
	stacker.Spec.Kind = "StacksStacker"
	stacker.Spec.ParticipantName = "stacker"
	stacker.Status.Admission.Configuration = api.Configuration{StacksStacker: &stacks.StacksStackerSpec{}}
	stacker.Status.Admission.PolicyDigest = foundation.Digest(stacker.Status.Admission.Configuration)
	digest := "sha256:" + strings.Repeat("e", 64)
	stacker.Status.Execution = &api.WorkerExecutionStatus{
		PodUID:              "worker-pod",
		ProcessNonce:        "worker-process",
		ProfileDigest:       digest,
		AppliedPolicyDigest: stacker.Status.Admission.PolicyDigest,
		ObservedGeneration:  1,
		NetworkGeneration:   1,
		Phase:               "Active",
		ObservedAt:          metav1.NewTime(f.now),
		Pending:             1,
		Transactions:        &api.TransactionExecutionStatus{Offered: 1, LastTxID: strings.Repeat("a", 64)},
	}
	for _, p := range []*api.StacksNetworkParticipant{actor, stacker} {
		if e := f.c.Create(ctx, p); e != nil {
			t.Fatal(e)
		}
	}
	var genesis api.StacksGenesis
	if e := f.c.Get(ctx, client.ObjectKey{Namespace: "test", Name: "genesis"}, &genesis); e != nil {
		t.Fatal(e)
	}
	genesis.Spec.Bootstrap.Requirements = []api.BootstrapRequirement{
		{Kind: "StacksNode", MiningEnabled: ptr.To(true), Participant: binding("StacksNetworkParticipant", actor)},
		{Kind: "StacksStacker", Participant: binding("StacksNetworkParticipant", stacker)},
	}
	if e := f.c.Update(ctx, &genesis); e != nil {
		t.Fatal(e)
	}
	ref := binding("StacksGenesis", &genesis)
	ref.Fingerprint = foundation.Digest(genesis.Spec)
	initial := f.readInitial(t)
	initial.Spec.Genesis = ref
	if e := f.c.Update(ctx, initial); e != nil {
		t.Fatal(e)
	}
	initial.Status.PreparedAt = ptr.To(metav1.NewTime(f.now))
	initial.Status.Funded = []bitcoin.BitcoinFundingCount{{WalletUID: "wallet-uid", Outputs: 203}}
	if e := f.c.Status().Update(ctx, initial); e != nil {
		t.Fatal(e)
	}
	root := f.root.DeepCopy()
	root.Spec.Participants = append(
		root.Spec.Participants,
		api.Participant{Name: "stacks", Kind: "StacksNode"},
		api.Participant{Name: "stacker", Kind: "StacksStacker"},
	)
	if e := f.c.Update(ctx, root); e != nil {
		t.Fatal(e)
	}
	root.Status.GenesisRef = &ref
	root.Status.Identities = append(
		root.Status.Identities,
		api.InstanceIdentity{Name: "stacks", UID: actor.UID},
		api.InstanceIdentity{
			Name: "stacker",
			UID:  stacker.UID,
			Worker: &api.WorkerSession{
				Pod:           api.WorkerPodBinding{Kind: "Pod", Name: "worker", UID: "worker-pod"},
				ProfileDigest: digest,
			},
		},
	)
	root.Status.Initialization = &api.InitializationStatus{
		GenesisUID:        genesis.UID,
		GenesisDigest:     root.Status.GenesisDigest,
		GateIndex:         1,
		AuthorizedCeiling: 234,
		Gates: []api.GateObservation{
			{Name: "PrepareBitcoin", CompletedAt: ptr.To(metav1.NewTime(f.now))},
			{Name: "EnrollPoX4"},
		},
	}
	if e := f.c.Status().Update(ctx, root); e != nil {
		t.Fatal(e)
	}
	f.root = root
	return f, actor, stacker
}

func TestDynamicGateAuthorityRejectsSkippedOrChangedRequirements(t *testing.T) {
	f, _, _ := nextGateFixture(t)
	ctx := context.Background()
	initial := f.readInitial(t)
	if gate, e := currentGate(ctx, f.c, f.root, initial); e != nil || gate.gate.BitcoinCeiling != 234 {
		t.Fatalf("valid next gate: %+v %v", gate, e)
	}
	for _, change := range []string{"prior", "ceiling", "digest", "order", "future-complete"} {
		t.Run(change, func(t *testing.T) {
			root := f.root.DeepCopy()
			switch change {
			case "prior":
				root.Status.Initialization.Gates[0].CompletedAt = nil
			case "ceiling":
				root.Status.Initialization.AuthorizedCeiling = 235
			case "digest":
				root.Status.GenesisDigest = "sha256:" + strings.Repeat("b", 64)
			case "order":
				root.Status.Initialization.Gates[1].Name = "PrepareNakamoto"
			case "future-complete":
				root.Status.Initialization.Gates[1].CompletedAt = ptr.To(metav1.NewTime(f.now))
			}
			if _, e := currentGate(ctx, f.c, root, initial); e == nil {
				t.Fatal("changed gate authority accepted")
			}
		})
	}
}

// TestPoX5ConfirmationWindow exercises actual offers/receipts while native PoX reads are unavailable.
func TestPoX5ConfirmationWindow(t *testing.T) {
	for _, ceiling := range []int64{284, 294} {
		t.Run(fmt.Sprintf("frozen-ceiling-%d", ceiling), func(t *testing.T) {
			f, actor, _ := nextGateFixture(t)
			ctx := context.Background()
			actor.Status.Runtime.Protocol = &api.StacksProtocolObservation{
				Available:  false,
				Reason:     "ObservationUnavailable",
				BurnHeight: 282,
			}
			if err := f.c.Status().Update(ctx, actor); err != nil {
				t.Fatal(err)
			}
			var genesis api.StacksGenesis
			if err := f.c.Get(ctx, client.ObjectKey{Namespace: "test", Name: "genesis"}, &genesis); err != nil {
				t.Fatal(err)
			}
			genesis.Spec.Bootstrap.Gates = []api.Gate{
				{Name: "PrepareBitcoin", BitcoinCeiling: 203},
				{Name: "EnrollPoX4", BitcoinCeiling: 234},
				{Name: "PrepareNakamoto", BitcoinCeiling: 251},
				{Name: "PreparePoX5", BitcoinCeiling: 281},
				{Name: "EnrollPoX5", BitcoinCeiling: ceiling, TargetCycle: ptr.To(int64(15))},
				{Name: "PrepareWaterfall", BitcoinCeiling: 299},
			}
			if err := f.c.Update(ctx, &genesis); err != nil {
				t.Fatal(err)
			}
			ref := binding("StacksGenesis", &genesis)
			ref.Fingerprint = foundation.Digest(genesis.Spec)
			initial := f.readInitial(t)
			initial.Spec.Genesis = ref
			if err := f.c.Update(ctx, initial); err != nil {
				t.Fatal(err)
			}
			root := f.root.DeepCopy()
			root.Status.GenesisRef = &ref
			state := root.Status.Initialization
			state.GateIndex, state.AuthorizedCeiling, state.Gates = 4, ceiling, nil
			for i, gate := range genesis.Spec.Bootstrap.Gates {
				observation := api.GateObservation{Name: gate.Name}
				if i < 4 {
					observation.CompletedAt = ptr.To(metav1.NewTime(f.now))
				}
				state.Gates = append(state.Gates, observation)
			}
			if err := f.c.Status().Update(ctx, root); err != nil {
				t.Fatal(err)
			}
			f.root, f.rpc.height = root, 284
			scheduler := f.schedulerFor()
			if err := f.worker.Step(ctx); err != nil {
				t.Fatal(err)
			}
			f.reconcile(t, scheduler)
			for height := int64(285); height <= ceiling; height++ {
				f.now = f.now.Add(time.Second)
				f.reconcile(t, scheduler)
				f.reconcile(t, scheduler)
				offer := f.readRecord(t).Spec.Offer
				if offer == nil || offer.ExpectedHeight != height-1 || offer.Ceiling != ceiling {
					t.Fatalf("confirmation at %d unavailable: %+v", height, offer)
				}
				if err := f.worker.Step(ctx); err != nil {
					t.Fatal(err)
				}
				eventually(t, func() bool { return f.readRecord(t).Status.CompletedOffer == height-284 })
				if err := f.worker.Step(ctx); err != nil {
					t.Fatal(err)
				}
				f.reconcile(t, scheduler)
			}
			for range 3 {
				f.now = f.now.Add(time.Second)
				f.reconcile(t, scheduler)
				_ = f.worker.Step(ctx)
			}
			if got := f.readInitial(
				t,
			); got.Status.Reason != "FrozenGateReached" || got.Status.Funded[0].Outputs != 203 ||
				f.rpc.count() != int(ceiling-284) {
				t.Fatalf("cutoff/funding changed: calls=%d status=%+v", f.rpc.count(), got.Status)
			}
		})
	}
}

func TestLaterBitcoinProgressDoesNotReplayFundingAndRequiresCurrentDemand(t *testing.T) {
	f, actor, stacker := nextGateFixture(t)
	ctx := context.Background()
	scheduler := f.schedulerFor()
	if e := f.worker.Step(ctx); e != nil {
		t.Fatal(e)
	}
	f.reconcile(t, scheduler)
	f.now = f.now.Add(time.Second)
	f.reconcile(t, scheduler)
	f.reconcile(t, scheduler)
	if offer := f.readRecord(t).Spec.Offer; offer == nil || offer.Ceiling != 234 || offer.ExpectedHeight != 203 {
		t.Fatalf("next gate offer missing: %+v", offer)
	}
	if e := f.worker.Step(ctx); e != nil {
		t.Fatal(e)
	}
	eventually(t, func() bool { return f.readRecord(t).Status.CompletedOffer == 1 })
	f.rpc.mu.Lock()
	f.rpc.height = 210
	f.rpc.mu.Unlock()
	if e := f.worker.Step(ctx); e != nil {
		t.Fatal(e)
	}
	stacker.Status.Execution.Pending = 0
	if e := f.c.Status().Update(ctx, stacker); e != nil {
		t.Fatal(e)
	}
	f.reconcile(t, scheduler)
	if state := f.readInitial(t).Status; state.Reason != "AwaitingEnrollmentDemand" || state.Funded[0].Outputs != 203 {
		t.Fatalf("post-preparation funding/demand changed: %+v", state)
	}
	stacker.Status.Execution.Pending = 1
	stacker.Status.Execution.ObservedAt = metav1.NewTime(f.now)
	if e := f.c.Status().Update(ctx, stacker); e != nil {
		t.Fatal(e)
	}
	f.now = f.now.Add(2 * time.Second)
	if e := f.worker.Step(ctx); e != nil {
		t.Fatal(e)
	}
	f.reconcile(t, scheduler)
	f.reconcile(t, scheduler)
	if offer := f.readRecord(t).Spec.Offer; offer == nil || offer.Number != 2 || offer.ExpectedHeight != 210 {
		t.Fatalf("pending enrollment did not get one confirmation opportunity: %+v", offer)
	}
	actor.Spec.Control = &api.Control{Suspended: ptr.To(true)}
	if e := f.c.Update(ctx, actor); e != nil {
		t.Fatal(e)
	}
	if e := f.worker.Step(ctx); e == nil {
		t.Fatal("last-send actor suspension did not withdraw authorization")
	}
	if f.rpc.count() != 1 {
		t.Fatal("actor suspension raced a second generation")
	}
	f.rpc.mu.Lock()
	f.rpc.height = 234
	f.rpc.mu.Unlock()
	_ = f.worker.Step(ctx)
	f.reconcile(t, scheduler)
	if state := f.readInitial(t).Status; state.Reason != "FrozenGateReached" || state.PreparedAt == nil {
		t.Fatalf("later frozen ceiling not held: %+v", state)
	}
}

func TestLegacyDemandRequiresBoundFreshWorker(t *testing.T) {
	f, _, stacker := nextGateFixture(t)
	ctx := context.Background()
	initial := f.readInitial(t)
	authority, e := currentGate(ctx, f.c, f.root, initial)
	if e != nil {
		t.Fatal(e)
	}
	for _, change := range []string{"none", "stale", "pod", "paused"} {
		t.Run(change, func(t *testing.T) {
			p := stacker.DeepCopy()
			if e := f.c.Get(ctx, client.ObjectKeyFromObject(p), p); e != nil {
				t.Fatal(e)
			}
			original := p.DeepCopy()
			switch change {
			case "none":
				p.Status.Execution.Pending = 0
			case "stale":
				p.Status.Execution.ObservedAt = metav1.NewTime(f.now.Add(-time.Minute))
			case "pod":
				p.Status.Execution.PodUID = "another-pod"
			case "paused":
				p.Status.Execution.Phase = "Paused"
			}
			if e := f.c.Status().Update(ctx, p); e != nil {
				t.Fatal(e)
			}
			ready, e := advancementReady(ctx, f.c, f.root, initial, authority, 210, f.now)
			if e != nil || ready {
				t.Fatalf("invalid demand accepted: %v", e)
			}
			original.ResourceVersion = p.ResourceVersion
			if e := f.c.Status().Update(ctx, original); e != nil {
				t.Fatal(e)
			}
		})
	}
}

// TestFirstAnchorDemandAllowsConfirmationButNotMissingOrStaleNativeEvidence covers startup's first Bitcoin dependency.
func TestFirstAnchorDemandAllowsConfirmationButNotMissingOrStaleNativeEvidence(t *testing.T) {
	f, actor, stacker := nextGateFixture(t)
	ctx := context.Background()
	stacker.Status.Execution.Pending = 0
	if err := f.c.Status().Update(ctx, stacker); err != nil {
		t.Fatal(err)
	}
	runtime := actor.Status.Runtime
	runtime.ConfigurationDigest = "configuration"
	runtime.Protocol = &api.StacksProtocolObservation{
		Reason:              "AwaitingFirstAnchor",
		NetworkID:           0x80000000,
		BurnHeight:          210,
		StacksTip:           strings.Repeat("0", 64),
		FullySynced:         true,
		ObservedAt:          metav1.NewTime(f.now),
		GenesisUID:          f.root.Status.GenesisRef.UID,
		PodUID:              runtime.PodRef.UID,
		ContainerID:         runtime.ContainerID,
		ConfigurationDigest: runtime.ConfigurationDigest,
	}
	if err := f.c.Status().Update(ctx, actor); err != nil {
		t.Fatal(err)
	}
	authority, err := currentGate(ctx, f.c, f.root, f.readInitial(t))
	if err != nil {
		t.Fatal(err)
	}
	if ready, err := advancementReady(ctx, f.c, f.root, f.readInitial(t), authority, 210, f.now); err != nil || !ready {
		t.Fatalf("first anchor cannot be confirmed: %v %v", ready, err)
	}
	original := runtime.Protocol.DeepCopy()
	for name, change := range map[string]func(*api.StacksProtocolObservation){
		"failed read": func(v *api.StacksProtocolObservation) { v.Reason = "ObservationUnavailable" },
		"stale": func(v *api.StacksProtocolObservation) {
			v.ObservedAt = metav1.NewTime(f.now.Add(-17 * time.Second))
		},
		"future": func(v *api.StacksProtocolObservation) {
			v.ObservedAt = metav1.NewTime(f.now.Add(time.Second))
		},
		"old pod":           func(v *api.StacksProtocolObservation) { v.PodUID = "old-pod" },
		"old process":       func(v *api.StacksProtocolObservation) { v.ContainerID = "old-container" },
		"old config":        func(v *api.StacksProtocolObservation) { v.ConfigurationDigest = "old-config" },
		"old genesis":       func(v *api.StacksProtocolObservation) { v.GenesisUID = "old-genesis" },
		"catching up":       func(v *api.StacksProtocolObservation) { v.FullySynced = false },
		"behind bitcoin":    func(v *api.StacksProtocolObservation) { v.BurnHeight-- },
		"established chain": func(v *api.StacksProtocolObservation) { v.HighestStacksHeight = 1 },
		"wrong network":     func(v *api.StacksProtocolObservation) { v.NetworkID = 1 },
	} {
		t.Run(name, func(t *testing.T) {
			actor.Status.Runtime.Protocol = original.DeepCopy()
			change(actor.Status.Runtime.Protocol)
			if err := f.c.Status().Update(ctx, actor); err != nil {
				t.Fatal(err)
			}
			if ready, err := advancementReady(
				ctx,
				f.c,
				f.root,
				//nolint:contextcheck // Fixture reads use testing.T.Context, independent of the exercised operation deadline.
				f.readInitial(t),
				authority,
				210,
				f.now,
			); err != nil ||
				ready {
				t.Fatalf("invalid startup evidence accepted: %v %v", ready, err)
			}
		})
	}
	actor.Status.Runtime.Protocol = nil
	if err := f.c.Status().Update(ctx, actor); err != nil {
		t.Fatal(err)
	}
	if ready, err := advancementReady(ctx, f.c, f.root, f.readInitial(t), authority, 210, f.now); err != nil || ready {
		t.Fatalf("missing native evidence accepted: %v %v", ready, err)
	}
}

// TestFirstAnchorDemandEndsForTheCohort prevents empty followers or miners extending startup.
func TestFirstAnchorDemandEndsForTheCohort(t *testing.T) {
	f, miner, stacker := nextGateFixture(t)
	ctx := context.Background()
	stacker.Status.Execution.Pending = 0
	if err := f.c.Status().Update(ctx, stacker); err != nil {
		t.Fatal(err)
	}
	empty := &api.StacksProtocolObservation{
		Reason:              "AwaitingFirstAnchor",
		NetworkID:           0x80000000,
		BurnHeight:          210,
		StacksTip:           strings.Repeat("0", 64),
		FullySynced:         true,
		ObservedAt:          metav1.NewTime(f.now),
		GenesisUID:          f.root.Status.GenesisRef.UID,
		PodUID:              miner.Status.Runtime.PodRef.UID,
		ContainerID:         miner.Status.Runtime.ContainerID,
		ConfigurationDigest: miner.Status.Runtime.ConfigurationDigest,
	}
	follower := miner.DeepCopy()
	follower.Name, follower.UID, follower.ResourceVersion = "follower", "follower-uid", ""
	follower.Spec.ParticipantName = "follower"
	follower.Status.Runtime.Protocol = empty.DeepCopy()
	if err := f.c.Create(ctx, follower); err != nil {
		t.Fatal(err)
	}
	f.root.Spec.Participants = append(f.root.Spec.Participants, api.Participant{
		Name: "follower",
		Kind: "StacksNode",
	})
	f.root.Status.Identities = append(
		f.root.Status.Identities,
		api.InstanceIdentity{Name: "follower", UID: follower.UID},
	)
	authority, err := currentGate(ctx, f.c, f.root, f.readInitial(t))
	if err != nil {
		t.Fatal(err)
	}
	authority.genesis.Spec.Bootstrap.Requirements = append(
		authority.genesis.Spec.Bootstrap.Requirements,
		api.BootstrapRequirement{
			Kind:          "StacksNode",
			MiningEnabled: ptr.To(false),
			Participant:   binding("StacksNetworkParticipant", follower),
		},
	)
	for _, tc := range []struct {
		name                        string
		minerHeight, followerHeight uint64
		minerMissing, want          bool
	}{
		{name: "empty miner and follower", want: true},
		{name: "anchored miner empty follower", minerHeight: 1},
		{name: "empty miner established follower", followerHeight: 1},
		{name: "empty follower is not mining demand", minerMissing: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			miner.Status.Runtime.Protocol = empty.DeepCopy()
			miner.Status.Runtime.Protocol.HighestStacksHeight = tc.minerHeight
			if tc.minerMissing {
				miner.Status.Runtime.Protocol = nil
			}
			follower.Status.Runtime.Protocol = empty.DeepCopy()
			follower.Status.Runtime.Protocol.HighestStacksHeight = tc.followerHeight
			for _, p := range []*api.StacksNetworkParticipant{miner, follower} {
				if err := f.c.Status().Update(ctx, p); err != nil {
					t.Fatal(err)
				}
			}
			//nolint:contextcheck // Fixture reads use testing.T.Context, independent of the exercised operation deadline.
			got, err := advancementReady(ctx, f.c, f.root, f.readInitial(t), authority, 210, f.now)
			if err != nil || got != tc.want {
				t.Fatalf("ready=%v err=%v", got, err)
			}
		})
	}
}
