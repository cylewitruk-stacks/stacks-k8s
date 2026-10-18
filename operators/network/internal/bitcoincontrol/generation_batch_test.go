package bitcoincontrol

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/foundation"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// setBatch freezes the first-gate batching choice into the fixture's initialization record.
func (f *testFixture) setBatch(t *testing.T, enabled bool) {
	t.Helper()
	initial := f.readInitial(t)
	initial.Spec.BatchGeneration = enabled
	if e := f.c.Update(context.Background(), initial); e != nil {
		t.Fatal(e)
	}
}

// generationCalls returns the recorded generation requests in order.
func (f *fakeRPC) generationCalls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, call := range f.calls {
		if strings.HasPrefix(call, "Generate") {
			out = append(out, call)
		}
	}
	return out
}

// The fixture's single miner wallet needs one output and doubles as the payout wallet.
func TestBatchedFirstGateReachesCeilingInFewRequests(t *testing.T) {
	for name, tc := range map[string]struct {
		enabled bool
		want    []string
	}{
		"batched": {enabled: true, want: []string{"Generate", "GenerateBlocks:202"}},
		// Disabled batching keeps single-block opportunities; three rounds suffice to show it.
		"single": {enabled: false, want: []string{"Generate", "Generate", "Generate"}},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			f.setBatch(t, tc.enabled)
			f.worker.Now = func() time.Time { return f.now }
			s := f.schedulerFor()
			if e := f.worker.Step(context.Background()); e != nil {
				t.Fatal(e)
			}
			f.reconcile(t, s)
			for round := range tc.want {
				f.now = f.now.Add(time.Second)
				f.reconcile(t, s)
				f.reconcile(t, s)
				offer := f.readRecord(t).Spec.Offer
				if offer == nil || offer.Ceiling != 203 || offer.ExpectedHeight != f.rpc.height ||
					offer.ExpectedHeight+offerBlocks(offer) > offer.Ceiling {
					t.Fatalf("round %d offer %+v", round, offer)
				}
				if e := f.worker.Step(context.Background()); e != nil {
					t.Fatalf("round %d: %v", round, e)
				}
				eventually(t, func() bool { return f.readRecord(t).Status.CompletedOffer == offer.Number })
				if e := f.worker.Step(context.Background()); e != nil {
					t.Fatal(e)
				}
				f.reconcile(t, s)
			}
			initial, record := f.readInitial(t), f.readRecord(t)
			if got := f.rpc.generationCalls(); !slices.Equal(got, tc.want) {
				t.Fatalf("generation requests %v, want %v", got, tc.want)
			}
			if !tc.enabled {
				if initial.Status.PreparedAt != nil || record.Status.BlocksGenerated != 3 || f.rpc.height != 3 {
					t.Fatalf("single-block generation changed: %+v", initial.Status)
				}
				return
			}
			if initial.Status.PreparedAt == nil || initial.Status.Reason != "NextGateNotImplemented" ||
				initial.Status.LastAccountedOffer != int64(len(tc.want)) ||
				len(initial.Status.Funded) != 1 || initial.Status.Funded[0].Outputs != 203 ||
				record.Status.BlocksGenerated != 203 || f.rpc.height != 203 {
				t.Fatalf(
					"unexpected first-gate result %+v, generated %d",
					initial.Status,
					record.Status.BlocksGenerated,
				)
			}
			before := f.rpc.count()
			for range 3 {
				f.now = f.now.Add(time.Second)
				f.reconcile(t, s)
				_ = f.worker.Step(context.Background())
			}
			if f.rpc.count() != before {
				t.Fatal("crossed frozen first ceiling")
			}
		})
	}
}

