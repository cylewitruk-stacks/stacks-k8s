//go:build live

package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	admissionv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// faultFixture confines live qualification to an explicitly selected disposable namespace.
type faultFixture struct {
	t                                           *testing.T
	ctx                                         context.Context
	admin                                       client.Client
	kubeconfig, kubecontext, namespace, network string
	username, password                          string
	pods                                        map[string]*corev1.Pod
	// onWaitTimeout collects optional best-effort evidence before a failed wait ends the test.
	onWaitTimeout func(string)
}

// newFaultFixture checks opt-in, empty fault inventory, enrollment, and current CEL compilation.
func newFaultFixture(t *testing.T, namespace, network string, enrolled bool) *faultFixture {
	t.Helper()
	if os.Getenv("STACKS_CHAOS_PARTITION_LIVE") != "1" {
		t.Skip("set STACKS_CHAOS_PARTITION_LIVE=1 for disposable partition fixtures")
	}
	f := &faultFixture{t: t, namespace: namespace, network: network, kubeconfig: os.Getenv("STACKS_CHAOS_KUBECONFIG"), kubecontext: os.Getenv("STACKS_CHAOS_CONTEXT"), pods: map[string]*corev1.Pod{}}
	if f.kubeconfig == "" || f.kubecontext == "" {
		t.Fatal("explicit kubeconfig and context required")
	}
	config, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(&clientcmd.ClientConfigLoadingRules{ExplicitPath: f.kubeconfig}, &clientcmd.ConfigOverrides{CurrentContext: f.kubecontext}).ClientConfig()
	if err != nil {
		t.Fatal(err)
	}
	config.Timeout = 10 * time.Second
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	f.admin, err = client.New(config, client.Options{Scheme: scheme})
	if err != nil {
		t.Fatal(err)
	}
	var cancel context.CancelFunc
	f.ctx, cancel = context.WithTimeout(context.Background(), 8*time.Minute)
	t.Cleanup(cancel)
	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(schema.GroupVersionKind{Group: "chaos-mesh.org", Version: "v1alpha1", Kind: "NetworkChaosList"})
	if err := f.admin.List(f.ctx, list, client.InNamespace(namespace)); err != nil {
		t.Fatal(err)
	}
	if len(list.Items) != 0 {
		t.Fatal("qualification requires an empty fault namespace")
	}
	ns := &corev1.Namespace{}
	if err := f.admin.Get(f.ctx, client.ObjectKey{Name: namespace}, ns); err != nil {
		t.Fatal(err)
	}
	if ns.Annotations["chaos-mesh.org/inject"] != "enabled" {
		t.Fatal("native namespace injection must be explicitly enabled")
	}
	if enrolled {
		if ns.Labels["network.stacks.org/chaos-profile"] != "network-faults-v1" {
			t.Fatal("fixture lacks bounded profile enrollment")
		}
		f.wait("current CEL compilation", 30*time.Second, func() bool {
			policy := &admissionv1.ValidatingAdmissionPolicy{}
			if err := f.admin.Get(f.ctx, client.ObjectKey{Name: "stacks-network-faults-" + namespace}, policy); err != nil {
				t.Fatal(err)
			}
			if policy.Status.ObservedGeneration != policy.Generation || policy.Status.TypeChecking == nil {
				return false
			}
			if len(policy.Status.TypeChecking.ExpressionWarnings) != 0 {
				t.Fatalf("CEL warnings: %+v", policy.Status.TypeChecking.ExpressionWarnings)
			}
			return true
		})
	} else if ns.Labels["network.stacks.org/chaos-profile"] != "" {
		t.Fatal("administrative control-loss fixture must not be agent-enrolled")
	}
	secret := &corev1.Secret{}
	if err := f.admin.Get(f.ctx, client.ObjectKey{Namespace: namespace, Name: "stacks-bitcoin-observer-rpc"}, secret); err != nil {
		t.Fatal(err)
	}
	var credentials struct{ Username, Password string }
	if json.Unmarshal(secret.Data["credentials.json"], &credentials) != nil || credentials.Username == "" || credentials.Password == "" {
		t.Fatal("invalid observer credentials")
	}
	f.username, f.password = credentials.Username, credentials.Password
	t.Logf("fixture context=%s namespace=%s namespaceUID=%s", f.kubecontext, namespace, ns.UID)
	return f
}

