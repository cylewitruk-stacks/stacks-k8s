//go:build live && bitcoinproduction

package integration

import (
	"context"
	"encoding/json"
	actionv1 "github.com/cylewitruk-stacks/stacks-k8s/apis/network/actions/v1alpha1"
	bitcoinv1 "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha1"
	networkv1 "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha1"
	"k8s.io/apimachinery/pkg/types"
	"testing"
	"time"
)

// TestLiveWeightedProduction qualifies two connected helper targets and per-target action exclusion.
func TestLiveWeightedProduction(t *testing.T) {
	c, key := bitcoinLiveClient(t)
	ctx := context.Background()
	otherKey := types.NamespacedName{Namespace: key.Namespace, Name: key.Name + "-bitcoin-2"}
	first := &bitcoinv1.BitcoinProductionTarget{}
	other := &bitcoinv1.BitcoinProductionTarget{}
	root := &bitcoinv1.BitcoinBlockProduction{}
	refresh := func() bool {
		return c.Get(ctx, productionTargetKey(key), first) == nil && c.Get(ctx, otherKey, other) == nil && c.Get(ctx, key, root) == nil
	}
	eventuallyLive(t, "both weighted targets acknowledge production", func() bool { return refresh() && first.Status.BlocksProduced >= 2 && other.Status.BlocksProduced >= 2 })
	uidFirst, uidOther := first.UID, other.UID
	t.Logf("initial acknowledged blocks: first=%d second=%d offered=%d", first.Status.BlocksProduced, other.Status.BlocksProduced, root.Status.Opportunities)
	// A finite action reserves one executor while another target continues baseline work.
	a := createGeneration(t, c, key, "weighted-isolation", 5, 3, time.Minute)
	waitGeneration(t, c, a, func(a *actionv1.BitcoinBlockGeneration) bool { return a.Status.Phase == "Active" })
	liveMust(t, c.Get(ctx, productionTargetKey(key), first))
	liveMust(t, c.Get(ctx, otherKey, other))
	heldBaseline, otherBaseline := first.Status.BlocksProduced, other.Status.BlocksProduced
	eventuallyLive(t, "other target advances during reservation", func() bool {
		return refresh() && first.Status.Action != nil && other.Status.BlocksProduced > otherBaseline
	})
	if first.Status.BlocksProduced != heldBaseline {
		t.Fatal("reserved target received baseline generation")
	}
	waitGeneration(t, c, a, func(a *actionv1.BitcoinBlockGeneration) bool { return a.Status.Phase == "Completed" })
	waitProduction(t, c, key, func(p *bitcoinv1.BitcoinProductionTarget) bool { return p.Status.Action == nil })
	t.Logf("finite action=%d blocks; other baseline advanced from %d to %d during reservation", a.Status.BlocksGenerated, otherBaseline, other.Status.BlocksProduced)
	// Pause both, then resume with one actor unavailable. Offered shares remain unchanged.
	updateNetwork(t, ctx, c, key, func(n *networkv1.StacksNetwork) { n.Spec.BitcoinBlockProduction.Paused = true })
	eventuallyLive(t, "both targets paused", func() bool {
		return refresh() && first.Status.Phase == "Paused" && other.Status.Phase == "Paused" && first.Status.DispatchState != "Armed" && other.Status.DispatchState != "Armed"
	})
	var firstTip, otherTip struct {
		Blocks        int64  `json:"blocks"`
		BestBlockHash string `json:"bestblockhash"`
	}
	eventuallyLive(t, "connected targets converge while paused", func() bool {
		liveMust(t, json.Unmarshal(observeBitcoinTarget(t, c, key, first.Name, "getblockchaininfo"), &firstTip))
		liveMust(t, json.Unmarshal(observeBitcoinTarget(t, c, key, other.Name, "getblockchaininfo"), &otherTip))
		return firstTip.Blocks > 0 && firstTip.BestBlockHash == otherTip.BestBlockHash
	})
	t.Logf("both Core nodes observed the same paused canonical tip at height %d", firstTip.Blocks)
	frozenFirst, frozenOther := first.Status.BlocksProduced, other.Status.BlocksProduced
	time.Sleep(3 * time.Second)
	if !refresh() || first.Status.BlocksProduced != frozenFirst || other.Status.BlocksProduced != frozenOther {
		t.Fatal("pause produced new baseline work")
	}
	updateNetwork(t, ctx, c, key, func(n *networkv1.StacksNetwork) {
		n.Spec.BitcoinNodes[0].Suspended = true
		n.Spec.BitcoinBlockProduction.Paused = false
		n.Spec.BitcoinBlockProduction.IntervalSeconds = 1
	})
	skipped := first.Status.OpportunitiesSkipped
	eventuallyLive(t, "unavailable target skips while peer advances", func() bool {
		return refresh() && first.Status.OpportunitiesSkipped > skipped && first.Status.LastSkipReason == "Unavailable" && other.Status.BlocksProduced >= frozenOther+3
	})
	if first.Status.BlocksProduced != frozenFirst || root.Spec.Policy.Targets[0].Weight != 1 || root.Spec.Policy.Targets[1].Weight != 3 {
		t.Fatal("unavailable target changed execution or declared weights")
	}
	t.Logf("unavailable first: skipped=%d; second baseline=%d", first.Status.OpportunitiesSkipped, other.Status.BlocksProduced)
	// Remove and re-add the target through parent intent; its execution UID persists.
	original := root.Spec.Policy.DeepCopy()
	updateNetwork(t, ctx, c, key, func(n *networkv1.StacksNetwork) {
		n.Spec.BitcoinBlockProduction.Targets = n.Spec.BitcoinBlockProduction.Targets[1:]
	})
	eventuallyLive(t, "one active target with two retained identities", func() bool { return refresh() && len(root.Spec.Policy.Targets) == 1 && len(root.Status.Targets) == 2 })
	updateNetwork(t, ctx, c, key, func(n *networkv1.StacksNetwork) {
		n.Spec.BitcoinNodes[0].Suspended = false
		n.Spec.BitcoinBlockProduction = original.DeepCopy()
		n.Spec.BitcoinBlockProduction.IntervalSeconds = 2
	})
	eventuallyLive(t, "re-added target resumes on retained ledger", func() bool {
		return refresh() && first.Status.BlocksProduced > frozenFirst && root.Spec.Policy.IntervalSeconds == 2
	})
	if first.UID != uidFirst || other.UID != uidOther {
		t.Fatal("membership update replaced an execution ledger")
	}
	liveMust(t, c.Delete(ctx, a))
	t.Logf("final acknowledged blocks: first=%d second=%d; both ledger UIDs retained", first.Status.BlocksProduced, other.Status.BlocksProduced)
}
