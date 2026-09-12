//go:build live

package integration

import (
	"bytes"
	"context"
	"os"
	"testing"
	"time"

	"github.com/cylewitruk-stacks/stacks-k8s/tools/chart-policy/internal/chaosprofile"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// TestLiveCurrentActorSelectors verifies current identities and admission without injecting a fault.
func TestLiveCurrentActorSelectors(t *testing.T) {
	if os.Getenv("STACKS_CHAOS_IDENTITY_LIVE") != "1" {
		t.Skip("set STACKS_CHAOS_IDENTITY_LIVE=1 for current actor selector qualification")
	}
	kubeconfig, kubecontext, namespace := os.Getenv("STACKS_CHAOS_KUBECONFIG"), os.Getenv("STACKS_CHAOS_CONTEXT"), os.Getenv("STACKS_CHAOS_NAMESPACE")
	source, target := os.Getenv("STACKS_CHAOS_SOURCE"), os.Getenv("STACKS_CHAOS_TARGET")
	if kubeconfig == "" || kubecontext == "" || namespace == "" || source == "" || target == "" {
		t.Fatal("explicit kubeconfig, context, namespace, source and target required")
	}
	config, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(&clientcmd.ClientConfigLoadingRules{ExplicitPath: kubeconfig}, &clientcmd.ConfigOverrides{CurrentContext: kubecontext}).ClientConfig()
	if err != nil {
		t.Fatal(err)
	}
	config.Timeout = 10 * time.Second
	scheme := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(scheme)
	c, err := client.New(config, client.Options{Scheme: scheme})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	raw, err := os.ReadFile("../../../../examples/chaos/network-delay.yaml")
	if err != nil {
		t.Fatal(err)
	}
	objects, err := chaosprofile.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	fault := objects[0]
	fault.SetNamespace(namespace)
	fault.SetName("current-actor-identity-check")
	if err := chaosprofile.BindActors(ctx, c, fault, source, target); err != nil {
		t.Fatal(err)
	}
	if err := c.Create(ctx, fault.DeepCopy(), client.DryRunAll); err != nil {
		t.Fatalf("current actor selectors rejected: %v", err)
	}
	for _, side := range [][]string{{"spec", "selector", "labelSelectors"}, {"spec", "target", "selector", "labelSelectors"}} {
		changed := fault.DeepCopy()
		_ = unstructured.SetNestedField(changed.Object, "control", append(side, "network.stacks.org/role")...)
		if err := c.Create(ctx, changed, client.DryRunAll); err == nil {
			t.Fatal("control worker selector admitted")
		}
	}
	t.Logf("verified current source %s and target %s with exact API identities; no fault was injected", source, target)
}
