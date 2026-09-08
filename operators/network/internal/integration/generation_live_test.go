//go:build live && bitcoinproduction

package integration

import (
	"context"
	"fmt"
	"testing"
	"time"

	actionv1 "github.com/cylewitruk-stacks/stacks-k8s/apis/network/actions/v1alpha1"
	bitcoinv1 "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha1"
	networkv1 "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// createGeneration requests finite work only in the explicitly selected disposable environment.
func createGeneration(t *testing.T, c client.Client, key types.NamespacedName, name string, count, interval int32, timeout time.Duration) *actionv1.BitcoinBlockGeneration {
	t.Helper()
	waitActionQuota(t, c, key.Namespace, "bitcoinblockgenerations")
	a := &actionv1.BitcoinBlockGeneration{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: key.Namespace}, Spec: actionv1.BitcoinBlockGenerationSpec{NetworkRef: actionv1.LocalReference{Name: key.Name}, BitcoinNodeRef: actionv1.LocalReference{Name: key.Name + "-bitcoin"}, Count: count, Cadence: actionv1.GenerationCadence{Mode: "Fixed", IntervalSeconds: interval}, Address: "mipcBbFg9gMiCh81Kj8tqqdgoZub1ZJRfn", Timeout: metav1.Duration{Duration: timeout}}}
	liveMust(t, c.Create(context.Background(), a))
	return a
}

// waitGeneration observes the action resource's own durable lifecycle facts.
func waitGeneration(t *testing.T, c client.Client, a *actionv1.BitcoinBlockGeneration, accept func(*actionv1.BitcoinBlockGeneration) bool) *actionv1.BitcoinBlockGeneration {
	t.Helper()
	eventuallyLive(t, "generation lifecycle", func() bool { return c.Get(context.Background(), client.ObjectKeyFromObject(a), a) == nil && accept(a) })
	return a
}

func TestLiveFiniteGeneration(t *testing.T) {
	c, key := bitcoinLiveClient(t)
	ctx := context.Background()
	waitProduction(t, c, key, func(p *bitcoinv1.BitcoinProductionTarget) bool {
		return p.Status.BlocksProduced >= 2 && p.Status.DispatchState == "Idle"
	})
	a := createGeneration(t, c, key, "finite", 3, 3, 2*time.Minute)
	waitGeneration(t, c, a, func(a *actionv1.BitcoinBlockGeneration) bool { return a.Status.BlocksGenerated == 1 })
	p := &bitcoinv1.BitcoinProductionTarget{}
	liveMust(t, c.Get(ctx, productionTargetKey(key), p))
	baseline := p.Status.BlocksProduced
	updateNetwork(t, ctx, c, key, func(n *networkv1.StacksNetwork) {
		n.Spec.BitcoinBlockProduction.Paused = true
		n.Spec.BitcoinBlockProduction.IntervalSeconds = 2
		n.Spec.StacksNodes = []networkv1.StacksNodeTemplate{{Name: "unready", Role: "follower", BitcoinNodeRef: "bitcoin", Image: n.Spec.Defaults.BitcoinImage, Config: liveGeneratedStacks(), Container: &networkv1.ContainerOverride{Command: []string{"/deliberately-missing-test-binary"}}}}
	})
	waitGeneration(t, c, a, func(a *actionv1.BitcoinBlockGeneration) bool { return a.Status.Phase == "Completed" })
	paused := waitProduction(t, c, key, func(p *bitcoinv1.BitcoinProductionTarget) bool {
		return p.Status.Action == nil && p.Status.Phase == "Paused"
	})
	if a.Status.BlocksGenerated != 3 || a.Status.LastBlockHash == "" || paused.Status.BlocksProduced != baseline {
		t.Fatal("receipt attribution or baseline exclusion failed")
	}
	eventuallyLive(t, "action finalizer released", func() bool { return c.Get(ctx, client.ObjectKeyFromObject(a), a) == nil && len(a.Finalizers) == 0 })
	t.Logf("completed action %s: count=%d lastHash=%s baselineCount=%d", a.UID, a.Status.BlocksGenerated, a.Status.LastBlockHash, baseline)
	// Restart with an idle retained reservation during a deliberately long gap.
	b := createGeneration(t, c, key, "restart", 2, 20, 2*time.Minute)
	waitGeneration(t, c, b, func(a *actionv1.BitcoinBlockGeneration) bool { return a.Status.BlocksGenerated == 1 })
	producer := liveProducerPod(t, c, key.Namespace)
	liveMust(t, c.Delete(ctx, producer))
	eventuallyLive(t, "new producer between receipts", func() bool { return replacementProducerReady(c, key.Namespace, producer.UID) })
	waitGeneration(t, c, b, func(a *actionv1.BitcoinBlockGeneration) bool { return a.Status.Phase == "Completed" })
	if b.Status.BlocksGenerated != 2 {
		t.Fatal("restart lost finite progress")
	}
	waitProduction(t, c, key, func(p *bitcoinv1.BitcoinProductionTarget) bool { return p.Status.Action == nil })
	updateNetwork(t, ctx, c, key, func(n *networkv1.StacksNetwork) {
		n.Spec.BitcoinBlockProduction.Paused = false
		n.Spec.StacksNodes = nil
	})
	waitProduction(t, c, key, func(p *bitcoinv1.BitcoinProductionTarget) bool { return p.Status.BlocksProduced >= baseline+2 })
	updateNetwork(t, ctx, c, key, func(n *networkv1.StacksNetwork) { n.Spec.BitcoinBlockProduction.IntervalSeconds = 5 })
	t.Log("finite exclusion, latest policy, unrelated actor independence, idle restart and baseline resumption passed")
}