// wait bounds a level-based observation without treating a transient missing condition as success.
func (f *faultFixture) wait(description string, bound time.Duration, condition func() bool) {
	f.t.Helper()
	deadline := time.Now().Add(bound)
	for !condition() {
		if time.Now().After(deadline) || f.ctx.Err() != nil {
			if f.onWaitTimeout != nil {
				f.onWaitTimeout(description)
			}
			f.t.Fatal("timed out: " + description)
		}
		select {
		case <-f.ctx.Done():
			if f.onWaitTimeout != nil {
				f.onWaitTimeout(description)
			}
			f.t.Fatal(f.ctx.Err())
		case <-time.After(300 * time.Millisecond):
		}
	}
}

// object reads a current resource without importing an operator runtime or API module.
func (f *faultFixture) object(group, kind, name string) *unstructured.Unstructured {
	f.t.Helper()
	o := &unstructured.Unstructured{}
	o.SetGroupVersionKind(schema.GroupVersionKind{Group: group, Version: "v1alpha1", Kind: kind})
	if err := f.admin.Get(f.ctx, client.ObjectKey{Namespace: f.namespace, Name: name}, o); err != nil {
		f.t.Fatal(err)
	}
	return o
}

// pod captures the exact actor incarnation used by the experiment.
func (f *faultFixture) pod(actor string) *corev1.Pod {
	f.t.Helper()
	pod := &corev1.Pod{}
	if err := f.admin.Get(f.ctx, client.ObjectKey{Namespace: f.namespace, Name: f.network + "-" + actor + "-0"}, pod); err != nil {
		f.t.Fatal(err)
	}
	if pod.Status.PodIP == "" {
		f.t.Fatal("actor has no Pod IP")
	}
	if old := f.pods[actor]; old != nil && (old.UID != pod.UID || old.Status.PodIP != pod.Status.PodIP) {
		f.t.Fatal("actor incarnation changed during qualification")
	}
	if old := f.pods[actor]; old != nil {
		if len(old.Status.ContainerStatuses) != len(pod.Status.ContainerStatuses) {
			f.t.Fatal("actor container inventory changed")
		}
		for i, before := range old.Status.ContainerStatuses {
			after := pod.Status.ContainerStatuses[i]
			if before.Name != after.Name || before.ContainerID != after.ContainerID || before.RestartCount != after.RestartCount {
				f.t.Fatal("actor process changed during qualification")
			}
		}
	}
	if f.pods[actor] == nil {
		f.t.Logf("actor=%s PodUID=%s PodIP=%s", actor, pod.UID, pod.Status.PodIP)
		f.pods[actor] = pod.DeepCopy()
	}
	return pod
}

// command uses the explicitly selected cluster and never includes credentials in argv.
func (f *faultFixture) command(ctx context.Context, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, "kubectl", append([]string{"--kubeconfig", f.kubeconfig, "--context", f.kubecontext, "-n", f.namespace}, args...)...)
}

// rpc sends observer credentials on stdin; its timeout is solely a read-only probe bound.
func (f *faultFixture) rpc(actor, address, port, method string, result any) error {
	return f.observerRPC(f.ctx, f.pod(actor).Name, address, port, method, result)
}

// observerRPC shares bounded, stdin-only observer authentication with diagnostic collection.
func (f *faultFixture) observerRPC(parent context.Context, pod, address, port, method string, result any) error {
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	command := f.command(ctx, "exec", "-i", pod, "--", "bitcoin-cli", "-regtest", "-rpcconnect="+address, "-rpcport="+port, "-rpcclienttimeout=2", "-rpcuser="+f.username, "-stdinrpcpass", method)
	command.Stdin = strings.NewReader(f.password + "\n")
	output, err := command.Output()
	if err != nil {
		return fmt.Errorf("observer probe failed: %w", err)
	}
	return json.Unmarshal(output, result)
}

// chain observes a node's local best chain through its read-only RPC principal.
func (f *faultFixture) chain(actor string) (int64, string) {
	f.t.Helper()
	var info struct {
		Blocks        int64
		BestBlockHash string
	}
	if err := f.rpc(actor, "127.0.0.1", "18443", "getblockchaininfo", &info); err != nil {
		f.t.Fatal(err)
	}
	return info.Blocks, info.BestBlockHash
}

