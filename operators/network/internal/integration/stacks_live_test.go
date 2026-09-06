//go:build live && stacksproduction

package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	stacksv1alpha1 "github.com/cylewitruk-stacks/stacks-k8s/apis/network/stacks/v1alpha1"
	networkv1alpha1 "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha1"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/transactions"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// stacksLiveClient connects only to the explicitly selected disposable network.
func stacksLiveClient(t *testing.T) (client.Client, types.NamespacedName) {
	t.Helper()
	scheme := runtime.NewScheme()
	liveMust(t, clientgoscheme.AddToScheme(scheme))
	liveMust(t, networkv1alpha1.AddToScheme(scheme))
	liveMust(t, stacksv1alpha1.AddToScheme(scheme))
	c, err := client.New(liveConfiguration(t), client.Options{Scheme: scheme})
	liveMust(t, err)
	return c, types.NamespacedName{Namespace: requiredEnvironment(t, "STACKS_TX_LIVE_NAMESPACE"), Name: requiredEnvironment(t, "STACKS_TX_LIVE_NETWORK")}
}

// waitTransfers waits for bounded durable transaction evidence.
func waitTransfers(t *testing.T, c client.Client, key types.NamespacedName, accept func(*stacksv1alpha1.StacksTransactionProduction) bool) *stacksv1alpha1.StacksTransactionProduction {
	t.Helper()
	var result *stacksv1alpha1.StacksTransactionProduction
	eventuallyLive(t, "transfer ledger", func() bool {
		p := &stacksv1alpha1.StacksTransactionProduction{}
		if c.Get(context.Background(), key, p) == nil && accept(p) {
			result = p
			return true
		}
		return false
	})
	return result
}

// stacksTip reads protocol progress through the explicitly forwarded ingress RPC.
func stacksTip(t *testing.T) int64 {
	t.Helper()
	c := &http.Client{Timeout: 5 * time.Second}
	response, err := c.Get(requiredEnvironment(t, "STACKS_TX_LIVE_RPC_URL") + "/v2/info")
	liveMust(t, err)
	defer response.Body.Close()
	var info struct {
		Height int64 `json:"stacks_tip_height"`
	}
	liveMust(t, json.NewDecoder(response.Body).Decode(&info))
	return info.Height
}