// TestLiveGenerationLateReceipt uses a fresh paused environment with the 15-second delay fixture.
func TestLiveGenerationLateReceipt(t *testing.T) {
	c, key := bitcoinLiveClient(t)
	waitProduction(t, c, key, func(p *bitcoinv1.BitcoinProductionTarget) bool {
		return p.Status.Phase == "Paused" && p.Status.DispatchID == ""
	})
	a := createGeneration(t, c, key, "late", 2, 1, 10*time.Second)
	waitBitcoinFixtureLog(t, key, "delaying delivery for 15 seconds")
	waitGeneration(t, c, a, func(a *actionv1.BitcoinBlockGeneration) bool { return a.Status.Phase == "Inconclusive" })
	finished := a.Status.FinishedAt.DeepCopy()
	waitGeneration(t, c, a, func(a *actionv1.BitcoinBlockGeneration) bool {
		return a.Status.BlocksGenerated == 1 && len(a.Finalizers) == 0
	})
	p := waitProduction(t, c, key, func(p *bitcoinv1.BitcoinProductionTarget) bool {
		return p.Status.Action == nil && p.Status.Phase == "Paused"
	})
	if a.Status.Phase != "Inconclusive" || !a.Status.FinishedAt.Equal(finished) || p.Status.BlocksProduced != 0 {
		t.Fatal("late receipt revised outcome or baseline count")
	}
	t.Logf("late receipt retained after deadline: action=%s dispatch=%s", a.UID, a.Status.LastDispatchID)
}

// TestLiveGenerationDroppedReceipt uses a fresh paused environment with the drop fixture.
func TestLiveGenerationDroppedReceipt(t *testing.T) {
	c, key := bitcoinLiveClient(t)
	ctx := context.Background()
	waitProduction(t, c, key, func(p *bitcoinv1.BitcoinProductionTarget) bool {
		return p.Status.Phase == "Paused" && p.Status.DispatchID == ""
	})
	a := createGeneration(t, c, key, "dropped", 2, 1, time.Minute)
	waitBitcoinFixtureLog(t, key, "dropping response connection")
	waitGeneration(t, c, a, func(a *actionv1.BitcoinBlockGeneration) bool { return a.Status.Phase == "Inconclusive" })
	p := waitProduction(t, c, key, func(p *bitcoinv1.BitcoinProductionTarget) bool {
		return p.Status.Phase == "Blocked" && p.Status.DispatchState == "Armed"
	})
	dispatch := p.Status.DispatchID
	liveMust(t, c.Delete(ctx, a))
	producer := liveProducerPod(t, c, key.Namespace)
	liveMust(t, c.Delete(ctx, producer))
	eventuallyLive(t, "replacement producer", func() bool { return replacementProducerReady(c, key.Namespace, producer.UID) })
	time.Sleep(3 * time.Second)
	liveMust(t, c.Get(ctx, productionTargetKey(key), p))
	liveMust(t, c.Get(ctx, client.ObjectKeyFromObject(a), a))
	if p.Status.DispatchID != dispatch || p.Status.DispatchState != "Armed" || p.Status.Action == nil || len(a.Finalizers) == 0 {
		t.Fatal("deletion/restart reopened unresolved action")
	}
	n := &networkv1.StacksNetwork{}
	liveMust(t, c.Get(ctx, key, n))
	liveMust(t, c.Delete(ctx, n))
	eventuallyLive(t, "abandoned action and ledger deleted", func() bool {
		return apierrors.IsNotFound(c.Get(ctx, productionTargetKey(key), p)) && apierrors.IsNotFound(c.Get(ctx, client.ObjectKeyFromObject(a), a))
	})
	t.Logf("dropped receipt retained across deletion/restart; environment abandonment cleared finalizers: %s", dispatch)
}