// count reads an accounted production fact, treating an omitted initial count as zero.
func (f *faultFixture) count(group, kind, name, field string) int64 {
	o := f.object(group, kind, name)
	n, _, err := unstructured.NestedInt64(o.Object, "status", field)
	if err != nil {
		f.t.Fatal(err)
	}
	return n
}

// receipts reads the per-target Bitcoin receipt ledger.
func (f *faultFixture) receipts(actor string) int64 {
	return f.count("bitcoin.stacks.org", "BitcoinProductionTarget", f.network+"-"+actor, "blocksProduced")
}

// pause changes desired Bitcoin production through the aggregate and waits for idle ledgers.
func (f *faultFixture) pause(paused bool, actors ...string) {
	f.t.Helper()
	f.wait("production policy update", 10*time.Second, func() bool {
		o := f.object("network.stacks.org", "StacksNetwork", f.network)
		base := o.DeepCopy()
		if err := unstructured.SetNestedField(o.Object, paused, "spec", "bitcoinBlockProduction", "paused"); err != nil {
			f.t.Fatal(err)
		}
		err := f.admin.Patch(f.ctx, o, client.MergeFromWithOptions(base, client.MergeFromWithOptimisticLock{}))
		if apierrors.IsConflict(err) {
			return false
		}
		if err != nil {
			f.t.Fatal(err)
		}
		return true
	})
	if paused {
		f.wait("paused idle target ledgers", 20*time.Second, func() bool {
			for _, actor := range actors {
				o := f.object("bitcoin.stacks.org", "BitcoinProductionTarget", f.network+"-"+actor)
				phase, _, _ := unstructured.NestedString(o.Object, "status", "phase")
				if phase != "Paused" {
					return false
				}
			}
			return true
		})
	}
}

// partition constructs one native actor-to-actor fault within the public profile.
func (f *faultFixture) partition(name, source, target, duration string) *unstructured.Unstructured {
	selector := func(actor string) map[string]any {
		return map[string]any{"namespaces": []any{f.namespace}, "labelSelectors": map[string]any{"network.stacks.org/network": f.network, "network.stacks.org/actor": actor}}
	}
	o := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "chaos-mesh.org/v1alpha1", "kind": "NetworkChaos", "spec": map[string]any{"action": "partition", "mode": "one", "direction": "both", "duration": duration, "selector": selector(source), "target": map[string]any{"mode": "one", "selector": selector(target)}}}}
	o.SetNamespace(f.namespace)
	o.SetName(name)
	o.SetLabels(map[string]string{"network.stacks.org/network": f.network, "actions.stacks.org/correlation-id": name})
	return o
}

// inject creates a fault and registers bounded best-effort native cleanup even on assertion failure.
func (f *faultFixture) inject(o *unstructured.Unstructured) {
	f.t.Helper()
	if err := f.admin.Create(f.ctx, o); err != nil {
		f.t.Fatal(err)
	}
	f.t.Logf("fault=%s UID=%s", o.GetName(), o.GetUID())
	f.t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := f.admin.Delete(ctx, o); err != nil && !apierrors.IsNotFound(err) {
			f.t.Error(err)
			return
		}
		for {
			current := o.DeepCopy()
			err := f.admin.Get(ctx, client.ObjectKeyFromObject(o), current)
			if apierrors.IsNotFound(err) {
				return
			}
			if err != nil || ctx.Err() != nil {
				f.t.Errorf("native cleanup not confirmed for %s: %v", o.GetName(), err)
				return
			}
			time.Sleep(300 * time.Millisecond)
		}
	})
	f.wait("native injection", 20*time.Second, func() bool { return f.condition(o.GetName(), "AllInjected") })
}

// condition observes a native controller condition; it is not protocol recovery evidence.
func (f *faultFixture) condition(name, kind string) bool {
	o := f.object("chaos-mesh.org", "NetworkChaos", name)
	conditions, _, _ := unstructured.NestedSlice(o.Object, "status", "conditions")
	for _, value := range conditions {
		c := value.(map[string]any)
		if c["type"] == kind && c["status"] == "True" {
			return true
		}
	}
	return false
}

