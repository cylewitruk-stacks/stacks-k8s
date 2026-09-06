//go:build live && bitcoinproduction

package integration

import (
	"context"
	"strings"
	"testing"
	"time"

	actionv1 "github.com/cylewitruk-stacks/stacks-k8s/apis/network/actions/v1alpha1"
	bitcoinv1alpha1 "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha1"
	networkv1alpha1 "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestLiveBitcoinBaseline(t *testing.T) {
	c, key := bitcoinLiveClient(t)
	ctx := context.Background()
	initial := waitProduction(t, c, key, func(p *bitcoinv1alpha1.BitcoinBlockProduction) bool {
		return p.Status.BlocksProduced >= 2 && p.Status.DispatchState == "Idle"
	})
	t.Logf("automatic production: %d acknowledged blocks", initial.Status.BlocksProduced)
	updateNetwork(t, ctx, c, key, func(n *networkv1alpha1.StacksNetwork) { n.Spec.BitcoinBlockProduction.Paused = true })
	paused := waitProduction(t, c, key, func(p *bitcoinv1alpha1.BitcoinBlockProduction) bool {
		return p.Status.Phase == "Paused" && p.Status.DispatchState == "Idle"
	})
	time.Sleep(6 * time.Second)
	current := &bitcoinv1alpha1.BitcoinBlockProduction{}
	liveMust(t, c.Get(ctx, key, current))
	if current.Status.BlocksProduced != paused.Status.BlocksProduced {
		t.Fatal("blocks were dispatched while paused")
	}
	updateNetwork(t, ctx, c, key, func(n *networkv1alpha1.StacksNetwork) {
		n.Spec.BitcoinBlockProduction.Paused = false
		n.Spec.BitcoinBlockProduction.IntervalSeconds = 2
		n.Spec.StacksNodes = []networkv1alpha1.StacksNodeTemplate{{Name: "unready", Role: "follower", Image: n.Spec.Defaults.BitcoinImage, BitcoinNodeRef: "bitcoin", Config: liveGeneratedStacks(), Container: &networkv1alpha1.ContainerOverride{Command: []string{"/deliberately-missing-test-binary"}}}}
	})
	eventuallyLive(t, "unready Stacks actor with current declaration catalog", func() bool {
		n := &networkv1alpha1.StacksNetwork{}
		return c.Get(ctx, key, n) == nil && !n.Status.InventoryReady && n.Status.TargetDeclarations != nil && n.Status.TargetDeclarations.ObservedGeneration == n.Generation && len(n.Status.TargetDeclarations.Actors) == 2
	})
	afterFailure := waitProduction(t, c, key, func(p *bitcoinv1alpha1.BitcoinBlockProduction) bool {
		return p.Spec.Policy.IntervalSeconds == 2 && p.Status.BlocksProduced >= paused.Status.BlocksProduced+3
	})
	t.Logf("production continued with an unrelated unready Stacks node: %d -> %d", paused.Status.BlocksProduced, afterFailure.Status.BlocksProduced)
	deployments := &appsv1.DeploymentList{}
	liveMust(t, c.List(ctx, deployments, client.InNamespace(key.Namespace), client.MatchingLabels{}))
	var producer *appsv1.Deployment
	for i := range deployments.Items {
		d := &deployments.Items[i]
		if d.Spec.Template.Labels["app.kubernetes.io/name"] == "bitcoin-production" {
			producer = d
		}
	}
	if producer == nil {
		t.Fatal("production Deployment is absent")
	}
	base := producer.DeepCopy()
	if producer.Spec.Template.Annotations == nil {
		producer.Spec.Template.Annotations = map[string]string{}
	}
	producer.Spec.Template.Annotations["test.stacks.org/restart"] = time.Now().UTC().Format(time.RFC3339Nano)
	liveMust(t, c.Patch(ctx, producer, client.MergeFrom(base)))
	waitProduction(t, c, key, func(p *bitcoinv1alpha1.BitcoinBlockProduction) bool {
		return p.Status.BlocksProduced >= afterFailure.Status.BlocksProduced+3
	})
	eventuallyLive(t, "producer rollout completes", func() bool {
		d := &appsv1.Deployment{}
		return c.Get(ctx, client.ObjectKeyFromObject(producer), d) == nil && d.Status.ObservedGeneration == d.Generation && d.Status.UpdatedReplicas == 1 && d.Status.AvailableReplicas == 1
	})
	updateNetwork(t, ctx, c, key, func(n *networkv1alpha1.StacksNetwork) {
		n.Spec.StacksNodes = nil
		n.Spec.BitcoinBlockProduction.IntervalSeconds = 5
	})
	waitNetworkReady(t, ctx, c, key, 1)
	t.Log("pause, cadence update, independent target admission and producer rollout passed")
}