func TestLiveStacksTransfers(t *testing.T) {
	c, key := stacksLiveClient(t)
	ctx := context.Background()
	initial := waitTransfers(t, c, key, func(p *stacksv1alpha1.StacksTransactionProduction) bool { return p.Status.Confirmed >= 3 })
	initialTip := stacksTip(t)
	for count := initial.Status.Confirmed + 1; count <= initial.Status.Confirmed+4; count++ {
		p := waitTransfers(t, c, key, func(p *stacksv1alpha1.StacksTransactionProduction) bool {
			return p.Status.Confirmed >= count && !p.Status.Outstanding
		})
		evidence, err := transactions.NewNodeRPC().Inclusion(ctx, requiredEnvironment(t, "STACKS_TX_LIVE_RPC_URL"), p.Status.TxID)
		liveMust(t, err)
		if !evidence.Found || !evidence.Success || evidence.BlockID != p.Status.LastBlockID {
			t.Fatal("ledger inclusion was not corroborated by native Core")
		}
		t.Logf("confirmed=%d nonce=%d authorization-to-observation=%s block=%s", p.Status.Confirmed, p.Status.Nonce, p.Status.LastConfirmedAt.Sub(p.Status.SubmittedAt.Time), p.Status.LastBlockID)
	}
	if stacksTip(t) <= initialTip {
		t.Fatal("Stacks protocol tip did not advance")
	}
	updateNetwork(t, ctx, c, key, func(n *networkv1alpha1.StacksNetwork) { n.Spec.StacksTransactionProduction.Paused = true })
	paused := waitTransfers(t, c, key, func(p *stacksv1alpha1.StacksTransactionProduction) bool {
		return p.Status.Phase == "Paused" && !p.Status.Outstanding
	})
	time.Sleep(time.Duration(paused.Spec.Policy.IntervalSeconds+3) * time.Second)
	current := &stacksv1alpha1.StacksTransactionProduction{}
	liveMust(t, c.Get(ctx, key, current))
	if current.Status.Confirmed != paused.Status.Confirmed || current.Status.TxID != paused.Status.TxID {
		t.Fatal("pause authorized another transfer")
	}
	updateNetwork(t, ctx, c, key, func(n *networkv1alpha1.StacksNetwork) {
		n.Spec.StacksTransactionProduction.Paused = false
		n.Spec.StacksTransactionProduction.IntervalSeconds = 5
		n.Spec.StacksNodes = append(n.Spec.StacksNodes, networkv1alpha1.StacksNodeTemplate{Name: "unready", Role: "follower", BitcoinNodeRef: "bitcoin", Image: n.Spec.Defaults.BitcoinImage, Config: liveGeneratedStacks(), Container: &networkv1alpha1.ContainerOverride{Command: []string{"/deliberately-missing-test-binary"}}})
	})
	eventuallyLive(t, "unrelated failure with current target declarations", func() bool {
		n := &networkv1alpha1.StacksNetwork{}
		return c.Get(ctx, key, n) == nil && !n.Status.InventoryReady && n.Status.TargetDeclarations != nil && n.Status.TargetDeclarations.ObservedGeneration == n.Generation
	})
	waitTransfers(t, c, key, func(p *stacksv1alpha1.StacksTransactionProduction) bool {
		return p.Status.Confirmed >= paused.Status.Confirmed+3
	})
	pending := waitTransfers(t, c, key, func(p *stacksv1alpha1.StacksTransactionProduction) bool {
		return p.Status.Outstanding && p.Status.Accepted
	})
	pods := &corev1.PodList{}
	liveMust(t, c.List(ctx, pods, client.InNamespace(key.Namespace), client.MatchingLabels{"app.kubernetes.io/name": "stacks-transactions"}))
	if len(pods.Items) != 1 {
		t.Fatal("expected one transaction worker")
	}
	liveMust(t, c.Delete(ctx, &pods.Items[0]))
	waitTransfers(t, c, key, func(p *stacksv1alpha1.StacksTransactionProduction) bool {
		return p.Status.Confirmed >= pending.Status.Confirmed+2
	})
	evidence, err := transactions.NewNodeRPC().Inclusion(ctx, requiredEnvironment(t, "STACKS_TX_LIVE_RPC_URL"), pending.Status.TxID)
	liveMust(t, err)
	if !evidence.Found || !evidence.Success {
		t.Fatal("pending transaction was not preserved across worker replacement")
	}
	updateNetwork(t, ctx, c, key, func(n *networkv1alpha1.StacksNetwork) {
		n.Spec.StacksTransactionProduction.IntervalSeconds = 10
		nodes := n.Spec.StacksNodes[:0]
		for _, node := range n.Spec.StacksNodes {
			if node.Name != "unready" {
				nodes = append(nodes, node)
			}
		}
		n.Spec.StacksNodes = nodes
	})
	waitNetworkReady(t, ctx, c, key, 4)
	t.Log("exact transfers, observed Stacks progress, pause, cadence edit, independent admission and pending worker replacement passed")
}

// TestLiveStacksAbandonment disposes a dedicated environment with an unresolved transfer.
func TestLiveStacksAbandonment(t *testing.T) {
	c, key := stacksLiveClient(t)
	waitTransfers(t, c, key, func(p *stacksv1alpha1.StacksTransactionProduction) bool { return p.Status.Outstanding })
	parent := &networkv1alpha1.StacksNetwork{}
	liveMust(t, c.Get(context.Background(), key, parent))
	liveMust(t, c.Delete(context.Background(), parent))
	eventuallyLive(t, "transaction finalizer removal through actual chart RBAC", func() bool {
		return apierrors.IsNotFound(c.Get(context.Background(), key, &stacksv1alpha1.StacksTransactionProduction{}))
	})
}