// recover waits for expiry when requested and for native finalized deletion in both modes.
func (f *faultFixture) recover(o *unstructured.Unstructured, expiry bool) {
	f.t.Helper()
	if expiry {
		f.wait("duration recovery", 100*time.Second, func() bool { return f.condition(o.GetName(), "AllRecovered") })
	}
	if err := f.admin.Delete(f.ctx, o); err != nil {
		f.t.Fatal(err)
	}
	f.wait("native finalized deletion", 30*time.Second, func() bool {
		return apierrors.IsNotFound(f.admin.Get(f.ctx, client.ObjectKeyFromObject(o), o.DeepCopy()))
	})
}

// checkPeerRPC tests round-trip reachability from both actors, not independent packet direction.
func (f *faultFixture) checkPeerRPC(reachable bool) {
	f.t.Helper()
	for _, pair := range [][2]string{{"bitcoin", "bitcoin-2"}, {"bitcoin-2", "bitcoin"}} {
		var height int64
		err := f.rpc(pair[0], f.pod(pair[1]).Status.PodIP, "18443", "getblockcount", &height)
		if reachable && err != nil || !reachable && err == nil {
			f.t.Fatalf("RPC %s -> %s expected reachable=%v: %v", pair[0], pair[1], reachable, err)
		}
		f.t.Logf("actor RPC %s -> %s reachable=%v", pair[0], pair[1], reachable)
	}
}

// TestLiveBitcoinPartition proves isolated peer chains, independent control, and observed reconvergence.
func TestLiveBitcoinPartition(t *testing.T) {
	namespace := os.Getenv("STACKS_CHAOS_BITCOIN_NAMESPACE")
	if namespace == "" {
		namespace = "partition-bitcoin"
	}
	f := newFaultFixture(t, namespace, "chaos", true)
	f.pod("bitcoin")
	f.pod("bitcoin-2")
	for _, expiry := range []bool{false, true} {
		name := fmt.Sprintf("bitcoin-partition-expiry-%t", expiry)
		f.pause(true, "bitcoin", "bitcoin-2")
		f.wait("common starting chain", 90*time.Second, func() bool { _, a := f.chain("bitcoin"); _, b := f.chain("bitcoin-2"); return a == b })
		f.checkPeerRPC(true)
		fault := f.partition(name, "bitcoin", "bitcoin-2", "60s")
		f.inject(fault)
		startA, startB := f.receipts("bitcoin"), f.receipts("bitcoin-2")
		f.pause(false)
		f.checkPeerRPC(false)
		f.wait("both independent producer receipt paths", 35*time.Second, func() bool { return f.receipts("bitcoin") > startA && f.receipts("bitcoin-2") > startB })
		// Compare drained tips: an in-flight receipt can change relative work while pause propagates.
		f.wait("stable unequal split-chain work", 25*time.Second, func() bool {
			f.pause(true, "bitcoin", "bitcoin-2")
			a, ha := f.chain("bitcoin")
			b, hb := f.chain("bitcoin-2")
			if a != b && ha != hb {
				return true
			}
			before := f.receipts("bitcoin") + f.receipts("bitcoin-2")
			f.pause(false)
			f.wait("another split-chain receipt", 10*time.Second, func() bool {
				return f.receipts("bitcoin")+f.receipts("bitcoin-2") > before
			})
			return false
		})
		a, ha := f.chain("bitcoin")
		b, hb := f.chain("bitcoin-2")
		if a == b || ha == hb || !f.condition(name, "AllInjected") || f.condition(name, "AllRecovered") {
			t.Fatal("split-chain evidence not established during injection")
		}
		t.Logf("split tips=%d/%s,%d/%s; receipts=%d->%d,%d->%d", a, ha, b, hb, startA, f.receipts("bitcoin"), startB, f.receipts("bitcoin-2"))
		// Delay and partition consume the same existing-object quota.
		extra := f.partition("quota-probe", "bitcoin", "bitcoin-2", "10s")
		_ = unstructured.SetNestedField(extra.Object, "delay", "spec", "action")
		_ = unstructured.SetNestedField(extra.Object, "to", "spec", "direction")
		_ = unstructured.SetNestedField(extra.Object, map[string]any{"latency": "100ms"}, "spec", "delay")
		if err := f.admin.Create(f.ctx, extra, client.DryRunAll); err == nil || !strings.Contains(err.Error(), "exceeded quota") {
			t.Fatalf("combined quota: %v", err)
		}
		f.recover(fault, expiry)
		f.checkPeerRPC(true)
		f.wait("heavier-chain reconvergence", 90*time.Second, func() bool { _, a := f.chain("bitcoin"); _, b := f.chain("bitcoin-2"); return a == b })
		_, tip := f.chain("bitcoin")
		expected := ha
		if b > a {
			expected = hb
		}
		if tip != expected {
			t.Fatal("reconnection did not select the observed heavier branch")
		}
		before := f.receipts("bitcoin") + f.receipts("bitcoin-2")
		f.pause(false)
		f.wait("production after reconnection", 20*time.Second, func() bool { return f.receipts("bitcoin")+f.receipts("bitcoin-2") > before })
		t.Logf("rejoined tip=%s; production resumed", tip)
		f.pod("bitcoin")
		f.pod("bitcoin-2")
	}
	f.pause(true, "bitcoin", "bitcoin-2")
}

