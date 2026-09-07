//go:build live

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/cylewitruk-stacks/stacks-k8s/tools/chart-policy/internal/chaosprofile"
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

// TestLiveNativeDelay qualifies the documented disposable two-Bitcoin-node fixture.
// It requires explicit opt-in, kubeconfig and context; it never uses the default cluster.
func TestLiveNativeDelay(t *testing.T) {
	if os.Getenv("STACKS_CHAOS_LIVE") != "1" {
		t.Skip("set STACKS_CHAOS_LIVE=1 for the isolated documented fixture")
	}
	kubeconfig, kubecontext := os.Getenv("STACKS_CHAOS_KUBECONFIG"), os.Getenv("STACKS_CHAOS_CONTEXT")
	if kubeconfig == "" || kubecontext == "" {
		t.Fatal("explicit STACKS_CHAOS_KUBECONFIG and STACKS_CHAOS_CONTEXT required")
	}
	config, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(&clientcmd.ClientConfigLoadingRules{ExplicitPath: kubeconfig}, &clientcmd.ConfigOverrides{CurrentContext: kubecontext}).ClientConfig()
	if err != nil {
		t.Fatal(err)
	}
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	admin, err := client.New(config, client.Options{Scheme: scheme})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	const ns = "chaos-live"
	native := func(name string) *unstructured.Unstructured {
		o := &unstructured.Unstructured{}
		o.SetGroupVersionKind(schema.GroupVersionKind{Group: "chaos-mesh.org", Version: "v1alpha1", Kind: "NetworkChaos"})
		o.SetNamespace(ns)
		o.SetName(name)
		return o
	}
	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(schema.GroupVersionKind{Group: "chaos-mesh.org", Version: "v1alpha1", Kind: "NetworkChaosList"})
	if err := admin.List(ctx, list, client.InNamespace(ns)); err != nil {
		t.Fatal(err)
	}
	if len(list.Items) != 0 {
		t.Fatal("qualification requires an empty NetworkChaos namespace")
	}
	destination := &corev1.Pod{}
	if err := admin.Get(ctx, client.ObjectKey{Namespace: ns, Name: "chaos-bitcoin-2-0"}, destination); err != nil {
		t.Fatal(err)
	}
	if destination.Status.PodIP == "" {
		t.Fatal("destination has no Pod IP")
	}
	secret := &corev1.Secret{}
	if err := admin.Get(ctx, client.ObjectKey{Namespace: ns, Name: "stacks-bitcoin-observer-rpc"}, secret); err != nil {
		t.Fatal(err)
	}
	var credentials struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.Unmarshal(secret.Data["credentials.json"], &credentials); err != nil {
		t.Fatal("invalid observer credentials")
	}
	if credentials.Username == "" || credentials.Password == "" {
		t.Fatal("missing observer credentials")
	}
	// Only the observer password enters stdin; no credential is placed in argv or logs.
	latency := func() time.Duration {
		t.Helper()
		var samples []time.Duration
		for range 3 {
			callctx, stop := context.WithTimeout(ctx, 10*time.Second)
			command := exec.CommandContext(callctx, "kubectl", "--kubeconfig", kubeconfig, "--context", kubecontext, "-n", ns, "exec", "-i", "chaos-bitcoin-0", "--", "bitcoin-cli", "-regtest", "-rpcconnect="+destination.Status.PodIP, "-rpcport=18443", "-rpcuser="+credentials.Username, "-stdinrpcpass", "getblockcount")
			command.Stdin = strings.NewReader(credentials.Password + "\n")
			start := time.Now()
			output, err := command.Output()
			elapsed := time.Since(start)
			stop()
			if err != nil {
				t.Fatalf("observer RPC failed: %v", err)
			}
			var height int64
			if err := json.Unmarshal(output, &height); err != nil {
				t.Fatal("invalid RPC height")
			}
			samples = append(samples, elapsed)
		}
		sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
		return samples[1]
	}
	progress := func() int64 {
		t.Helper()
		o := &unstructured.Unstructured{}
		o.SetGroupVersionKind(schema.GroupVersionKind{Group: "bitcoin.stacks.org", Version: "v1alpha1", Kind: "BitcoinProductionTarget"})
		if err := admin.Get(ctx, client.ObjectKey{Namespace: ns, Name: "chaos-bitcoin-2"}, o); err != nil {
			t.Fatal(err)
		}
		count, found, err := unstructured.NestedInt64(o.Object, "status", "blocksProduced")
		if err != nil || !found {
			t.Fatalf("missing production receipt count: %v", err)
		}
		return count
	}
	wait := func(description string, condition func() bool) {
		t.Helper()
		deadline := time.Now().Add(75 * time.Second)
		for !condition() {
			if time.Now().After(deadline) || ctx.Err() != nil {
				t.Fatal("timed out: " + description)
			}
			time.Sleep(200 * time.Millisecond)
		}
	}
	condition := func(name, kind string) bool {
		o := native(name)
		if err := admin.Get(ctx, client.ObjectKeyFromObject(o), o); err != nil {
			t.Fatal(err)
		}
		conditions, _, _ := unstructured.NestedSlice(o.Object, "status", "conditions")
		for _, c := range conditions {
			m := c.(map[string]any)
			if m["type"] == kind && m["status"] == "True" {
				return true
			}
		}
		return false
	}
	// The type-checking controller is present here, but absent from envtest.
	wait("current CEL type checking", func() bool {
		policy := &admissionv1.ValidatingAdmissionPolicy{}
		if err := admin.Get(ctx, client.ObjectKey{Name: "stacks-network-delay-" + ns}, policy); err != nil {
			t.Fatal(err)
		}
		if policy.Status.ObservedGeneration != policy.Generation || policy.Status.TypeChecking == nil {
			return false
		}
		if len(policy.Status.TypeChecking.ExpressionWarnings) != 0 {
			t.Fatalf("CEL type warnings: %+v", policy.Status.TypeChecking.ExpressionWarnings)
		}
		return true
	})
	baseline := latency()
	t.Logf("baseline median actor RPC: %s; destination Pod UID: %s", baseline, destination.UID)
	raw, err := os.ReadFile("../../../../examples/chaos/network-delay.yaml")
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"cancellation", "expiry"} {
		t.Log("case:", mode)
		faults, err := chaosprofile.Decode(bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		fault := faults[0]
		name := "qualification-" + mode
		fault.SetName(name)
		// Leave enough time for receipt progress and three measured delayed requests.
		_ = unstructured.SetNestedField(fault.Object, "30s", "spec", "duration")
		if err := admin.Create(ctx, fault); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			cleanupCtx, stop := context.WithTimeout(context.Background(), 15*time.Second)
			defer stop()
			if err := admin.Delete(cleanupCtx, native(name)); err != nil && !apierrors.IsNotFound(err) {
				t.Error(err)
			}
		})
		wait("native injection", func() bool { return condition(name, "AllInjected") })
		count := progress()
		during := latency()
		if during < baseline+300*time.Millisecond {
			t.Fatalf("no measured delay: baseline=%s during=%s", baseline, during)
		}
		wait("producer receipt during injection", func() bool { return progress() > count })
		if !condition(name, "AllInjected") || condition(name, "AllRecovered") {
			t.Fatal("production evidence was not collected during active injection")
		}
		t.Logf("fault UID=%s; delayed median=%s; producer receipts %d -> %d", fault.GetUID(), during, count, progress())
		fresh, err := chaosprofile.Decode(bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		extra := fresh[0]
		extra.SetName("qualification-extra")
		if err := admin.Create(ctx, extra, client.DryRunAll); err == nil || !strings.Contains(err.Error(), "exceeded quota") {
			t.Fatalf("one-object quota not enforced: %v", err)
		}
		if mode == "cancellation" {
			if err := admin.Delete(ctx, native(name)); err != nil {
				t.Fatal(err)
			}
			wait("finalized deletion", func() bool {
				err := admin.Get(ctx, client.ObjectKey{Namespace: ns, Name: name}, native(name))
				if err != nil && !apierrors.IsNotFound(err) {
					t.Fatal(err)
				}
				return apierrors.IsNotFound(err)
			})
		} else {
			// Controller replacement must retain the native fault and eventually recover it.
			for _, args := range [][]string{{"rollout", "restart", "deployment/chaos-controller-manager"}, {"rollout", "status", "deployment/chaos-controller-manager", "--timeout=20s"}} {
				command := exec.CommandContext(ctx, "kubectl", append([]string{"--kubeconfig", kubeconfig, "--context", kubecontext, "-n", "chaos-mesh"}, args...)...)
				if output, err := command.CombinedOutput(); err != nil {
					t.Fatalf("controller restart: %v: %s", err, output)
				}
			}
			if afterRestart := latency(); afterRestart < baseline+300*time.Millisecond {
				t.Fatalf("delay absent after controller replacement: %s", afterRestart)
			}
			t.Log("native controller replaced; delay remains measurable before expiry")
			wait("native duration recovery", func() bool { return condition(name, "AllRecovered") })
			if err := admin.Delete(ctx, native(name)); err != nil {
				t.Fatal(err)
			}
			wait("expired object deletion", func() bool {
				return apierrors.IsNotFound(admin.Get(ctx, client.ObjectKey{Namespace: ns, Name: name}, native(name)))
			})
		}
		recovered := latency()
		if recovered >= during-300*time.Millisecond {
			t.Fatalf("delay did not recover: during=%s after=%s", during, recovered)
		}
		t.Logf("recovered median actor RPC: %s", recovered)
	}
	current := &corev1.Pod{}
	if err := admin.Get(ctx, client.ObjectKeyFromObject(destination), current); err != nil {
		t.Fatal(err)
	}
	if current.UID != destination.UID || current.Status.PodIP != destination.Status.PodIP {
		t.Fatal("destination identity changed during qualification")
	}
	t.Logf("qualified native delay on %s/%s", kubecontext, ns)
}
