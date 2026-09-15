//go:build integration

package networkruntime

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"

	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

// TestCachedInventoryContinuationDoesNotBlockBootstrap exercises the real manager client.
func TestCachedInventoryContinuationDoesNotBlockBootstrap(t *testing.T) {
	environment := &envtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join("..", "..", "..", "..", "charts", "stacks-network-operator", "crds"),
		},
		ErrorIfCRDPathMissing:       true,
		DownloadBinaryAssets:        true,
		DownloadBinaryAssetsVersion: "1.37.0",
		BinaryAssetsDirectory:       filepath.Join(os.TempDir(), "stacks-network-operator-envtest"),
	}
	config, err := environment.Start()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := environment.Stop(); err != nil {
			t.Error(err)
		}
	})
	scheme := runtime.NewScheme()
	for _, add := range []func(*runtime.Scheme) error{corev1.AddToScheme, api.AddToScheme} {
		if err := add(scheme); err != nil {
			t.Fatal(err)
		}
	}
	direct, err := client.New(config, client.Options{Scheme: scheme})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	namespace := "cached-bootstrap"
	if err := direct.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}); err != nil {
		t.Fatal(err)
	}
	root := &api.StacksNetwork{
		ObjectMeta: metav1.ObjectMeta{Name: "network", Namespace: namespace},
		Spec:       api.StacksNetworkSpec{Operation: "Running"},
	}
	if err := direct.Create(ctx, root); err != nil {
		t.Fatal(err)
	}
	manager, err := ctrl.NewManager(
		config,
		ctrl.Options{
			Scheme:                 scheme,
			Metrics:                metricsserver.Options{BindAddress: "0"},
			HealthProbeBindAddress: "0",
			Cache:                  cache.Options{DefaultNamespaces: map[string]cache.Config{namespace: {}}},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.GetCache().GetInformer(ctx, &api.StacksNetworkParticipant{}); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- manager.Start(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(5 * time.Second):
			t.Error("manager did not stop")
		}
	})
	if !manager.GetCache().WaitForCacheSync(ctx) {
		t.Fatal("participant cache failed to synchronize")
	}
	var cached api.StacksNetworkParticipantList
	if err := manager.GetClient().List(ctx, &cached, client.InNamespace(namespace), client.Limit(1001)); err != nil {
		t.Fatal(err)
	}
	if len(cached.Items) != 0 || cached.Continue != "continue-not-supported" {
		t.Fatalf(
			"fixture did not exercise limited cache sentinel: count=%d continue=%q",
			len(cached.Items),
			cached.Continue,
		)
	}
	var live api.StacksNetworkParticipantList
	if err := direct.List(ctx, &live, client.InNamespace(namespace), client.Limit(1001)); err != nil {
		t.Fatal(err)
	}
	if len(live.Items) != 0 || live.Continue != "" {
		t.Fatal("API-server empty inventory fixture differs")
	}
	reconciler := &Reconciler{Client: manager.GetClient(), Reader: manager.GetAPIReader(), Scheme: scheme}
	if _, err := reconciler.Reconcile(ctx, root); err != nil {
		t.Fatal(err)
	}
	initialized := meta.FindStatusCondition(root.Status.Conditions, "Initialized")
	if initialized == nil || initialized.Status != metav1.ConditionFalse || initialized.Reason != "BootstrapPending" {
		t.Fatalf("complete cached inventory blocked bootstrap projection: %+v", initialized)
	}
	for _, condition := range root.Status.Conditions {
		if condition.Reason == "ObservationIncomplete" {
			t.Fatalf("cache continuation marker was treated as truncation: %+v", condition)
		}
	}
	// Persist completed bootstrap, then let the API server advance generation.
	// Mutable operation must not make the historical completion condition stale.
	set(root, "Initialized", metav1.ConditionTrue, "BootstrapCompleted", "frozen gates completed")
	if err := direct.Status().Update(ctx, root); err != nil {
		t.Fatal(err)
	}
	completed := *meta.FindStatusCondition(root.Status.Conditions, "Initialized")
	root.Spec.Operation = "Paused"
	if err := direct.Update(ctx, root); err != nil {
		t.Fatal(err)
	}
	if root.Generation <= completed.ObservedGeneration {
		t.Fatal("operation update did not advance API-server generation")
	}
	if _, err := reconciler.Reconcile(ctx, root); err != nil {
		t.Fatal(err)
	}
	if err := direct.Status().Update(ctx, root); err != nil {
		t.Fatal(err)
	}
	if err := direct.Get(ctx, client.ObjectKeyFromObject(root), root); err != nil {
		t.Fatal(err)
	}
	current := meta.FindStatusCondition(root.Status.Conditions, "Initialized")
	if current == nil || current.Status != metav1.ConditionTrue || current.ObservedGeneration != root.Generation ||
		current.Reason != completed.Reason ||
		current.Message != completed.Message ||
		!current.LastTransitionTime.Equal(&completed.LastTransitionTime) {
		t.Fatalf("operation edit lost or made bootstrap completion stale: %+v", current)
	}
	// Prove generated status schema preserves independently attributed samples and removes them.
	sample := &api.BurnHeightSample{
		Participant:         common.Binding{Kind: "StacksNetworkParticipant", Name: "btc", UID: "participant"},
		Pod:                 common.Binding{Kind: "Pod", Name: "btc-0", UID: "pod"},
		ContainerID:         "containerd://process",
		ConfigurationDigest: "config",
		Height:              1000,
		FirstObservedAt:     metav1.NewTime(time.Now().Truncate(time.Second)),
	}
	root.Status.BurnchainObservations = &api.BurnchainObservations{Bitcoin: sample}
	if err := direct.Status().Update(ctx, root); err != nil {
		t.Fatal(err)
	}
	if err := direct.Get(ctx, client.ObjectKeyFromObject(root), root); err != nil {
		t.Fatal(err)
	}
	if root.Status.BurnchainObservations == nil ||
		!reflect.DeepEqual(root.Status.BurnchainObservations.Bitcoin, sample) ||
		root.Status.BurnchainObservations.Miner != nil {
		t.Fatal("diagnostic identity or source time pruned")
	}
	root.Status.BurnchainObservations = nil
	if err := direct.Status().Update(ctx, root); err != nil {
		t.Fatal(err)
	}
	if err := direct.Get(ctx, client.ObjectKeyFromObject(root), root); err != nil {
		t.Fatal(err)
	}
	if root.Status.BurnchainObservations != nil {
		t.Fatal("unavailable diagnostic retained")
	}
}
