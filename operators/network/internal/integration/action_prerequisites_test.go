//go:build integration

package integration

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	actionv1 "github.com/cylewitruk-stacks/stacks-k8s/apis/network/actions/v1alpha1"
	bitcoinv1 "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha1"
	networkv1 "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha1"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/production"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/config"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

// TestExecutorActionPrerequisites exercises discovery before startup and actual cache synchronization.
func TestExecutorActionPrerequisites(t *testing.T) {
	root := filepath.Join("..", "..", "..", "..")
	environment := &envtest.Environment{CRDDirectoryPaths: []string{filepath.Join(root, "charts/stacks-network-operator/crds")}, ErrorIfCRDPathMissing: true, DownloadBinaryAssets: true, DownloadBinaryAssetsVersion: "1.36", BinaryAssetsDirectory: filepath.Join(os.TempDir(), "stacks-network-operator-envtest")}
	configuration, err := environment.Start()
	must(t, err)
	t.Cleanup(func() { must(t, environment.Stop()) })
	scheme := runtime.NewScheme()
	for _, add := range []func(*runtime.Scheme) error{clientgoscheme.AddToScheme, networkv1.AddToScheme, bitcoinv1.AddToScheme, actionv1.AddToScheme} {
		must(t, add(scheme))
	}
	ctx := context.Background()
	direct, err := client.New(configuration, client.Options{Scheme: scheme})
	must(t, err)
	const namespace = "executor-prerequisites"
	must(t, direct.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}))
	n := bitcoinNetwork("baseline")
	n.Namespace = namespace
	policy := bitcoinv1.ProductionPolicy{Targets: []bitcoinv1.ProductionTarget{{Name: "bitcoin", Weight: 1, Address: "mipcBbFg9gMiCh81Kj8tqqdgoZub1ZJRfn"}}, IntervalSeconds: 1, Paused: true}
	n.Spec.BitcoinBlockProduction = &policy
	must(t, direct.Create(ctx, n))
	policyRoot := &bitcoinv1.BitcoinBlockProduction{ObjectMeta: metav1.ObjectMeta{Name: n.Name, Namespace: namespace}, Spec: bitcoinv1.BitcoinBlockProductionSpec{NetworkName: n.Name, NetworkUID: string(n.UID), Policy: policy}}
	must(t, controllerutil.SetControllerReference(n, policyRoot, scheme))
	must(t, direct.Create(ctx, policyRoot))
	n.Status.BitcoinProductionUID = string(policyRoot.UID)
	must(t, direct.Status().Update(ctx, n))
	ledger := createProductionTarget(t, ctx, direct, policyRoot)
	newManager := func() (ctrl.Manager, *production.Reconciler) {
		skipNames := true // These sequential managers share one test process and metric registry.
		m, err := ctrl.NewManager(configuration, ctrl.Options{Scheme: scheme, Metrics: metricsserver.Options{BindAddress: "0"}, Cache: cache.Options{DefaultNamespaces: map[string]cache.Config{namespace: {}}}, Controller: config.Controller{SkipNameValidation: &skipNames}})
		must(t, err)
		r := &production.Reconciler{Client: m.GetClient(), APIReader: m.GetAPIReader(), ConfigDigest: "sha256:" + strings.Repeat("a", 64), RPC: production.NewBitcoinRPC(production.Credentials{})}
		return m, r
	}
	for _, kind := range []string{"generation", "reorganization"} {
		t.Run("missing "+kind, func(t *testing.T) {
			m, r := newManager()
			r.ActionsEnabled = kind == "generation"
			r.ReorganizationEnabled = kind == "reorganization"
			err := r.SetupWithManager(m, 1)
			if err == nil || !strings.Contains(err.Error(), "bitcoin-"+kind+"-enabled") || !strings.Contains(err.Error(), "stacks-action-operator chart CRDs") {
				t.Fatalf("expected prerequisite error before manager.Start: %v", err)
			}
		})
	}
	run := func(t *testing.T, generation, reorganization bool) {
		// Require a fresh reconciliation after each manager starts; prior status cannot satisfy the check.
		must(t, direct.Get(ctx, client.ObjectKeyFromObject(ledger), ledger))
		ledger.Status.Phase = ""
		must(t, direct.Status().Update(ctx, ledger))
		m, r := newManager()
		r.ActionsEnabled = generation
		r.ReorganizationEnabled = reorganization
		must(t, r.SetupWithManager(m, 1))
		runCtx, cancel := context.WithCancel(ctx)
		done := make(chan error, 1)
		go func() { done <- m.Start(runCtx) }()
		defer func() {
			cancel()
			select {
			case err := <-done:
				must(t, err)
			case <-time.After(10 * time.Second):
				t.Error("manager did not stop")
			}
		}()
		syncCtx, stopSync := context.WithTimeout(runCtx, 15*time.Second)
		defer stopSync()
		if !m.GetCache().WaitForCacheSync(syncCtx) {
			t.Fatal("enabled caches did not synchronize")
		}
		eventually(t, "baseline reconciled", func() bool {
			return direct.Get(ctx, client.ObjectKeyFromObject(ledger), ledger) == nil && ledger.Status.Phase == "Paused" && len(ledger.Finalizers) > 0
		})
	}
	t.Run("baseline without action CRDs", func(t *testing.T) { run(t, false, false) })
	_, err = envtest.InstallCRDs(configuration, envtest.CRDInstallOptions{Paths: []string{filepath.Join(root, "charts/stacks-action-operator/crds")}, ErrorIfPathMissing: true})
	must(t, err)
	for _, tc := range []struct {
		name                       string
		generation, reorganization bool
	}{{"generation installed", true, false}, {"reorganization installed", false, true}, {"both installed", true, true}} {
		t.Run(tc.name, func(t *testing.T) { run(t, tc.generation, tc.reorganization) })
	}
}