func TestLiveBitcoinAmbiguousReceipt(t *testing.T) {
	c, key := bitcoinLiveClient(t)
	waitBitcoinFixtureLog(t, key, "dropping response connection")
	blocked := waitProduction(t, c, key, func(p *bitcoinv1alpha1.BitcoinBlockProduction) bool {
		return p.Status.Phase == "Blocked" && p.Status.DispatchState == "Armed"
	})
	time.Sleep(12 * time.Second)
	current := &bitcoinv1alpha1.BitcoinBlockProduction{}
	liveMust(t, c.Get(context.Background(), key, current))
	if current.Status.DispatchID != blocked.Status.DispatchID || current.Status.BlocksProduced != blocked.Status.BlocksProduced || current.Status.DispatchState != "Armed" {
		t.Fatal("ambiguous request was retried or silently accounted")
	}
	producer := liveProducerPod(t, c, key.Namespace)
	liveMust(t, c.Delete(context.Background(), producer))
	eventuallyLive(t, "replacement producer ready", func() bool {
		return replacementProducerReady(c, key.Namespace, producer.UID)
	})
	time.Sleep(5 * time.Second)
	liveMust(t, c.Get(context.Background(), key, current))
	if current.Status.DispatchID != blocked.Status.DispatchID || current.Status.DispatchState != "Armed" || current.Status.Phase != "Blocked" || current.Status.BlocksProduced != 0 {
		t.Fatal("replacement producer reopened ambiguous dispatch")
	}
	t.Logf("unresolved dispatch remains closed: %s", blocked.Status.DispatchID)
}

// TestLiveBitcoinDelayedReceipt requires a fresh, initially paused delay-fixture network.
func TestLiveBitcoinDelayedReceipt(t *testing.T) { qualifyDelayedReceipt(t, false) }

// TestLiveBitcoinInFlightShutdown terminates the producer only after Core has returned its delayed receipt.
func TestLiveBitcoinInFlightShutdown(t *testing.T) { qualifyDelayedReceipt(t, true) }

// qualifyDelayedReceipt preserves the same Armed dispatch across delay and optional SIGTERM draining.
func qualifyDelayedReceipt(t *testing.T, shutdown bool) {
	c, key := bitcoinLiveClient(t)
	initial := waitProduction(t, c, key, func(p *bitcoinv1alpha1.BitcoinBlockProduction) bool { return p.Status.Phase == "Paused" })
	if initial.Status.BlocksProduced != 0 || initial.Status.DispatchID != "" {
		t.Fatal("delay qualification requires a fresh unused ledger")
	}
	producer := liveProducerPod(t, c, key.Namespace)
	updateNetwork(t, context.Background(), c, key, func(n *networkv1alpha1.StacksNetwork) { n.Spec.BitcoinBlockProduction.Paused = false })
	waitBitcoinFixtureLog(t, key, "delaying delivery for 15 seconds")
	armed := &bitcoinv1alpha1.BitcoinBlockProduction{}
	liveMust(t, c.Get(context.Background(), key, armed))
	if armed.Status.DispatchState != "Armed" || armed.Status.BlocksProduced != 0 {
		t.Fatal("receipt was not in flight at the qualification boundary")
	}
	updateNetwork(t, context.Background(), c, key, func(n *networkv1alpha1.StacksNetwork) { n.Spec.BitcoinBlockProduction.Paused = true })
	if shutdown {
		liveMust(t, c.Delete(context.Background(), producer))
		// The actual manager drain must have observed this outstanding request.
		waitPodLog(t, producer.Namespace, producer.Name, "manager", "Draining Bitcoin receipt collectors", "\"pending\":1")
	}
	completed := waitProduction(t, c, key, func(p *bitcoinv1alpha1.BitcoinBlockProduction) bool {
		return p.Status.DispatchState == "Idle" && p.Status.BlocksProduced == 1
	})
	if completed.Status.DispatchID != armed.Status.DispatchID || completed.Status.LastBlockHash == "" {
		t.Fatal("delayed dispatch was replaced rather than accounted")
	}
	if shutdown {
		eventuallyLive(t, "replacement producer ready after drain", func() bool { return replacementProducerReady(c, key.Namespace, producer.UID) })
	}
	paused := waitProduction(t, c, key, func(p *bitcoinv1alpha1.BitcoinBlockProduction) bool { return p.Status.Phase == "Paused" })
	if paused.Status.BlocksProduced != 1 || paused.Status.DispatchID != armed.Status.DispatchID {
		t.Fatal("pause allowed a second dispatch")
	}
	t.Logf("15-second delayed receipt accounted once, in-flight shutdown=%t, dispatch=%s", shutdown, armed.Status.DispatchID)
}