// stacksBurnHeight reads node telemetry through the administrator's API proxy, outside actor links.
func (f *faultFixture) stacksBurnHeight(actor string) int64 {
	f.t.Helper()
	ctx, cancel := context.WithTimeout(f.ctx, 10*time.Second)
	defer cancel()
	output, err := f.stacksQuery(ctx, f.pod(actor).Name, "info")
	if err != nil {
		f.t.Fatalf("Stacks info: %v", err)
	}
	var info struct {
		BurnBlockHeight *int64 `json:"burn_block_height"`
	}
	if json.Unmarshal(output, &info) != nil || info.BurnBlockHeight == nil {
		f.t.Fatal("missing Stacks burn height")
	}
	return *info.BurnBlockHeight
}

// TestLiveStacksBitcoinPartition observes burn-chain lag and subsequent productive recovery.
func TestLiveStacksBitcoinPartition(t *testing.T) {
	namespace := os.Getenv("STACKS_CHAOS_STACKS_NAMESPACE")
	if namespace == "" {
		namespace = "partition-stacks"
	}
	f := newFaultFixture(t, namespace, "stacks", true)
	f.onWaitTimeout = func(description string) { f.logCycleContext("timeout: " + description) }
	confirmed := func() int64 {
		return f.count("stacks.stacks.org", "StacksTransactionProduction", "stacks", "confirmed")
	}
	f.pod("bitcoin")
	f.pod("miner")
	f.pod("signer-node")
	initial := confirmed()
	f.wait("fresh productive Stacks baseline", 90*time.Second, func() bool { return confirmed() > initial })
	t.Logf("fresh baseline confirmations=%d->%d", initial, confirmed())
	for _, expiry := range []bool{false, true} {
		name := fmt.Sprintf("stacks-partition-expiry-%t", expiry)
		before, receipts := confirmed(), f.receipts("bitcoin")
		fault := f.partition(name, "miner", "bitcoin", "45s")
		f.logCycleContext("before injection: " + name)
		f.inject(fault)
		f.logCycleContext("injected: " + name)
		f.wait("selected miner burn-chain lag", 30*time.Second, func() bool {
			height, _ := f.chain("bitcoin")
			return f.receipts("bitcoin") >= receipts+2 && height-f.stacksBurnHeight("miner") >= 2 && f.stacksBurnHeight("signer-node") > f.stacksBurnHeight("miner")
		})
		if !f.condition(name, "AllInjected") || f.condition(name, "AllRecovered") {
			t.Fatal("lag not observed during active fault")
		}
		height, _ := f.chain("bitcoin")
		t.Logf("during fault: Core=%d miner=%d signer-node=%d; Bitcoin receipts=%d->%d; confirmed=%d->%d", height, f.stacksBurnHeight("miner"), f.stacksBurnHeight("signer-node"), receipts, f.receipts("bitcoin"), before, confirmed())
		f.recover(fault, expiry)
		f.logCycleContext("native cleanup: " + name)
		after := confirmed()
		f.wait("miner catches up and new transfer confirms", 120*time.Second, func() bool {
			height, _ := f.chain("bitcoin")
			return height-f.stacksBurnHeight("miner") <= 1 && confirmed() > after
		})
		f.logCycleContext("protocol recovery: " + name)
		t.Logf("recovered: confirmed=%d->%d", after, confirmed())
		f.pod("bitcoin")
		f.pod("miner")
		f.pod("signer-node")
	}
}
