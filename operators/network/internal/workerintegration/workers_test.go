//go:build integration

package workerintegration

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha1"
	stacks "github.com/cylewitruk-stacks/stacks-k8s/apis/network/stacks/v1alpha1"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha1"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/accountledger"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/ledgerlifecycle"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/network"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/productionscheduler"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/workers"
	apps "k8s.io/api/apps/v1"
	core "k8s.io/api/core/v1"
	rbac "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/yaml"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	metrics "sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

func TestOperatorProvisionsIndependentNetworkWorkers(t *testing.T) {
	env := &envtest.Environment{CRDDirectoryPaths: []string{filepath.Join("..", "..", "..", "..", "charts", "stacks-network-operator", "crds")}, ErrorIfCRDPathMissing: true, DownloadBinaryAssets: true, DownloadBinaryAssetsVersion: "1.37.0", BinaryAssetsDirectory: filepath.Join(os.TempDir(), "stacks-network-operator-envtest"), ControlPlaneStartTimeout: 60 * time.Second}
	env.ControlPlane.GetAPIServer().Configure().Set("authorization-mode", "RBAC")
	config, err := env.Start()
	must(t, err)
	t.Cleanup(func() { must(t, env.Stop()) })
	scheme := runtime.NewScheme()
	must(t, clientgoscheme.AddToScheme(scheme))
	must(t, api.AddToScheme(scheme))
	must(t, bitcoin.AddToScheme(scheme))
	must(t, stacks.AddToScheme(scheme))
	admin, err := client.New(config, client.Options{Scheme: scheme})
	must(t, err)
	must(t, admin.Create(context.Background(), &core.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "operator-system"}}))
	rendered, err := exec.Command("helm", "template", "verify", "../../../../charts/stacks-network-operator", "--namespace", "operator-system").CombinedOutput()
	if err != nil {
		t.Fatalf("render installation: %v: %s", err, rendered)
	}
	decoder := yaml.NewYAMLOrJSONDecoder(bytes.NewReader(rendered), 4096)
	for {
		object := &unstructured.Unstructured{}
		err := decoder.Decode(object)
		if err == io.EOF {
			break
		}
		must(t, err)
		if object.GetKind() != "" {
			must(t, admin.Create(context.Background(), object))
		}
	}
	operatorConfig := rest.CopyConfig(config)
	operatorConfig.Impersonate = rest.ImpersonationConfig{UserName: "system:serviceaccount:operator-system:verify-stacks-network-operator"}
	m, err := ctrl.NewManager(operatorConfig, ctrl.Options{Scheme: scheme, Metrics: metrics.Options{BindAddress: "0"}})
	must(t, err)
	must(t, (&network.Reconciler{Client: m.GetClient(), APIReader: m.GetAPIReader(), Scheme: scheme, ProductionEnabled: true, TransactionsEnabled: true, OperationEnabled: true}).SetupWithManager(m, 1))
	must(t, (&productionscheduler.Reconciler{Client: m.GetClient(), APIReader: m.GetAPIReader()}).SetupWithManager(m))
	must(t, (&accountledger.Reconciler{Client: m.GetClient(), Reader: m.GetAPIReader()}).SetupWithManager(m))
	must(t, ledgerlifecycle.SetupWithManager(m))
	for _, component := range []string{"bitcoin-production", "stacks-transactions", "stacks-contracts", "stacks-stacking", "stacks-receipts"} {
		must(t, (&workers.Reconciler{Client: m.GetClient(), Reader: m.GetAPIReader(), Scheme: scheme, Component: component, Settings: workers.Settings{Image: "operator:test", SDKImage: "sdk:test", PullPolicy: core.PullIfNotPresent}}).SetupWithManager(m))
	}
	c, err := client.New(config, client.Options{Scheme: scheme})
	must(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- m.Start(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			must(t, err)
		case <-time.After(10 * time.Second):
			t.Error("manager did not stop")
		}
	})
	parents := []*api.StacksNetwork{}
	for _, ns := range []string{"workers-one", "workers-two"} {
		must(t, c.Create(ctx, &core.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}}))
		parent := workerNetwork(ns)
		must(t, c.Create(ctx, parent))
		parents = append(parents, parent)
	}
	for _, parent := range parents {
		deployments := &apps.DeploymentList{}
		eventually(t, "five independently provisioned worker deployments in "+parent.Namespace, func() bool {
			return c.List(ctx, deployments, client.InNamespace(parent.Namespace)) == nil && len(deployments.Items) == 5
		})
		for _, d := range deployments.Items {
			if d.Namespace != parent.Namespace || len(d.OwnerReferences) != 1 || len(d.Spec.Template.Spec.Containers) != 1 {
				t.Fatal("invalid worker ownership")
			}
			component := d.Spec.Template.Labels["app.kubernetes.io/name"]
			if component == "stacks-stacking" && len(d.Spec.Template.Spec.Volumes) != 3 {
				t.Fatal("stacking worker must mount only holder, administrator and consensus keys")
			}
			role := &rbac.Role{}
			must(t, c.Get(ctx, client.ObjectKeyFromObject(&d), role))
			for _, rule := range role.Rules {
				for _, resource := range rule.Resources {
					if resource == "secrets" {
						t.Fatal("worker can read namespace Secrets")
					}
				}
			}
			if component == "stacks-receipts" && len(d.Spec.Template.Spec.Volumes) != 0 {
				t.Fatal("receipt worker received signing keys")
			}
		}
	}
	// Creation records Update ownership. Later desired changes must still converge
	// through Apply, including receipt permissions as account pins accumulate.
	for _, parent := range parents {
		roleKey := client.ObjectKey{Namespace: parent.Namespace, Name: workers.Name(parent, "stacks-receipts")}
		role := &rbac.Role{}
		eventually(t, "all receipt account permissions converge", func() bool {
			if c.Get(ctx, roleKey, role) != nil {
				return false
			}
			for _, rule := range role.Rules {
				if len(rule.Resources) == 1 && rule.Resources[0] == "stacksaccounts/status" {
					return len(rule.ResourceNames) == 3
				}
			}
			return false
		})
		original := role.DeepCopy()
		role.Rules = role.Rules[:1]
		must(t, c.Update(ctx, role, client.FieldOwner("qualification-edit")))
		eventually(t, "owned Role fields are repaired", func() bool {
			return c.Get(ctx, roleKey, role) == nil && reflect.DeepEqual(role.Rules, original.Rules)
		})
	}
	parent := parents[0]
	ledger := &stacks.StacksTransactionProduction{}
	key := client.ObjectKeyFromObject(parent)
	must(t, c.Get(ctx, key, ledger))
	ledger.Status.Outstanding = true
	ledger.Status.TxID = strings.Repeat("a", 64)
	ledger.Status.Nonce = 3
	ledger.Status.Phase = "Ambiguous"
	must(t, c.Status().Update(ctx, ledger))
	workerKey := client.ObjectKey{Namespace: parent.Namespace, Name: workers.Name(ledger, "stacks-transactions")}
	d := &apps.Deployment{}
	must(t, c.Get(ctx, workerKey, d))
	oldUID := d.UID
	// Defaulted fields must not cause continual writes or rollout churn.
	version := d.ResourceVersion
	time.Sleep(6 * time.Second)
	must(t, c.Get(ctx, workerKey, d))
	if d.ResourceVersion != version {
		t.Fatal("defaulting causes repeated workload writes")
	}
	must(t, c.Delete(ctx, d))
	eventually(t, "deleted execution Deployment is recreated", func() bool { return c.Get(ctx, workerKey, d) == nil && d.UID != oldUID })
	must(t, c.Get(ctx, key, ledger))
	if !ledger.Status.Outstanding || ledger.Status.Nonce != 3 || ledger.Status.TxID != strings.Repeat("a", 64) {
		t.Fatal("worker recreation reset execution authority")
	}
	other := &apps.Deployment{}
	must(t, c.Get(ctx, client.ObjectKey{Namespace: parents[1].Namespace, Name: workerKey.Name}, other))
	otherUID := other.UID
	// Exercise the API-server authorizer, not just rendered policy shape.
	workerConfig := rest.CopyConfig(config)
	workerConfig.Impersonate = rest.ImpersonationConfig{UserName: "system:serviceaccount:" + parent.Namespace + ":" + d.Spec.Template.Spec.ServiceAccountName}
	worker, err := client.New(workerConfig, client.Options{Scheme: scheme})
	must(t, err)
	own := &stacks.StacksTransactionProduction{}
	must(t, worker.Get(ctx, key, own))
	before := own.DeepCopy()
	own.Status.Message = "Scoped worker status test"
	must(t, worker.Status().Patch(ctx, own, client.MergeFrom(before)))

	if err := worker.Get(ctx, client.ObjectKey{Namespace: parents[1].Namespace, Name: key.Name}, &stacks.StacksTransactionProduction{}); !apierrors.IsForbidden(err) {
		t.Fatalf("worker escaped namespace: %v", err)
	}
	if err := worker.Get(ctx, client.ObjectKey{Namespace: parent.Namespace, Name: "transfer-key"}, &core.Secret{}); !apierrors.IsForbidden(err) {
		t.Fatalf("worker acquired Secret API access: %v", err)
	}
	forbidden := &stacks.StacksTransactionProduction{ObjectMeta: metav1.ObjectMeta{Name: "other-ledger", Namespace: parent.Namespace}}
	if err := worker.Status().Patch(ctx, forbidden, client.RawPatch(types.MergePatchType, []byte(`{"status":{"message":"forbidden"}}`))); !apierrors.IsForbidden(err) {
		t.Fatalf("worker acquired another ledger: %v", err)
	}
	if err := worker.Create(ctx, &apps.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "forbidden", Namespace: parent.Namespace}}); !apierrors.IsForbidden(err) {
		t.Fatalf("worker acquired workload mutation: %v", err)
	}

	must(t, updateNetworkWithRetry(ctx, c, parent, func(p *api.StacksNetwork) {
		p.Spec.Operation.Paused = true
		p.Spec.StacksTransactionProduction.Paused = true
		p.Spec.BitcoinBlockProduction.Paused = true
	}))
	time.Sleep(500 * time.Millisecond)
	must(t, c.Get(ctx, client.ObjectKeyFromObject(other), other))
	if other.UID != otherUID {
		t.Fatal("another network's pause changed worker identity")
	}

	// Withdrawal keeps pinned observation workers repairable without signing mounts.
	accounts := &stacks.StacksAccountList{}
	must(t, c.List(ctx, accounts, client.InNamespace(parent.Namespace)))
	account := &accounts.Items[0]
	account.Status.Phase = "Ambiguous"
	account.Status.Transaction = &stacks.AccountTransaction{Ordinal: 1, OperationDigest: "sha256:" + strings.Repeat("b", 64), ConsumerUID: "consumer", TxID: strings.Repeat("c", 64), Nonce: 7, AuthorizedAt: metav1.Now()}
	must(t, c.Status().Update(ctx, account))
	retained := account.Status.DeepCopy()
	must(t, updateNetworkWithRetry(ctx, c, parent, func(p *api.StacksNetwork) { p.Spec.Operation = nil }))
	deployments := &apps.DeploymentList{}
	must(t, c.List(ctx, deployments, client.InNamespace(parent.Namespace)))
	for _, old := range deployments.Items {
		component := old.Spec.Template.Labels["app.kubernetes.io/name"]
		if component != "stacks-contracts" && component != "stacks-stacking" && component != "stacks-receipts" {
			continue
		}
		stripped := &apps.Deployment{}
		eventually(t, "existing withdrawn worker loses signing mounts", func() bool {
			return c.Get(ctx, client.ObjectKeyFromObject(&old), stripped) == nil && len(stripped.Spec.Template.Spec.Volumes) == 0 && len(stripped.Spec.Template.Spec.Containers[0].VolumeMounts) == 0
		})
		must(t, c.Delete(ctx, &old))
		recreated := &apps.Deployment{}
		eventually(t, "withdrawn observation worker recreated without keys", func() bool {
			return c.Get(ctx, client.ObjectKeyFromObject(&old), recreated) == nil && recreated.UID != old.UID && len(recreated.Spec.Template.Spec.Volumes) == 0
		})
	}
	must(t, c.Get(ctx, client.ObjectKeyFromObject(account), account))
	if !reflect.DeepEqual(account.Status, *retained) {
		t.Fatal("withdrawal reset account evidence")
	}
	// Envtest has no execution Pods. Extra retention keeps abandonment evidence inspectable.
	must(t, c.Get(ctx, key, ledger))
	ledger.Finalizers = []string{ledgerlifecycle.TransferFinalizer, "test.stacks.org/evidence"}
	must(t, c.Update(ctx, ledger))
	targets := &bitcoin.BitcoinProductionTargetList{}
	must(t, c.List(ctx, targets, client.InNamespace(parent.Namespace)))
	if len(targets.Items) != 1 {
		t.Fatal("expected one target ledger")
	}
	target := &targets.Items[0]
	target.Finalizers = []string{ledgerlifecycle.BitcoinFinalizer, "test.stacks.org/evidence"}
	must(t, c.Update(ctx, target))
	target.Status.DispatchState, target.Status.DispatchID = "Armed", "unresolved-dispatch"
	must(t, c.Status().Update(ctx, target))
	must(t, c.Delete(ctx, ledger))
	must(t, c.Delete(ctx, target))
	time.Sleep(500 * time.Millisecond)
	must(t, c.Get(ctx, key, ledger))
	if len(ledger.Finalizers) != 2 {
		t.Fatal("live parent did not retain ledger")
	}
	must(t, c.Delete(ctx, parent))
	for _, object := range []client.Object{ledger, target} {
		eventually(t, "operator releases ledger without execution", func() bool {
			return c.Get(ctx, client.ObjectKeyFromObject(object), object) == nil && len(object.GetFinalizers()) == 1
		})
	}
	if ledger.Status.Phase != "Abandoned" || !ledger.Status.Outstanding || ledger.Status.Nonce != 3 || ledger.Status.TxID != strings.Repeat("a", 64) {
		t.Fatal("disposal discarded transfer evidence")
	}
	if target.Status.Phase != "Abandoned" || target.Status.DispatchState != "Armed" || target.Status.DispatchID != "unresolved-dispatch" {
		t.Fatal("disposal claimed Bitcoin quiescence")
	}
	for _, object := range []client.Object{ledger, target} {
		object.SetFinalizers(nil)
		must(t, c.Update(ctx, object))
		eventually(t, "released ledger deleted", func() bool { return apierrors.IsNotFound(c.Get(ctx, client.ObjectKeyFromObject(object), object)) })
	}
	// The API accepts only safe Secret names in both aggregate and compiled policy schemas.
	invalid := workerNetwork("workers-one")
	invalid.Name = "invalid-secret"
	invalid.Spec.StacksTransactionProduction.CredentialsSecret = "../foreign"
	if err := c.Create(ctx, invalid); !apierrors.IsInvalid(err) {
		t.Fatalf("invalid credential reference admitted: %v", err)
	}
}

