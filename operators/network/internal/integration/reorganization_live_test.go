//go:build live && bitcoinproduction

package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"

	actionv1 "github.com/cylewitruk-stacks/stacks-k8s/apis/network/actions/v1alpha1"
	bitcoinv1 "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha1"
	networkv1 "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// createReorganization waits for actual quota admission before creating one bounded request.
func createReorganization(t *testing.T, c client.Client, key types.NamespacedName, name string, depth int32, timeout time.Duration) *actionv1.BitcoinReorganization {
	t.Helper()
	waitActionQuota(t, c, key.Namespace, "bitcoinreorganizations")
	a := &actionv1.BitcoinReorganization{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: key.Namespace}, Spec: actionv1.BitcoinReorganizationSpec{NetworkRef: actionv1.LocalReference{Name: key.Name}, BitcoinNodeRef: actionv1.LocalReference{Name: key.Name + "-bitcoin"}, Depth: depth, Address: "mipcBbFg9gMiCh81Kj8tqqdgoZub1ZJRfn", Timeout: metav1.Duration{Duration: timeout}, BoundaryPolicy: actionv1.ReorganizationBoundaryPolicy{AllowEpochBoundaryCrossing: true, AllowRewardCycleBoundaryCrossing: true, AllowPreparePhaseBoundaryCrossing: true}}}
	liveMust(t, c.Create(context.Background(), a))
	return a
}

// waitReorganization observes this action's exact status rather than inferring from chain height.
func waitReorganization(t *testing.T, c client.Client, a *actionv1.BitcoinReorganization, accept func(*actionv1.BitcoinReorganization) bool) {
	t.Helper()
	eventuallyLive(t, "reorganization lifecycle", func() bool { return c.Get(context.Background(), client.ObjectKeyFromObject(a), a) == nil && accept(a) })
}

// observeBitcoin executes only fixed test reads with observer credentials delivered over stdin.
func observeBitcoin(t *testing.T, c client.Client, key types.NamespacedName, method string, args ...string) []byte {
	t.Helper()
	switch method {
	case "getblockhash", "getblockheader", "getchaintips", "getblockchaininfo":
	default:
		t.Fatal("unsupported observer method")
	}
	secret := &corev1.Secret{}
	liveMust(t, c.Get(context.Background(), client.ObjectKey{Namespace: key.Namespace, Name: "stacks-bitcoin-observer-rpc"}, secret))
	var credential struct{ Username, Password string }
	liveMust(t, json.Unmarshal(secret.Data["credentials.json"], &credential))
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	command := []string{"--kubeconfig", requiredEnvironment(t, "STACKS_NETWORK_LIVE_KUBECONFIG"), "--context", requiredEnvironment(t, "STACKS_NETWORK_LIVE_CONTEXT"), "-n", key.Namespace, "exec", "-i", key.Name + "-bitcoin-0", "-c", "actor", "--", "bitcoin-cli", "-regtest", "-rpcport=18443", "-rpcuser=" + credential.Username, "-stdinrpcpass", method}
	cmd := exec.CommandContext(ctx, "kubectl", append(command, args...)...)
	cmd.Stdin = strings.NewReader(credential.Password + "\n")
	output, err := cmd.Output()
	liveMust(t, err)
	if len(output) > 65536 {
		t.Fatal("observer response oversized")
	}
	return output
}

// pausedReorgEnvironment establishes an original suffix using only normal baseline production.
func pausedReorgEnvironment(t *testing.T) (client.Client, types.NamespacedName) {
	t.Helper()
	c, key := bitcoinLiveClient(t)

	updateNetwork(t, context.Background(), c, key, func(n *networkv1.StacksNetwork) { n.Spec.BitcoinBlockProduction.Paused = false })
	waitNetworkReady(t, context.Background(), c, key, 1)
	eventuallyLive(t, "current canonical chain has an original suffix", func() bool {
		var chain struct {
			Blocks int64 `json:"blocks"`
		}
		liveMust(t, json.Unmarshal(observeBitcoin(t, c, key, "getblockchaininfo"), &chain))
		return chain.Blocks >= 8
	})

	updateNetwork(t, context.Background(), c, key, func(n *networkv1.StacksNetwork) { n.Spec.BitcoinBlockProduction.Paused = true })
	waitProduction(t, c, key, func(p *bitcoinv1.BitcoinBlockProduction) bool {
		return p.Status.Phase == "Paused" && p.Status.DispatchState == "Idle"
	})
	waitNetworkReady(t, context.Background(), c, key, 1)
	return c, key
}

