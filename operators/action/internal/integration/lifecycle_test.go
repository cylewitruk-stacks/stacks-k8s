//go:build integration

package integration

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	actionv1 "github.com/cylewitruk-stacks/stacks-k8s/apis/network/actions/v1alpha1"
	bitcoinv1 "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha1"
	networkv1 "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha1"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/action/controllers/generation"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/action/internal/manager"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/yaml"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

// TestChartAuthorizedLifecycleRestart exercises the rendered Role against an API server.
func TestChartAuthorizedLifecycleRestart(t *testing.T) {
	ctx := context.Background()
	root := filepath.Join("..", "..", "..", "..")
	environment := &envtest.Environment{CRDDirectoryPaths: []string{filepath.Join(root, "charts/stacks-network-operator/crds"), filepath.Join(root, "charts/stacks-action-operator/crds")}, ErrorIfCRDPathMissing: true, DownloadBinaryAssets: true, DownloadBinaryAssetsVersion: "1.36", BinaryAssetsDirectory: filepath.Join(os.TempDir(), "stacks-network-operator-envtest")}
	config, err := environment.Start()
	must(t, err)
	t.Cleanup(func() { must(t, environment.Stop()) })
	scheme := runtime.NewScheme()
	for _, add := range []func(*runtime.Scheme) error{clientgoscheme.AddToScheme, actionv1.AddToScheme, bitcoinv1.AddToScheme, networkv1.AddToScheme} {
		must(t, add(scheme))
	}
	admin, err := client.New(config, client.Options{Scheme: scheme})
	must(t, err)
	const namespace = "action-boundary"
	must(t, admin.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}))
	rendered, err := exec.Command("helm", "template", "action", filepath.Join(root, "charts/stacks-action-operator"), "--namespace", namespace).Output()
	must(t, err)
	decoder := yaml.NewYAMLOrJSONDecoder(bytes.NewReader(rendered), 4096)
	account := ""
	for {
		object := &unstructured.Unstructured{}
		err := decoder.Decode(object)
		if err == io.EOF {
			break
		}
		must(t, err)
		switch object.GetKind() {
		case "Role", "RoleBinding", "ServiceAccount":
			must(t, admin.Create(ctx, object))
			if object.GetKind() == "ServiceAccount" {
				account = object.GetName()
			}
		}
	}
	if account == "" {
		t.Fatal("chart omitted ServiceAccount")
	}
	user, err := environment.AddUser(envtest.User{Name: "system:serviceaccount:" + namespace + ":" + account, Groups: []string{"system:serviceaccounts", "system:serviceaccounts:" + namespace}}, config)
	must(t, err)
	restricted, err := client.New(user.Config(), client.Options{Scheme: scheme})
	must(t, err)
	n := &networkv1.StacksNetwork{ObjectMeta: metav1.ObjectMeta{Name: "network", Namespace: namespace}, Spec: networkv1.StacksNetworkSpec{BitcoinNodes: []networkv1.BitcoinNodeTemplate{{Name: "bitcoin", Config: networkv1.ConfigSource{Generated: &networkv1.GeneratedConfig{Profile: "bitcoin-regtest/v1"}}}}}}
	must(t, admin.Create(ctx, n))
	policyRoot := &bitcoinv1.BitcoinBlockProduction{ObjectMeta: metav1.ObjectMeta{Name: n.Name, Namespace: namespace}, Spec: bitcoinv1.BitcoinBlockProductionSpec{NetworkName: n.Name, NetworkUID: string(n.UID), Policy: bitcoinv1.ProductionPolicy{Targets: []bitcoinv1.ProductionTarget{{Name: "bitcoin", Weight: 1, Address: "mipcBbFg9gMiCh81Kj8tqqdgoZub1ZJRfn"}}, IntervalSeconds: 1}}}
	must(t, controllerutil.SetControllerReference(n, policyRoot, scheme))
	must(t, admin.Create(ctx, policyRoot))
	n.Status.BitcoinProductionUID = string(policyRoot.UID)
	must(t, admin.Status().Update(ctx, n))
	p := &bitcoinv1.BitcoinProductionTarget{ObjectMeta: metav1.ObjectMeta{Name: "network-bitcoin", Namespace: namespace}, Spec: bitcoinv1.BitcoinProductionTargetSpec{NetworkName: n.Name, NetworkUID: string(n.UID), ProductionUID: string(policyRoot.UID), Policy: *policyRoot.Spec.Policy.ExecutionPolicy("bitcoin")}}
	must(t, controllerutil.SetControllerReference(policyRoot, p, scheme))
	must(t, admin.Create(ctx, p))
	policyRoot.Status.Targets = []bitcoinv1.TargetLedger{{Name: "bitcoin", ResourceName: p.Name, UID: string(p.UID)}}
	must(t, admin.Status().Update(ctx, policyRoot))
	a := &actionv1.BitcoinBlockGeneration{ObjectMeta: metav1.ObjectMeta{Name: "finite", Namespace: namespace}, Spec: actionv1.BitcoinBlockGenerationSpec{NetworkRef: actionv1.LocalReference{Name: n.Name}, BitcoinNodeRef: actionv1.LocalReference{Name: "network-bitcoin"}, Count: 1, IntervalSeconds: 1, Address: p.Spec.Policy.Address, Timeout: metav1.Duration{Duration: time.Minute}}}
	must(t, admin.Create(ctx, a))
	p.Status.DispatchState = "Armed"
	p.Status.Action = &bitcoinv1.GenerationReservation{Spec: a.Spec, ActionReservation: bitcoinv1.ActionReservation{Name: a.Name, UID: string(a.UID), Generation: a.Generation, AdmittedAt: metav1.Now(), ExpiresAt: metav1.NewTime(a.CreationTimestamp.Add(time.Minute)), Network: actionv1.NetworkIdentity{Name: n.Name, UID: string(n.UID), ObservedGeneration: n.Generation}, Policy: actionv1.PolicyIdentity{UID: string(p.UID)}}}
	must(t, admin.Status().Update(ctx, p))
	// Real chart authority permits lifecycle writes, but neither ledger nor workload authority.
	for label, operation := range map[string]func() error{
		"ledger write":   func() error { return restricted.Status().Update(ctx, p) },
		"topology write": func() error { return restricted.Update(ctx, n) },
		"secret read": func() error {
			return restricted.Get(ctx, client.ObjectKey{Namespace: namespace, Name: "absent"}, &corev1.Secret{})
		},
		"pod deletion": func() error {
			return restricted.Delete(ctx, &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "absent", Namespace: namespace}})
		},
		"disabled kind list": func() error {
			return restricted.List(ctx, &actionv1.BitcoinReorganizationList{}, client.InNamespace(namespace))
		},
	} {
		if err := operation(); !apierrors.IsForbidden(err) {
			t.Fatalf("%s: expected forbidden, got %v", label, err)
		}
	}
	starts := 0
	start := func() func() {
		options := manager.Options{Namespace: namespace, GenerationEnabled: true, Concurrency: 2, MetricsAddress: "0", ProbeAddress: "0"}
		m, err := options.New(user.Config(), scheme)
		must(t, err)
		r := &generation.Reconciler{Client: m.GetClient(), APIReader: m.GetAPIReader(), Now: time.Now}
		if starts == 0 {
			must(t, r.SetupWithManager(m))
		} else {
			// Production restarts have separate metric registries; this in-process restart needs a distinct controller name.
			must(t, ctrl.NewControllerManagedBy(m).Named("restarted-generation").For(&actionv1.BitcoinBlockGeneration{}).Complete(r))
		}
		starts++
		run, cancel := context.WithCancel(ctx)
		done := make(chan error, 1)
		go func() { done <- m.Start(run) }()
		return func() {
			cancel()
			select {
			case err := <-done:
				must(t, err)
			case <-time.After(10 * time.Second):
				t.Fatal("manager did not stop")
			}
		}
	}
	stop := start()
	wait(t, func() bool {
		return admin.Get(ctx, client.ObjectKeyFromObject(a), a) == nil && a.Status.Phase == "Admitted" && controllerutil.ContainsFinalizer(a, actionv1.CleanupFinalizer)
	})
	stop()
	// A new lifecycle process must preserve Armed work until the executor publishes a receipt.
	stop = start()
	defer stop()
	must(t, admin.Get(ctx, client.ObjectKeyFromObject(p), p))
	if p.Status.DispatchState != "Armed" || p.Status.Action == nil {
		t.Fatal("lifecycle restart changed execution authority")
	}
	now := metav1.Now()
	p.Status.DispatchState = "Idle"
	p.Status.Action.StartedAt = &now
	p.Status.Action.BlocksGenerated = 1
	p.Status.Action.LastDispatchID = "receipt"
	p.Status.Action.LastBlockHash = "retained"
	p.Status.Action.LastCompletedAt = &now
	must(t, admin.Status().Update(ctx, p))
	wait(t, func() bool {
		return admin.Get(ctx, client.ObjectKeyFromObject(a), a) == nil && a.Status.Phase == "Completed" && a.Status.LastDispatchID == "receipt"
	})
	if a.Status.BlocksGenerated != 1 || !controllerutil.ContainsFinalizer(a, actionv1.CleanupFinalizer) {
		t.Fatal("receipt/finalizer not retained before executor release")
	}
	must(t, admin.Get(ctx, client.ObjectKeyFromObject(p), p))
	p.Status.Action = nil
	must(t, admin.Status().Update(ctx, p))
	wait(t, func() bool { return admin.Get(ctx, client.ObjectKeyFromObject(a), a) == nil && len(a.Finalizers) == 0 })
	must(t, admin.Delete(ctx, a))
	if err := admin.Get(ctx, client.ObjectKeyFromObject(a), a); !apierrors.IsNotFound(err) {
		t.Fatalf("released action not deleted: %v", err)
	}
}

// must fails the test on unexpected setup or API errors.
func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// wait bounds an eventually consistent lifecycle observation.
func wait(t *testing.T, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if check() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("lifecycle observation timed out")
}
