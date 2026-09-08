//go:build integration

package ledgerlifecycle_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha1"
	stacks "github.com/cylewitruk-stacks/stacks-k8s/apis/network/stacks/v1alpha1"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha1"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/ledgerlifecycle"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/network"
	core "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	metrics "sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

func TestDisabledProductionReleasesRootRetention(t *testing.T) {
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	env := &envtest.Environment{CRDDirectoryPaths: []string{"../../../../charts/stacks-network-operator/crds"}, ErrorIfCRDPathMissing: true, DownloadBinaryAssets: true, DownloadBinaryAssetsVersion: "1.37.0", BinaryAssetsDirectory: filepath.Join(os.TempDir(), "stacks-network-operator-envtest")}
	cfg, err := env.Start()
	must(err)
	t.Cleanup(func() { must(env.Stop()) })
	scheme := runtime.NewScheme()
	must(clientgoscheme.AddToScheme(scheme))
	must(api.AddToScheme(scheme))
	must(bitcoin.AddToScheme(scheme))
	must(stacks.AddToScheme(scheme))
	m, err := ctrl.NewManager(cfg, ctrl.Options{Scheme: scheme, Metrics: metrics.Options{BindAddress: "0"}})
	must(err)
	// Match the disabled installation: aggregate and lifecycle run, scheduler/workers do not.
	must((&network.Reconciler{Client: m.GetClient(), APIReader: m.GetAPIReader(), Scheme: scheme, ProductionEnabled: false}).SetupWithManager(m, 1))
	must(ledgerlifecycle.SetupWithManager(m))
	c, err := client.New(cfg, client.Options{Scheme: scheme})
	must(err)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- m.Start(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			must(err)
		case <-time.After(10 * time.Second):
			t.Error("manager did not stop")
		}
	})
	wait := func(label string, check func() bool) {
		t.Helper()
		until := time.Now().Add(15 * time.Second)
		for time.Now().Before(until) {
			if check() {
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
		t.Fatal(label)
	}
	must(c.Create(ctx, &core.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "disabled-production"}}))
	parent := &api.StacksNetwork{ObjectMeta: metav1.ObjectMeta{Name: "network", Namespace: "disabled-production"}, Spec: api.StacksNetworkSpec{Defaults: api.NetworkDefaults{BitcoinImage: "bitcoin:test"}, BitcoinNodes: []api.BitcoinNodeTemplate{{Name: "bitcoin", Config: api.ConfigSource{Generated: &api.GeneratedConfig{Profile: "bitcoin-regtest/v1"}}}}, BitcoinBlockProduction: &bitcoin.ProductionPolicy{IntervalSeconds: 60, Targets: []bitcoin.ProductionTarget{{Name: "bitcoin", Weight: 1, Address: "bcrt1qexample0000000000000000000000000000000"}}}}}
	must(c.Create(ctx, parent))
	key := client.ObjectKeyFromObject(parent)
	root := &bitcoin.BitcoinBlockProduction{}
	wait("aggregate did not compile policy while scheduling disabled", func() bool { return c.Get(ctx, key, root) == nil })
	root.Finalizers = []string{ledgerlifecycle.PolicyFinalizer, "test.stacks.org/evidence"}
	must(c.Update(ctx, root))
	root.Status.Opportunities = 7
	must(c.Status().Update(ctx, root))
	uid := root.UID
	must(c.Delete(ctx, root))
	time.Sleep(2200 * time.Millisecond)
	must(c.Get(ctx, key, root))
	if len(root.Finalizers) != 2 {
		t.Fatal("policy retention released under a live parent")
	}
	must(c.Delete(ctx, parent))
	wait("disabled production policy remained retained", func() bool {
		return c.Get(ctx, key, root) == nil && len(root.Finalizers) == 1 && root.Status.Phase == "Abandoned"
	})
	if root.UID != uid || root.Status.Opportunities != 7 || root.Finalizers[0] != "test.stacks.org/evidence" {
		t.Fatal("retirement discarded identity/evidence or another finalizer")
	}
	root.Finalizers = nil
	must(c.Update(ctx, root))
	wait("retired root did not delete", func() bool { return apierrors.IsNotFound(c.Get(ctx, key, root)) })
}