// liveProducerPod returns the ready producer in this dedicated namespace.
func liveProducerPod(t *testing.T, c client.Client, namespace string) *corev1.Pod {
	t.Helper()
	pods := &corev1.PodList{}
	liveMust(t, c.List(context.Background(), pods, client.InNamespace(namespace), client.MatchingLabels{"app.kubernetes.io/name": "bitcoin-production"}))
	for i := range pods.Items {
		if readyProducer(&pods.Items[i]) {
			return pods.Items[i].DeepCopy()
		}
	}
	t.Fatal("ready producer Pod is absent")
	return nil
}

// readyProducer checks the Pod's own readiness and deletion boundary.
func readyProducer(p *corev1.Pod) bool {
	for _, condition := range p.Status.Conditions {
		if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue && p.DeletionTimestamp.IsZero() {
			return true
		}
	}
	return false
}

// replacementProducerReady observes a different ready producer UID after deletion.
func replacementProducerReady(c client.Client, namespace string, old types.UID) bool {
	pods := &corev1.PodList{}
	if c.List(context.Background(), pods, client.InNamespace(namespace), client.MatchingLabels{"app.kubernetes.io/name": "bitcoin-production"}) != nil {
		return false
	}
	for i := range pods.Items {
		if pods.Items[i].UID != old && readyProducer(&pods.Items[i]) {
			return true
		}
	}
	return false
}

// waitBitcoinFixtureLog establishes that the real server already returned the selected receipt.
func waitBitcoinFixtureLog(t *testing.T, key types.NamespacedName, message string) {
	t.Helper()
	waitPodLog(t, key.Namespace, key.Name+"-bitcoin-0", "actor", "Bitcoin returned generation receipt", message)
}

// waitPodLog reads credential-free fixture or manager markers from the selected Pod.
func waitPodLog(t *testing.T, namespace, name, container string, messages ...string) {
	t.Helper()
	c, err := kubernetes.NewForConfig(liveConfiguration(t))
	liveMust(t, err)
	eventuallyLive(t, "Pod log evidence: "+strings.Join(messages, ", "), func() bool {
		body, err := c.CoreV1().Pods(namespace).GetLogs(name, &corev1.PodLogOptions{Container: container}).DoRaw(context.Background())
		if err != nil {
			return false
		}
		for _, message := range messages {
			if !strings.Contains(string(body), message) {
				return false
			}
		}
		return true
	})
}

// TestLiveBitcoinAbandonment removes a dedicated ambiguous environment's owning network.
func TestLiveBitcoinAbandonment(t *testing.T) {
	c, key := bitcoinLiveClient(t)
	waitProduction(t, c, key, func(p *bitcoinv1alpha1.BitcoinBlockProduction) bool {
		return p.Status.DispatchState == "Armed" && p.Status.Phase == "Blocked"
	})
	network := &networkv1alpha1.StacksNetwork{}
	liveMust(t, c.Get(context.Background(), key, network))
	liveMust(t, c.Delete(context.Background(), network))
	eventuallyLive(t, "unresolved ledger finalizer released during environment teardown", func() bool {
		return apierrors.IsNotFound(c.Get(context.Background(), key, &bitcoinv1alpha1.BitcoinBlockProduction{}))
	})
	t.Log("unresolved production ledger removed after owning network deletion")
}

// bitcoinLiveClient connects only to the explicitly selected disposable environment.
func bitcoinLiveClient(t *testing.T) (client.Client, types.NamespacedName) {
	t.Helper()
	scheme := runtime.NewScheme()
	liveMust(t, clientgoscheme.AddToScheme(scheme))
	liveMust(t, networkv1alpha1.AddToScheme(scheme))
	liveMust(t, bitcoinv1alpha1.AddToScheme(scheme))
	liveMust(t, actionv1.AddToScheme(scheme))
	c, err := client.New(liveConfiguration(t), client.Options{Scheme: scheme})
	liveMust(t, err)
	return c, types.NamespacedName{Namespace: requiredEnvironment(t, "STACKS_BITCOIN_LIVE_NAMESPACE"), Name: requiredEnvironment(t, "STACKS_BITCOIN_LIVE_NETWORK")}
}

// waitProduction waits for a specific durable production state.
func waitProduction(t *testing.T, c client.Client, key types.NamespacedName, accept func(*bitcoinv1alpha1.BitcoinBlockProduction) bool) *bitcoinv1alpha1.BitcoinBlockProduction {
	t.Helper()
	var result *bitcoinv1alpha1.BitcoinBlockProduction
	eventuallyLive(t, "production state", func() bool {
		current := &bitcoinv1alpha1.BitcoinBlockProduction{}
		if c.Get(context.Background(), key, current) == nil && accept(current) {
			result = current
			return true
		}
		return false
	})
	return result
}