// TestLiveGenerationQuota verifies the chart's actual resource-count admission limit.
func TestLiveGenerationQuota(t *testing.T) {
	c, key := bitcoinLiveClient(t)
	ctx := context.Background()
	// A separate empty namespace is required to avoid deleting evidence from earlier suites.
	list := &actionv1.BitcoinBlockGenerationList{}
	liveMust(t, c.List(ctx, list, client.InNamespace(key.Namespace)))
	if len(list.Items) != 0 {
		t.Fatal("quota test requires no existing actions")
	}
	for i := 0; i < 64; i++ {
		createGeneration(t, c, types.NamespacedName{Namespace: key.Namespace, Name: "absent"}, fmt.Sprintf("quota-%02d", i), 1, 1, time.Minute)
	}
	extra := &actionv1.BitcoinBlockGeneration{}
	liveMust(t, c.Get(ctx, client.ObjectKey{Namespace: key.Namespace, Name: "quota-00"}, extra))
	extra.Name = "overflow"
	extra.ResourceVersion = ""
	extra.UID = ""
	if err := c.Create(ctx, extra); !apierrors.IsForbidden(err) {
		t.Fatalf("quota failed to reject 65th request: %v", err)
	}
	liveMust(t, c.DeleteAllOf(ctx, &actionv1.BitcoinBlockGeneration{}, client.InNamespace(key.Namespace)))
	t.Log("namespace quota admitted 64 requests and rejected the 65th")
}

// TestLiveGenerationInFlightShutdown requires a fresh paused delay-fixture network.
func TestLiveGenerationInFlightShutdown(t *testing.T) {
	c, key := bitcoinLiveClient(t)
	ctx := context.Background()
	waitProduction(t, c, key, func(p *bitcoinv1.BitcoinProductionTarget) bool {
		return p.Status.Phase == "Paused" && p.Status.DispatchID == ""
	})
	producer := liveProducerPod(t, c, key.Namespace)
	a := createGeneration(t, c, key, "drain", 1, 1, time.Minute)
	waitBitcoinFixtureLog(t, key, "delaying delivery for 15 seconds")
	armed := &bitcoinv1.BitcoinProductionTarget{}
	liveMust(t, c.Get(ctx, productionTargetKey(key), armed))
	if armed.Status.DispatchState != "Armed" || armed.Status.Action == nil {
		t.Fatal("shutdown did not overlap action dispatch")
	}
	liveMust(t, c.Delete(ctx, producer))
	waitPodLog(t, producer.Namespace, producer.Name, "manager", "Draining Bitcoin receipt collectors", "\"pending\":1")
	eventuallyLive(t, "new producer after active action drain", func() bool { return replacementProducerReady(c, key.Namespace, producer.UID) })
	waitGeneration(t, c, a, func(a *actionv1.BitcoinBlockGeneration) bool {
		return a.Status.Phase == "Completed" && len(a.Finalizers) == 0
	})
	p := waitProduction(t, c, key, func(p *bitcoinv1.BitcoinProductionTarget) bool {
		return p.Status.Action == nil && p.Status.Phase == "Paused"
	})
	if a.Status.BlocksGenerated != 1 || a.Status.LastDispatchID != armed.Status.DispatchID || p.Status.BlocksProduced != 0 {
		t.Fatal("drain lost or duplicated action receipt")
	}
	t.Logf("active action receipt accounted once through SIGTERM drain: %s", a.Status.LastDispatchID)
}