// workerNetwork is a public, non-executing fixture: no signing Secrets or live RPC service exists.
func workerNetwork(namespace string) *api.StacksNetwork {
	n := &api.StacksNetwork{ObjectMeta: metav1.ObjectMeta{Name: "same-name", Namespace: namespace}, Spec: api.StacksNetworkSpec{
		Defaults:     api.NetworkDefaults{BitcoinImage: "bitcoin:test", StacksNodeImage: "stacks:test", StacksSignerImage: "stacks:test", DependencyImage: "busybox:test", ImagePullPolicy: core.PullIfNotPresent},
		BitcoinNodes: []api.BitcoinNodeTemplate{{Name: "bitcoin", Config: api.ConfigSource{Generated: &api.GeneratedConfig{Profile: "bitcoin-regtest/v1"}}}},
	}}
	n.Spec.BitcoinBlockProduction = &bitcoin.ProductionPolicy{CredentialsSecret: "bitcoin-key", Targets: []bitcoin.ProductionTarget{{Name: "bitcoin", Weight: 1, Address: "bcrt1qexample0000000000000000000000000000000"}}, IntervalSeconds: 60}
	n.Spec.StacksNodes = []api.StacksNodeTemplate{{Name: "ingress", Role: "signer-node", BitcoinNodeRef: "bitcoin", Config: api.ConfigSource{Generated: &api.GeneratedConfig{Profile: "nakamoto-regtest-node/v1"}}}}
	digest := "sha256:" + strings.Repeat("a", 64)
	n.Spec.Signers = []api.StacksSignerTemplate{{Name: "signer", NodeRef: "ingress", Index: 0, Weight: 1, PublicKey: "0279be667ef9dcbbac55a06295ce870b07029bfcdb2dce28d959f2815b16f81798", Config: api.ConfigSource{SecretRef: &api.ConfigObjectRef{Name: "signer-config", Key: "signer.toml", ExpectedDigest: digest}}}}
	n.Spec.StacksTransactionProduction = &stacks.TransferPolicy{CredentialsSecret: "transfer-key", Target: "ingress", Sender: "ST" + strings.Repeat("A", 30), Recipient: "ST" + strings.Repeat("B", 30), AmountMicroSTX: 1, FeeMicroSTX: 1000, IntervalSeconds: 60}
	ref := func(name string) stacks.ArtifactReference {
		return stacks.ArtifactReference{Name: name, Key: "key.json", Digest: digest}
	}
	n.Spec.Operation = &api.NetworkOperation{ContractSets: []stacks.ContractSetPolicy{{Name: "contracts", Account: "deployer", Contracts: []stacks.ContractArtifact{{Name: "example", ClarityVersion: 3, SourceRef: ref("sources")}}}}, Participants: []stacks.StackingPolicy{{Name: "signer", Signer: "signer", HolderAccount: "holder", AdministratorAccount: "admin", ConsensusKeySecretRef: ref("consensus"), AmountMicroSTX: 100000000000, LockCycles: 12}}}
	for i, name := range []string{"deployer", "holder", "admin"} {
		kind, consumer := "StacksStackingParticipant", "signer"
		if i == 0 {
			kind, consumer = "StacksContractSet", "contracts"
		}
		n.Spec.Operation.Accounts = append(n.Spec.Operation.Accounts, stacks.ManagedAccountPolicy{Name: name, Address: "ST" + strings.Repeat(string(rune('C'+i)), 30), Target: "ingress", ConfigDigest: digest, KeySecretRef: ref(name), ConsumerKind: kind, Consumer: consumer})
	}
	return n
}

// must marks setup failures at their call sites.
func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// eventually waits for the real controller queue without assuming immediate cache delivery.
func eventually(t *testing.T, description string, check func() bool) {
	t.Helper()
	until := time.Now().Add(20 * time.Second)
	for time.Now().Before(until) {
		if check() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("timed out: " + description)
}

// updateNetworkWithRetry changes only current parent policy under optimistic concurrency.
func updateNetworkWithRetry(ctx context.Context, c client.Client, n *api.StacksNetwork, change func(*api.StacksNetwork)) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		current := &api.StacksNetwork{}
		if err := c.Get(ctx, client.ObjectKeyFromObject(n), current); err != nil {
			return err
		}
		change(current)
		return c.Update(ctx, current)
	})
}