func TestOfferCountAuthorization(t *testing.T) {
	initial := &bitcoin.BitcoinInitialization{Spec: bitcoin.BitcoinInitializationSpec{
		MinimumHeight:   1000,
		BatchGeneration: true,
	}}
	unbatched := &bitcoin.BitcoinInitialization{Spec: bitcoin.BitcoinInitializationSpec{MinimumHeight: 103}}
	offer := func(count int32, height, ceiling int64, mode bitcoin.OfferMode) *bitcoin.BitcoinBlockOffer {
		return &bitcoin.BitcoinBlockOffer{Count: count, ExpectedHeight: height, Ceiling: ceiling, Mode: mode}
	}
	for name, tc := range map[string]struct {
		initial *bitcoin.BitcoinInitialization
		offer   *bitcoin.BitcoinBlockOffer
		want    bool
	}{
		"single-omitted":     {unbatched, offer(0, 10, 103, ""), true},
		"single-explicit":    {unbatched, offer(1, 10, 103, bitcoin.OfferBootstrap), true},
		"single-later-gate":  {initial, offer(1, 1020, 1034, bitcoin.OfferBootstrap), true},
		"batch-first-gate":   {initial, offer(500, 10, 1000, bitcoin.OfferBootstrap), true},
		"batch-to-ceiling":   {initial, offer(3, 997, 1000, ""), true},
		"batch-disabled":     {unbatched, offer(2, 10, 103, bitcoin.OfferBootstrap), false},
		"batch-above-limit":  {initial, offer(501, 10, 1000, bitcoin.OfferBootstrap), false},
		"batch-past-ceiling": {initial, offer(4, 997, 1000, bitcoin.OfferBootstrap), false},
		"batch-later-gate":   {initial, offer(2, 1020, 1034, bitcoin.OfferBootstrap), false},
		"batch-baseline":     {initial, offer(2, 10, 1000, bitcoin.OfferBaseline), false},
		"negative-count":     {initial, offer(-1, 10, 1000, bitcoin.OfferBootstrap), false},
		"at-ceiling":         {initial, offer(0, 1000, 1000, bitcoin.OfferBootstrap), false},
	} {
		t.Run(name, func(t *testing.T) {
			if got := offerCountAuthorized(tc.initial, tc.offer); got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestBootstrapBatchSizing(t *testing.T) {
	initial := func(enabled bool, minimum int64) *bitcoin.BitcoinInitialization {
		return &bitcoin.BitcoinInitialization{Spec: bitcoin.BitcoinInitializationSpec{
			MinimumHeight:   minimum,
			BatchGeneration: enabled,
		}}
	}
	for name, tc := range map[string]struct {
		initial                    *bitcoin.BitcoinInitialization
		height, ceiling, remaining int64
		want                       int32
	}{
		"disabled":        {initial(false, 103), 0, 103, 0, 0},
		"payout-to-gate":  {initial(true, 103), 2, 103, 0, 101},
		"payout-limited":  {initial(true, 1000), 2, 1000, 0, 500},
		"miner-remaining": {initial(true, 103), 0, 103, 2, 2},
		"miner-single":    {initial(true, 103), 0, 103, 1, 0},
		"last-block":      {initial(true, 103), 102, 103, 0, 0},
		"later-gate":      {initial(true, 103), 110, 134, 0, 0},
	} {
		t.Run(name, func(t *testing.T) {
			if got := bootstrapBatch(tc.initial, tc.height, tc.ceiling, tc.remaining); got != tc.want {
				t.Fatalf("got %d, want %d", got, tc.want)
			}
		})
	}
}

// A count the frozen initialization does not permit is never sent, even when both offer copies agree.
func TestWorkerRejectsUnauthorizedOfferCount(t *testing.T) {
	for name, tc := range map[string]struct {
		enabled bool
		height  int64
		count   int32
	}{
		"disabled":      {enabled: false, height: 0, count: 5},
		"past-ceiling":  {enabled: true, height: 200, count: 5},
		"beyond-client": {enabled: true, height: 0, count: 501},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			f.setBatch(t, tc.enabled)
			f.offer(t, tc.height)
			initial := f.readInitial(t)
			initial.Status.Offer.Count = tc.count
			if e := f.c.Status().Update(context.Background(), initial); e != nil {
				t.Fatal(e)
			}
			record := f.readRecord(t)
			record.Spec.Offer = initial.Status.Offer.DeepCopy()
			if e := f.c.Update(context.Background(), record); e != nil {
				t.Fatal(e)
			}
			_ = f.worker.Step(context.Background())
			f.worker.workers.Wait()
			if calls := f.rpc.generationCalls(); len(calls) != 0 {
				t.Fatalf("unauthorized count generated blocks: %v", calls)
			}
		})
	}
}

// Freezing copies the batching choice from captured requirements; omission stays false so records
// frozen before batching existed keep an identical spec.
func TestFreezeInitializationCapturesBatchGeneration(t *testing.T) {
	f := newFixture(t)
	node := api.StacksNetworkParticipant{
		ObjectMeta: metav1.ObjectMeta{Name: "node", Namespace: "test", UID: "node-uid"},
		Spec:       api.StacksNetworkParticipantSpec{ParticipantName: "bitcoin", Kind: api.ParticipantBitcoinNode},
	}
	production := api.StacksNetworkParticipant{
		ObjectMeta: metav1.ObjectMeta{Name: "production", Namespace: "test", UID: "production-uid"},
		Spec: api.StacksNetworkParticipantSpec{
			ParticipantName: "production",
			Kind:            api.ParticipantBitcoinBlockProduction,
		},
	}
	wallet := common.Binding{
		Kind:        bitcoin.KindBitcoinWallet,
		Name:        "miner",
		UID:         "wallet-uid",
		Fingerprint: "wallet-digest",
	}
	for name, tc := range map[string]struct {
		batch *bool
		want  bool
	}{
		"omitted":  {batch: nil, want: false},
		"disabled": {batch: ptr.To(false), want: false},
		"enabled":  {batch: ptr.To(true), want: true},
	} {
		t.Run(name, func(t *testing.T) {
			genesis := &api.StacksGenesis{
				ObjectMeta: metav1.ObjectMeta{Name: "genesis", Namespace: "test", UID: "genesis-uid"},
				Spec: api.StacksGenesisSpec{Bootstrap: api.Bootstrap{
					Gates: []api.Gate{{Name: api.GatePrepareBitcoin, BitcoinCeiling: 103}},
					Requirements: []api.BootstrapRequirement{
						{Kind: api.ParticipantBitcoinNode, Participant: binding("StacksNetworkParticipant", &node)},
						{
							Kind:                api.ParticipantBitcoinBlockProduction,
							Participant:         binding("StacksNetworkParticipant", &production),
							Dependencies:        []common.Binding{wallet},
							BitcoinPayoutWallet: &wallet,
							BitcoinInitialization: &bitcoin.Initialization{
								TargetNodeRef:         common.NameRef{Name: "bitcoin"},
								MinimumHeight:         103,
								MinerWalletRefs:       &[]common.NameRef{{Name: "miner"}},
								MatureOutputsPerMiner: 2,
								BatchGeneration:       tc.batch,
							},
						},
					},
				}},
			}
			spec, err := freezeInitialization(
				context.Background(), f.c, f.root, genesis, []api.StacksNetworkParticipant{node, production},
			)
			if err != nil {
				t.Fatal(err)
			}
			if spec.BatchGeneration != tc.want || spec.MinimumHeight != 103 || spec.MatureOutputsPerMiner != 2 ||
				len(spec.MinerWallets) != 1 || spec.PayoutWallet.Address != testAddress {
				t.Fatalf("unexpected frozen spec %+v", spec)
			}
		})
	}
}

// TestReplacementProducerPreservesBatchGeneration checks both scheduler admission paths against frozen batching.
func TestReplacementProducerPreservesBatchGeneration(t *testing.T) {
	for name, tc := range map[string]struct {
		frozen   bool
		declared *bool
		accepted bool
	}{
		"omitted-disabled":       {frozen: false, declared: nil, accepted: true},
		"explicit-disabled":      {frozen: false, declared: ptr.To(false), accepted: true},
		"enabled":                {frozen: true, declared: ptr.To(true), accepted: true},
		"enable-frozen-disabled": {frozen: false, declared: ptr.To(true)},
		"disable-frozen-enabled": {frozen: true, declared: ptr.To(false)},
		"omit-frozen-enabled":    {frozen: true, declared: nil},
	} {
		t.Run(name, func(t *testing.T) {
			f := baselineFixture(t)
			f.setBatch(t, tc.frozen)
			f.production.Status.Admission.Configuration.BitcoinBlockProduction.Initialization.BatchGeneration = tc.declared
			f.production.Status.Admission.PolicyDigest = foundation.Digest(f.production.Status.Admission.Configuration)
			replaceBootstrapProducer(t, f, "replacement")
			if err := f.c.Get(t.Context(), client.ObjectKeyFromObject(f.root), f.root); err != nil {
				t.Fatal(err)
			}
			var replacement api.StacksNetworkParticipant
			if err := f.c.Get(t.Context(), client.ObjectKey{
				Namespace: f.root.Namespace,
				Name:      foundation.ParticipantName(string(f.root.UID), "replacement"),
			}, &replacement); err != nil {
				t.Fatal(err)
			}
			s := f.schedulerFor()
			initial := f.readInitial(t)
			_, productionErr := s.currentProduction(t.Context(), f.root, initial)
			_, baselineErr := baselineInputs(t.Context(), s.Reader, f.root, initial, &replacement)
			for path, err := range map[string]error{"production": productionErr, "baseline": baselineErr} {
				if (err == nil) != tc.accepted {
					t.Errorf("%s accepted=%t, want %t: %v", path, err == nil, tc.accepted, err)
				}
			}
		})
	}
}