func TestLiveReorganizationAndCompensatedDeadline(t *testing.T) {
	c, key := pausedReorgEnvironment(t)
	a := createReorganization(t, c, key, "replace-two", 2, time.Minute)
	waitReorganization(t, c, a, func(a *actionv1.BitcoinReorganization) bool {
		return a.Status.Phase == "Completed" && len(a.Finalizers) == 0
	})
	if !a.Status.InvalidationAcknowledged || !a.Status.CleanupAcknowledged || a.Status.BlocksGenerated != 3 || a.Status.FinalChain == nil || a.Status.FinalChain.Chainwork <= a.Status.OriginalChain.Chainwork {
		t.Fatal("incomplete reorganization evidence")
	}
	canonical := strings.TrimSpace(string(observeBitcoin(t, c, key, "getblockhash", fmt.Sprint(a.Status.ForkParent.Height+3))))
	if canonical != a.Status.LastBlockHash {
		t.Fatal("acknowledged branch is not canonical")
	}
	var tips []struct{ Hash, Status string }
	liveMust(t, json.Unmarshal(observeBitcoin(t, c, key, "getchaintips"), &tips))
	originalValid := false
	for _, tip := range tips {
		if tip.Hash == a.Status.OriginalChain.Hash && tip.Status == "valid-fork" {
			originalValid = true
		}
	}
	if !originalValid {
		t.Fatal("original branch did not become a valid fork after cleanup")
	}
	t.Logf("replacement verified: original=%s replacement=%s work=%s old branch=valid-fork", a.Status.OriginalChain.Hash, canonical, a.Status.FinalChain.Chainwork)
	b := createReorganization(t, c, key, "short-deadline", 6, 5*time.Second)
	waitReorganization(t, c, b, func(a *actionv1.BitcoinReorganization) bool {
		return a.Status.Phase == "Failed" && len(a.Finalizers) == 0
	})
	if !b.Status.InvalidationAcknowledged || !b.Status.CleanupAcknowledged || b.Status.BlocksGenerated >= 6 {
		t.Fatal("deadline did not interrupt and compensate bounded work")
	}
	restored := strings.TrimSpace(string(observeBitcoin(t, c, key, "getblockhash", fmt.Sprint(b.Status.OriginalChain.Height))))
	if restored != b.Status.OriginalChain.Hash {
		t.Fatal("higher-work original branch was not eligible after compensation")
	}
	waitProduction(t, c, key, func(p *bitcoinv1.BitcoinBlockProduction) bool {
		return p.Status.Reorganization == nil && p.Status.Phase == "Paused"
	})
	t.Logf("deadline compensated after %d acknowledged replacement blocks", b.Status.BlocksGenerated)
}

// TestLiveReorganizationLostReceipt uses a fresh baseline-capable fixture for the selected method.
func TestLiveReorganizationLostReceipt(t *testing.T) {
	c, key := pausedReorgEnvironment(t)
	ctx := context.Background()
	method := requiredEnvironment(t, "STACKS_BITCOIN_REORG_FAULT")
	switch method {
	case "invalidateblock", "generation", "reconsiderblock":
	default:
		t.Fatal("unsupported fixture method")
	}
	a := createReorganization(t, c, key, "lost-receipt", 2, time.Minute)
	waitPodLog(t, key.Namespace, key.Name+"-bitcoin-0", "actor", "Bitcoin returned "+method+" receipt", "dropping response connection")
	waitReorganization(t, c, a, func(a *actionv1.BitcoinReorganization) bool { return a.Status.Phase == "Inconclusive" })
	p := waitProduction(t, c, key, func(p *bitcoinv1.BitcoinBlockProduction) bool {
		return p.Status.Phase == "Blocked" && p.Status.DispatchState == "Armed"
	})
	dispatch := p.Status.DispatchID
	blocks := a.Status.BlocksGenerated
	// Independent read evidence never authorizes compensating an unknown call.
	var tips []struct{ Hash, Status string }
	liveMust(t, json.Unmarshal(observeBitcoin(t, c, key, "getchaintips"), &tips))
	originalStatus := ""
	for _, tip := range tips {
		if tip.Hash == a.Status.OriginalChain.Hash {
			originalStatus = tip.Status
		}
	}
	expected := "invalid"
	if method == "reconsiderblock" {
		expected = "valid-fork"
	}
	if originalStatus != expected {
		t.Fatalf("actual original branch status %s, want %s", originalStatus, expected)
	}
	liveMust(t, c.Delete(ctx, a))
	producer := liveProducerPod(t, c, key.Namespace)
	liveMust(t, c.Delete(ctx, producer))
	eventuallyLive(t, "replacement executor with retained reorganization", func() bool { return replacementProducerReady(c, key.Namespace, producer.UID) })
	time.Sleep(3 * time.Second)
	liveMust(t, c.Get(ctx, key, p))
	liveMust(t, c.Get(ctx, client.ObjectKeyFromObject(a), a))
	if p.Status.DispatchID != dispatch || p.Status.DispatchState != "Armed" || p.Status.Reorganization == nil || a.Status.BlocksGenerated != blocks || len(a.Finalizers) == 0 {
		t.Fatal("unknown call resumed or released during restart/deletion")
	}
	n := &networkv1.StacksNetwork{}
	liveMust(t, c.Get(ctx, key, n))
	liveMust(t, c.Delete(ctx, n))
	eventuallyLive(t, "administrative abandonment removes action and ledger", func() bool {
		return apierrors.IsNotFound(c.Get(ctx, key, p)) && apierrors.IsNotFound(c.Get(ctx, client.ObjectKeyFromObject(a), a))
	})
	t.Logf("%s receipt loss: original=%s, acknowledged replacements=%d, retained dispatch=%s; teardown passed", method, originalStatus, blocks, dispatch)
}
