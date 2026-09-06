//go:build integration

// Package integration verifies the controllers against a real Kubernetes API server.
package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	actionv1 "github.com/cylewitruk-stacks/stacks-k8s/apis/network/actions/v1alpha1"
	bitcoinv1alpha1 "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha1"
	stacksv1alpha1 "github.com/cylewitruk-stacks/stacks-k8s/apis/network/stacks/v1alpha1"
	networkv1alpha1 "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha1"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/bitcoinnode"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/canonical"
	operatorlabels "github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/labels"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/network"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/stacksnode"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/stackssigner"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/workload"
)

const testNamespace = "operator-integration"

func TestManagerLifecycleAndAPIServerValidation(t *testing.T) {
	testEnvironment := &envtest.Environment{
		CRDDirectoryPaths: []string{filepath.Join(
			"..", "..", "..", "..", "charts", "stacks-network-operator", "crds",
		)},
		ErrorIfCRDPathMissing:       true,
		DownloadBinaryAssets:        true,
		DownloadBinaryAssetsVersion: "1.36",
		BinaryAssetsDirectory:       filepath.Join(os.TempDir(), "stacks-network-operator-envtest"),
		ControlPlaneStartTimeout:    60 * time.Second,
		ControlPlaneStopTimeout:     60 * time.Second,
	}
	configuration, err := testEnvironment.Start()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := testEnvironment.Stop(); err != nil {
			t.Errorf("stop envtest: %v", err)
		}
	})

	scheme := runtime.NewScheme()
	must(t, clientgoscheme.AddToScheme(scheme))
	must(t, networkv1alpha1.AddToScheme(scheme))
	must(t, bitcoinv1alpha1.AddToScheme(scheme))
	must(t, actionv1.AddToScheme(scheme))
	must(t, stacksv1alpha1.AddToScheme(scheme))
	manager, err := ctrl.NewManager(configuration, ctrl.Options{
		Scheme:  scheme,
		Metrics: metricsserver.Options{BindAddress: "0"},
		Cache: cache.Options{DefaultNamespaces: map[string]cache.Config{
			testNamespace: {},
		}},
	})
	must(t, err)
	must(t, (&network.Reconciler{Client: manager.GetClient(), APIReader: manager.GetAPIReader(), Scheme: scheme}).SetupWithManager(manager, 1))
	must(t, (&bitcoinnode.Reconciler{Client: manager.GetClient(), APIReader: manager.GetAPIReader(), Scheme: scheme}).SetupWithManager(manager, 1))
	must(t, (&stacksnode.Reconciler{Client: manager.GetClient(), APIReader: manager.GetAPIReader(), Scheme: scheme}).SetupWithManager(manager, 1))
	must(t, (&stackssigner.Reconciler{Client: manager.GetClient(), APIReader: manager.GetAPIReader(), Scheme: scheme}).SetupWithManager(manager, 1))

	direct, err := client.New(configuration, client.Options{Scheme: scheme})
	must(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	must(t, direct.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: testNamespace}}))
	started := make(chan error, 1)
	go func() { started <- manager.Start(ctx) }()
	if !manager.GetCache().WaitForCacheSync(ctx) {
		t.Fatal("manager cache did not synchronize")
	}
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-started:
			if err != nil {
				t.Errorf("manager stopped: %v", err)
			}
		case <-time.After(10 * time.Second):
			t.Error("manager did not stop")
		}
	})

	verifyGenerationAdmission(t, ctx, direct)
	verifyReorganizationAdmission(t, ctx, direct)
	verifyActionCancellation(t, ctx, direct)

	// A real API server enforces transaction target/sender immutability and ledger retention.
	txNetwork := bitcoinNetwork("transactions")
	txNetwork.Spec.StacksNodes = []networkv1alpha1.StacksNodeTemplate{{Name: "ingress", Role: "follower", BitcoinNodeRef: "bitcoin", Config: generatedStacks()}}
	txNetwork.Spec.StacksTransactionProduction = &stacksv1alpha1.TransferPolicy{Target: "ingress", Sender: "ST1PQHQKV0RJXZFY1DGX8MNSNYVE3VGZJSRTPGZGM", Recipient: "ST2CY5V39NHDPWSXMW9QDT3HC3GD6Q6XX4CFRK9AG", AmountMicroSTX: 1, FeeMicroSTX: 1000, IntervalSeconds: 10}
	must(t, direct.Create(ctx, txNetwork))
	txKey := client.ObjectKeyFromObject(txNetwork)
	txLedger := &stacksv1alpha1.StacksTransactionProduction{}
	eventually(t, "transaction ledger is pinned before ingress readiness", func() bool {
		return direct.Get(ctx, txKey, txLedger) == nil && direct.Get(ctx, txKey, txNetwork) == nil && txNetwork.Status.TransactionProductionUID == string(txLedger.UID) && txLedger.UID != ""
	})
	// The API retains bounded rejection evidence and rejects arbitrary server text.
	txLedger.Status.RejectionReason = "FeeTooLow"
	must(t, direct.Status().Update(ctx, txLedger))
	must(t, direct.Get(ctx, txKey, txLedger))
	if txLedger.Status.RejectionReason != "FeeTooLow" {
		t.Fatal("rejection evidence was pruned")
	}
	invalidRejection := txLedger.DeepCopy()
	invalidRejection.Status.RejectionReason = "unbounded upstream detail"
	if err := direct.Status().Update(ctx, invalidRejection); !apierrors.IsInvalid(err) {
		t.Fatalf("unbounded rejection reason admitted: %v", err)
	}
	changedTx := txNetwork.DeepCopy()
	changedTx.Spec.StacksTransactionProduction.Sender = changedTx.Spec.StacksTransactionProduction.Recipient
	if err := direct.Update(ctx, changedTx); !apierrors.IsInvalid(err) {
		t.Fatalf("transaction sender mutation: %v", err)
	}
	changedChild := txLedger.DeepCopy()
	changedChild.Spec.Policy.Target = "other"
	if err := direct.Update(ctx, changedChild); !apierrors.IsInvalid(err) {
		t.Fatalf("child ingress mutation: %v", err)
	}
	// Removing the public declaration cannot bypass the retained sender/ledger binding.
	oldPolicy := txNetwork.Spec.StacksTransactionProduction.DeepCopy()
	must(t, direct.Get(ctx, txKey, txNetwork))
	txNetwork.Spec.StacksTransactionProduction = nil
	must(t, direct.Update(ctx, txNetwork))
	must(t, direct.Get(ctx, txKey, txNetwork))
	oldPolicy.Sender = oldPolicy.Recipient
	txNetwork.Spec.StacksTransactionProduction = oldPolicy
	must(t, direct.Update(ctx, txNetwork))
	eventually(t, "remove/re-add cannot change the retained transaction sender", func() bool {
		if direct.Get(ctx, txKey, txNetwork) != nil || direct.Get(ctx, txKey, txLedger) != nil {
			return false
		}
		for _, condition := range txNetwork.Status.Conditions {
			if condition.Type == "TransactionsConfigured" && condition.ObservedGeneration == txNetwork.Generation && condition.Reason == "PolicyUnavailable" {
				return txLedger.Spec.Policy.Sender != oldPolicy.Sender && string(txLedger.UID) == txNetwork.Status.TransactionProductionUID
			}
		}
		return false
	})
	txUID := txLedger.UID
	must(t, direct.Delete(ctx, txLedger))
	must(t, direct.Get(ctx, txKey, txNetwork))
	txNetwork.Spec.BitcoinNodes[0].Image = "bitcoin:after-transaction-ledger-deletion"
	must(t, direct.Update(ctx, txNetwork))
	eventually(t, "transaction ledger failure does not block actors", func() bool {
		leaf := &networkv1alpha1.BitcoinNode{}
		return direct.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: "transactions-bitcoin"}, leaf) == nil && leaf.Spec.Image == "bitcoin:after-transaction-ledger-deletion"
	})
	if err := direct.Get(ctx, txKey, &stacksv1alpha1.StacksTransactionProduction{}); !apierrors.IsNotFound(err) {
		t.Fatalf("removed transaction ledger was recreated: %v", err)
	}
	must(t, direct.Get(ctx, txKey, txNetwork))
	if txNetwork.Status.TransactionProductionUID != string(txUID) {
		t.Fatal("transaction ledger UID was reset")
	}
	must(t, direct.Delete(ctx, txNetwork))

	invalid := bitcoinNetwork("invalid")
	invalidProduction := bitcoinNetwork("invalid-production")
	invalidProduction.Spec.BitcoinBlockProduction = &bitcoinv1alpha1.ProductionPolicy{Target: "missing", IntervalSeconds: 5, Address: "mipcBbFg9gMiCh81Kj8tqqdgoZub1ZJRfn"}
	if err := direct.Create(ctx, invalidProduction); !apierrors.IsInvalid(err) {
		t.Fatalf("invalid production target: %v", err)
	}
	productionNetwork := bitcoinNetwork("production")
	productionNetwork.Spec.BitcoinBlockProduction = &bitcoinv1alpha1.ProductionPolicy{Target: "bitcoin", IntervalSeconds: 5, Address: "mipcBbFg9gMiCh81Kj8tqqdgoZub1ZJRfn"}
	must(t, direct.Create(ctx, productionNetwork))
	productionKey := client.ObjectKeyFromObject(productionNetwork)
	productionPolicy := &bitcoinv1alpha1.BitcoinBlockProduction{}
	eventually(t, "aggregate pins production independently of workload readiness", func() bool {
		return direct.Get(ctx, productionKey, productionNetwork) == nil && direct.Get(ctx, productionKey, productionPolicy) == nil && productionPolicy.UID != "" && productionNetwork.Status.BitcoinProductionUID == string(productionPolicy.UID) && !productionNetwork.Status.InventoryReady
	})
	changedTarget := productionPolicy.DeepCopy()
	changedTarget.Spec.Policy.Target = "other"
	if err := direct.Update(ctx, changedTarget); !apierrors.IsInvalid(err) {
		t.Fatalf("production target identity mutation: %v", err)
	}
	immutableLedgerUID := productionPolicy.UID
	// Parent admission rejects direct retargeting before any controller runs.
	changedParent := productionNetwork.DeepCopy()
	otherBitcoin := changedParent.Spec.BitcoinNodes[0]
	otherBitcoin.Name = "other"
	changedParent.Spec.BitcoinNodes = append(changedParent.Spec.BitcoinNodes, otherBitcoin)
	changedParent.Spec.BitcoinBlockProduction.Target = "other"
	if err := direct.Update(ctx, changedParent); !apierrors.IsInvalid(err) {
		t.Fatalf("parent production target mutation: %v", err)
	}
	// Removing and re-adding policy cannot bypass the retained ledger's target.
	must(t, updateNetworkWithRetry(ctx, direct, productionNetwork, func(n *networkv1alpha1.StacksNetwork) { n.Spec.BitcoinBlockProduction = nil }))
	must(t, updateNetworkWithRetry(ctx, direct, productionNetwork, func(n *networkv1alpha1.StacksNetwork) {
		n.Spec.BitcoinNodes = append(n.Spec.BitcoinNodes, otherBitcoin)
		n.Spec.BitcoinBlockProduction = changedParent.Spec.BitcoinBlockProduction.DeepCopy()
	}))
	_, _ = (&network.Reconciler{Client: direct, APIReader: direct, Scheme: scheme, Now: time.Now}).Reconcile(ctx, ctrl.Request{NamespacedName: productionKey})
	must(t, direct.Get(ctx, productionKey, productionPolicy))
	if productionPolicy.UID != immutableLedgerUID || productionPolicy.Spec.Policy.Target != "bitcoin" {
		t.Fatal("remove/re-add changed the retained ledger identity")
	}
	must(t, direct.Delete(ctx, productionPolicy))
	// Reconciliation may run repeatedly; a removed ledger must not acquire fresh authorization.
	for i := 0; i < 5; i++ {
		_, _ = (&network.Reconciler{Client: direct, APIReader: direct, Scheme: scheme, Now: time.Now}).Reconcile(ctx, ctrl.Request{NamespacedName: productionKey})
		if err := direct.Get(ctx, productionKey, &bitcoinv1alpha1.BitcoinBlockProduction{}); !apierrors.IsNotFound(err) {
			t.Fatalf("deleted ledger was recreated: %v", err)
		}
	}
	must(t, direct.Get(ctx, productionKey, productionNetwork))
	if productionNetwork.Status.BitcoinProductionUID != string(immutableLedgerUID) {
		t.Fatal("missing ledger erased the pinned UID")
	}
	must(t, updateNetworkWithRetry(ctx, direct, productionNetwork, func(n *networkv1alpha1.StacksNetwork) {
		n.Spec.BitcoinNodes[0].Image = "bitcoin:updated-after-ledger-deletion"
	}))
	eventually(t, "missing production ledger does not block actor updates", func() bool {
		actor := &networkv1alpha1.BitcoinNode{}
		return direct.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: "production-bitcoin"}, actor) == nil && actor.Spec.Image == "bitcoin:updated-after-ledger-deletion"
	})
	must(t, direct.Delete(ctx, productionNetwork))
	invalid.Spec.StacksNodes = []networkv1alpha1.StacksNodeTemplate{{
		Name: "follower", Role: networkv1alpha1.StacksNodeFollower,
		BitcoinNodeRef: "missing", Config: generatedStacks(),
	}}
	if err := direct.Create(ctx, invalid); !apierrors.IsInvalid(err) {
		t.Fatalf("invalid cross-reference error = %v", err)
	}
	publicSigner := &networkv1alpha1.StacksSigner{ObjectMeta: metav1.ObjectMeta{Name: "public-signer", Namespace: testNamespace}, Spec: networkv1alpha1.StacksSignerSpec{
		NetworkRef: networkv1alpha1.LocalObjectReference{Name: "invalid"}, ActorName: "signer", Image: "signer:test",
		NodeRef: networkv1alpha1.LocalObjectReference{Name: "signer-node"}, Weight: 1,
		Config: networkv1alpha1.ConfigSource{ConfigMapRef: &networkv1alpha1.ConfigObjectRef{Name: "public-config"}},
	}}
	if err := direct.Create(ctx, publicSigner); !apierrors.IsInvalid(err) {
		t.Fatalf("public signer config error = %v", err)
	}
	reservedEnvironment := bitcoinNetwork("reserved-environment")
	reservedEnvironment.Spec.BitcoinNodes[0].Container = &networkv1alpha1.ContainerOverride{Env: map[string]string{"POD_IP": "forged"}}
	if err := direct.Create(ctx, reservedEnvironment); !apierrors.IsInvalid(err) {
		t.Fatalf("reserved environment error = %v", err)
	}
	if err := direct.Create(ctx, bitcoinNetwork("contains.dot")); !apierrors.IsInvalid(err) {
		t.Fatalf("non-DNS-label network name error = %v", err)
	}
	danglingServiceRef := bitcoinNetwork("dangling-service-ref")
	danglingServiceRef.Spec.StacksNodes = []networkv1alpha1.StacksNodeTemplate{{
		Name: "follower", Role: networkv1alpha1.StacksNodeFollower, BitcoinNodeRef: "bitcoin",
		Config: generatedStacks(), ServiceRefs: []string{"missing"},
	}}
	if err := direct.Create(ctx, danglingServiceRef); !apierrors.IsInvalid(err) ||
		!strings.Contains(err.Error(), "Stacks node serviceRefs must name declared actors") || strings.Contains(err.Error(), "no such key") {
		t.Fatalf("dangling service reference error = %v", err)
	}
	invalidActorName := &networkv1alpha1.BitcoinNode{ObjectMeta: metav1.ObjectMeta{Name: "invalid-actor-name", Namespace: testNamespace}, Spec: networkv1alpha1.BitcoinNodeSpec{
		NetworkRef: networkv1alpha1.LocalObjectReference{Name: "invalid"}, ActorName: "Bad/Value",
		Image: "bitcoin:test", Config: networkv1alpha1.ConfigSource{Generated: &networkv1alpha1.GeneratedConfig{Profile: "bitcoin-regtest/v1"}}, RPCPort: 18443, P2PPort: 18444,
	}}
	if err := direct.Create(ctx, invalidActorName); !apierrors.IsInvalid(err) {
		t.Fatalf("invalid actor-name error = %v", err)
	}
	invalidPort := invalidActorName.DeepCopy()
	invalidPort.Name = "invalid-port"
	invalidPort.Spec.ActorName = "bitcoin"
	invalidPort.Spec.RPCPort = 65536
	if err := direct.Create(ctx, invalidPort); !apierrors.IsInvalid(err) {
		t.Fatalf("invalid actor port error = %v", err)
	}
	negativeGrace := int64(-1)
	invalidGrace := &networkv1alpha1.StacksNode{ObjectMeta: metav1.ObjectMeta{Name: "invalid-grace", Namespace: testNamespace}, Spec: networkv1alpha1.StacksNodeSpec{
		NetworkRef: networkv1alpha1.LocalObjectReference{Name: "invalid"}, ActorName: "follower", Role: networkv1alpha1.StacksNodeFollower,
		Image: "stacks:test", BitcoinNodeRef: networkv1alpha1.LocalObjectReference{Name: "bitcoin"}, Config: generatedStacks(),
		Workload: networkv1alpha1.WorkloadSpec{TerminationGracePeriodSeconds: &negativeGrace},
	}}
	if err := direct.Create(ctx, invalidGrace); !apierrors.IsInvalid(err) {
		t.Fatalf("negative termination grace error = %v", err)
	}
	assertLeafDigestRoundTrips(t, ctx, direct)
	assertServiceReplacementRoundTrip(t, ctx, direct, scheme)
	serviceMap := make(map[string]string, 233)
	for index := 0; index < 233; index++ {
		serviceMap[fmt.Sprintf("actor-%03d", index)] = fmt.Sprintf("service-%03d", index)
	}
	oversizedLeaf := &networkv1alpha1.StacksNode{ObjectMeta: metav1.ObjectMeta{Name: "oversized-map", Namespace: testNamespace}, Spec: networkv1alpha1.StacksNodeSpec{
		NetworkRef: networkv1alpha1.LocalObjectReference{Name: "invalid"}, ActorName: "follower", Role: networkv1alpha1.StacksNodeFollower,
		Image: "stacks:test", BitcoinNodeRef: networkv1alpha1.LocalObjectReference{Name: "bitcoin"}, Config: generatedStacks(), ServiceMap: serviceMap,
	}}
	if err := direct.Create(ctx, oversizedLeaf); !apierrors.IsInvalid(err) {
		t.Fatalf("oversized service map error = %v", err)
	}

	networkObject := bitcoinNetwork("lifecycle")
	must(t, direct.Create(ctx, networkObject))
	childKey := types.NamespacedName{Namespace: testNamespace, Name: "lifecycle-bitcoin"}
	eventually(t, "compiled BitcoinNode", func() bool {
		return direct.Get(ctx, childKey, &networkv1alpha1.BitcoinNode{}) == nil
	})
	eventually(t, "declarations published without ready actors", func() bool {
		current := &networkv1alpha1.StacksNetwork{}
		if direct.Get(ctx, client.ObjectKeyFromObject(networkObject), current) != nil {
			return false
		}
		catalog := current.Status.TargetDeclarations
		if catalog == nil || catalog.SchemaVersion != networkv1alpha1.TargetDeclarationsVersion ||
			catalog.NetworkUID != string(current.UID) || catalog.ObservedGeneration != current.Generation || len(catalog.Actors) != 1 {
			return false
		}
		child := &networkv1alpha1.BitcoinNode{}
		if direct.Get(ctx, childKey, child) != nil {
			return false
		}
		digest, err := workload.SpecDigest(child.Spec)
		return err == nil && catalog.Actors[0].SpecDigest == digest &&
			catalog.Actors[0].Name == child.Name && !current.Status.InventoryReady
	})
	staleCatalog := &networkv1alpha1.StacksNetwork{}
	must(t, direct.Get(ctx, client.ObjectKeyFromObject(networkObject), staleCatalog))
	newer := staleCatalog.DeepCopy()
	newer.Labels = map[string]string{"test-catalog-conflict": "newer"}
	must(t, direct.Patch(ctx, newer, client.MergeFrom(staleCatalog)))
	staleStatus := staleCatalog.DeepCopy()
	staleStatus.Status.TargetDeclarations = nil
	if err := direct.Status().Patch(ctx, staleStatus, client.MergeFromWithOptions(staleCatalog, client.MergeFromWithOptimisticLock{})); !apierrors.IsConflict(err) {
		t.Fatalf("stale catalog status patch = %v, want conflict", err)
	}
	eventually(t, "workload resources", func() bool {
		return direct.Get(ctx, childKey, &appsv1.StatefulSet{}) == nil &&
			direct.Get(ctx, childKey, &corev1.Service{}) == nil &&
			direct.Get(ctx, types.NamespacedName{Namespace: testNamespace, Name: "lifecycle-bitcoin-config"}, &corev1.ConfigMap{}) == nil
	})
	service := &corev1.Service{}
	must(t, direct.Get(ctx, childKey, service))
	service.Spec.Type = corev1.ServiceTypeExternalName
	service.Spec.ExternalName = "redirect.example"
	must(t, direct.Update(ctx, service))
	eventually(t, "actor Service routing contract restored", func() bool {
		current := &corev1.Service{}
		return direct.Get(ctx, childKey, current) == nil && current.Spec.Type == corev1.ServiceTypeClusterIP &&
			current.Spec.ClusterIP == corev1.ClusterIPNone && current.Spec.ExternalName == "" && current.Spec.SessionAffinity == corev1.ServiceAffinityNone
	})

	child := &networkv1alpha1.BitcoinNode{}
	must(t, direct.Get(ctx, childKey, child))
	child.Spec.Image = "tampered:latest"
	must(t, direct.Update(ctx, child))
	eventually(t, "aggregate restores owned child", func() bool {
		current := &networkv1alpha1.BitcoinNode{}
		return direct.Get(ctx, childKey, current) == nil && current.Spec.Image == "bitcoin/bitcoin:25.2"
	})

	statefulSet := &appsv1.StatefulSet{}
	must(t, direct.Get(ctx, childKey, statefulSet))
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{
		Name: "lifecycle-bitcoin-0", Namespace: testNamespace, UID: "pod-one",
		Labels: map[string]string{
			"app.kubernetes.io/managed-by": "stacks-network-operator", "network.stacks.org/network": "lifecycle",
			"network.stacks.org/actor": "bitcoin", "network.stacks.org/actor-resource": "lifecycle-bitcoin",
			"network.stacks.org/actor-kind": string(operatorlabels.Bitcoin), appsv1.StatefulSetRevisionLabel: "revision-one",
		},
		OwnerReferences: []metav1.OwnerReference{{
			APIVersion: "apps/v1", Kind: "StatefulSet", Name: statefulSet.Name, UID: statefulSet.UID,
			Controller: pointer(true), BlockOwnerDeletion: pointer(true),
		}},
	}, Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "actor", Image: "bitcoin/bitcoin:25.2"}}}}
	must(t, direct.Create(ctx, pod))
	must(t, direct.Get(ctx, client.ObjectKeyFromObject(pod), pod))
	pod.Status.Conditions = []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}
	pod.Status.ContainerStatuses = []corev1.ContainerStatus{{Name: "actor", ImageID: "docker-pullable://bitcoin@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}
	must(t, direct.Status().Update(ctx, pod))
	var simulatedControllerError error
	eventually(t, "complete admitted inventory", func() bool {
		simulatedControllerError = simulateStatefulSetController(ctx, direct, childKey, "revision-one")
		if simulatedControllerError != nil {
			return false
		}
		current := &networkv1alpha1.StacksNetwork{}
		return direct.Get(ctx, client.ObjectKeyFromObject(networkObject), current) == nil && current.Status.InventoryReady && current.Status.InventoryDigest != "" && len(current.Status.Actors) == 1
	}, func() string {
		currentNetwork := &networkv1alpha1.StacksNetwork{}
		currentChild := &networkv1alpha1.BitcoinNode{}
		currentSet := &appsv1.StatefulSet{}
		currentPod := &corev1.Pod{}
		_ = direct.Get(ctx, client.ObjectKeyFromObject(networkObject), currentNetwork)
		_ = direct.Get(ctx, childKey, currentChild)
		_ = direct.Get(ctx, childKey, currentSet)
		_ = direct.Get(ctx, client.ObjectKeyFromObject(pod), currentPod)
		return fmt.Sprintf("simulatedControllerError=%v network=%+v child=%+v statefulSetStatus=%+v podStatus=%+v", simulatedControllerError, currentNetwork.Status, currentChild.Status, currentSet.Status, currentPod.Status)
	})

	must(t, direct.Get(ctx, client.ObjectKeyFromObject(networkObject), networkObject))
	stableDigest := networkObject.Status.InventoryDigest
	networkObject.Spec.Suspended = true
	must(t, direct.Update(ctx, networkObject))
	eventually(t, "suspension withdraws identity", func() bool {
		currentNetwork := &networkv1alpha1.StacksNetwork{}
		currentSet := &appsv1.StatefulSet{}
		return direct.Get(ctx, client.ObjectKeyFromObject(networkObject), currentNetwork) == nil &&
			direct.Get(ctx, childKey, currentSet) == nil && currentNetwork.Status.Phase == "Suspended" &&
			!currentNetwork.Status.InventoryReady && currentNetwork.Status.InventoryDigest == "" &&
			currentSet.Spec.Replicas != nil && *currentSet.Spec.Replicas == 0
	})
	if stableDigest == "" {
		t.Fatal("ready inventory digest was empty")
	}
}

func assertServiceReplacementRoundTrip(t *testing.T, ctx context.Context, kubeClient client.Client, scheme *runtime.Scheme) {
	t.Helper()
	const namespace = "service-replacement-test"
	must(t, kubeClient.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}))
	storageDisabled := false
	actor := &networkv1alpha1.BitcoinNode{ObjectMeta: metav1.ObjectMeta{Name: "replacement-bitcoin", Namespace: namespace}, Spec: networkv1alpha1.BitcoinNodeSpec{
		NetworkRef: networkv1alpha1.LocalObjectReference{Name: "replacement-network"}, ActorName: "bitcoin",
		Image: "bitcoin:test", Config: networkv1alpha1.ConfigSource{Generated: &networkv1alpha1.GeneratedConfig{Profile: "bitcoin-regtest/v1"}},
		RPCPort: 18443, P2PPort: 18444, DependencyImage: "busybox:test",
		Workload: networkv1alpha1.WorkloadSpec{Storage: &networkv1alpha1.StorageSpec{Enabled: &storageDisabled}},
	}}
	must(t, kubeClient.Create(ctx, actor))
	actorKey := client.ObjectKeyFromObject(actor)
	reconciler := &bitcoinnode.Reconciler{Client: kubeClient, APIReader: kubeClient, Scheme: scheme}
	request := ctrl.Request{NamespacedName: actorKey}
	if _, err := reconciler.Reconcile(ctx, request); err != nil {
		t.Fatalf("create replacement fixture workload: %v", err)
	}

	service := &corev1.Service{}
	must(t, kubeClient.Get(ctx, actorKey, service))
	originalUID := service.UID
	service.Spec.Type = corev1.ServiceTypeExternalName
	service.Spec.ExternalName = "redirect.example"
	must(t, kubeClient.Update(ctx, service))
	must(t, kubeClient.Get(ctx, actorKey, service))
	if service.Spec.Type != corev1.ServiceTypeExternalName || service.Spec.ClusterIP != "" {
		t.Fatalf("API server did not apply ExternalName transition: %#v", service.Spec)
	}
	service.Spec.Type = corev1.ServiceTypeClusterIP
	service.Spec.ExternalName = ""
	must(t, kubeClient.Update(ctx, service))
	must(t, kubeClient.Get(ctx, actorKey, service))
	if service.UID != originalUID || service.Spec.ClusterIP == "" || service.Spec.ClusterIP == corev1.ClusterIPNone {
		t.Fatalf("API server did not allocate a non-headless ClusterIP: uid=%s spec=%#v", service.UID, service.Spec)
	}

	if _, err := reconciler.Reconcile(ctx, request); err == nil {
		t.Fatal("non-headless Service replacement did not report a pending recreation")
	}
	must(t, kubeClient.Get(ctx, actorKey, actor))
	if actor.Status.Phase != "Degraded" || actor.Status.Ready || actor.Status.Identity != nil {
		t.Fatalf("replacement did not withdraw leaf identity: %#v", actor.Status)
	}
	if err := kubeClient.Get(ctx, actorKey, &corev1.Service{}); !apierrors.IsNotFound(err) {
		t.Fatalf("non-headless Service survived identity-safe deletion: %v", err)
	}

	if _, err := reconciler.Reconcile(ctx, request); err != nil {
		t.Fatalf("recreate headless Service: %v", err)
	}
	recreated := &corev1.Service{}
	must(t, kubeClient.Get(ctx, actorKey, recreated))
	if recreated.UID == originalUID || recreated.Spec.Type != corev1.ServiceTypeClusterIP || recreated.Spec.ClusterIP != corev1.ClusterIPNone {
		t.Fatalf("Service was not recreated with a new headless identity: uid=%s spec=%#v", recreated.UID, recreated.Spec)
	}

	statefulSet := &appsv1.StatefulSet{}
	must(t, kubeClient.Get(ctx, actorKey, statefulSet))
	revision := "replacement-revision"
	podLabels := make(map[string]string, len(statefulSet.Spec.Template.Labels)+1)
	for key, value := range statefulSet.Spec.Template.Labels {
		podLabels[key] = value
	}
	podLabels[appsv1.StatefulSetRevisionLabel] = revision
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{
		Name: actor.Name + "-0", Namespace: namespace, Labels: podLabels,
		OwnerReferences: []metav1.OwnerReference{{APIVersion: "apps/v1", Kind: "StatefulSet", Name: statefulSet.Name, UID: statefulSet.UID, Controller: pointer(true), BlockOwnerDeletion: pointer(true)}},
	}, Spec: *statefulSet.Spec.Template.Spec.DeepCopy()}
	must(t, kubeClient.Create(ctx, pod))
	must(t, kubeClient.Get(ctx, client.ObjectKeyFromObject(pod), pod))
	pod.Status.Conditions = []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}
	pod.Status.ContainerStatuses = []corev1.ContainerStatus{{Name: "actor", ImageID: "docker-pullable://bitcoin@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}}
	must(t, kubeClient.Status().Update(ctx, pod))
	must(t, simulateStatefulSetController(ctx, kubeClient, actorKey, revision))
	if _, err := reconciler.Reconcile(ctx, request); err != nil {
		t.Fatalf("readmit leaf after Service recreation: %v", err)
	}
	must(t, kubeClient.Get(ctx, actorKey, actor))
	if actor.Status.Phase != "Ready" || !actor.Status.Ready || actor.Status.Identity == nil || actor.Status.Identity.ServiceName != recreated.Name {
		t.Fatalf("leaf did not recover after Service recreation: %#v", actor.Status)
	}
}

func assertLeafDigestRoundTrips(t *testing.T, ctx context.Context, kubeClient client.Client) {
	t.Helper()
	var fixture struct {
		Contract string `json:"contract"`
		Vectors  []struct {
			ID     string          `json:"id"`
			Kind   string          `json:"kind"`
			Digest string          `json:"digest"`
			Spec   json.RawMessage `json:"spec"`
		} `json:"vectors"`
	}
	content, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "contracts", "leaf-spec-v1.json"))
	must(t, err)
	must(t, json.Unmarshal(content, &fixture))
	if fixture.Contract != "network.stacks.org/leaf-spec/v1" || len(fixture.Vectors) != 7 {
		t.Fatalf("leaf-spec contract = %q, vectors = %d", fixture.Contract, len(fixture.Vectors))
	}
	for _, vector := range fixture.Vectors {
		var object client.Object
		var spec any
		switch vector.Kind {
		case "BitcoinNode":
			value := &networkv1alpha1.BitcoinNode{ObjectMeta: metav1.ObjectMeta{Name: "roundtrip-" + vector.ID, Namespace: testNamespace}}
			must(t, json.Unmarshal(vector.Spec, &value.Spec))
			object, spec = value, value.Spec
		case "StacksNode":
			value := &networkv1alpha1.StacksNode{ObjectMeta: metav1.ObjectMeta{Name: "roundtrip-" + vector.ID, Namespace: testNamespace}}
			must(t, json.Unmarshal(vector.Spec, &value.Spec))
			object, spec = value, value.Spec
		case "StacksSigner":
			value := &networkv1alpha1.StacksSigner{ObjectMeta: metav1.ObjectMeta{Name: "roundtrip-" + vector.ID, Namespace: testNamespace}}
			must(t, json.Unmarshal(vector.Spec, &value.Spec))
			object, spec = value, value.Spec
		default:
			t.Fatalf("unknown leaf kind %q", vector.Kind)
		}
		expected, err := workload.SpecDigest(spec)
		must(t, err)
		if expected != vector.Digest {
			t.Fatalf("%s typed digest = %s, contract records %s", vector.ID, expected, vector.Digest)
		}
		must(t, kubeClient.Create(ctx, object))
		readBack := &unstructured.Unstructured{}
		readBack.SetGroupVersionKind(networkv1alpha1.GroupVersion.WithKind(vector.Kind))
		must(t, kubeClient.Get(ctx, client.ObjectKeyFromObject(object), readBack))
		readSpec, found, err := unstructured.NestedMap(readBack.Object, "spec")
		must(t, err)
		if !found {
			t.Fatalf("%s API-server spec is absent", vector.ID)
		}
		if vector.Kind == "BitcoinNode" {
			if _, exists := readSpec["role"]; exists {
				t.Fatal("Bitcoin API-server spec includes a role")
			}
		}
		actual, err := canonical.Digest(readSpec)
		must(t, err)
		if actual != expected {
			t.Fatalf("%s API-server spec digest = %s, want %s", vector.ID, actual, expected)
		}
	}
}

func bitcoinNetwork(name string) *networkv1alpha1.StacksNetwork {
	return &networkv1alpha1.StacksNetwork{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: testNamespace}, Spec: networkv1alpha1.StacksNetworkSpec{
		Defaults: networkv1alpha1.NetworkDefaults{
			BitcoinImage: "bitcoin/bitcoin:25.2", StacksNodeImage: "unused:latest", StacksSignerImage: "unused:latest",
			DependencyImage: "busybox:1.36.1", ImagePullPolicy: corev1.PullIfNotPresent,
			Workload: networkv1alpha1.WorkloadSpec{Storage: &networkv1alpha1.StorageSpec{Enabled: pointer(false)}},
		},
		BitcoinNodes: []networkv1alpha1.BitcoinNodeTemplate{{
			Name:   "bitcoin",
			Config: networkv1alpha1.ConfigSource{Generated: &networkv1alpha1.GeneratedConfig{Profile: "bitcoin-regtest/v1"}},
		}},
	}}
}

func generatedStacks() networkv1alpha1.ConfigSource {
	return networkv1alpha1.ConfigSource{Generated: &networkv1alpha1.GeneratedConfig{Profile: "nakamoto-regtest-node/v1"}}
}

func eventually(t *testing.T, description string, check func() bool, diagnostics ...func() string) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if check() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	for _, diagnostic := range diagnostics {
		t.Log(diagnostic())
	}
	t.Fatalf("timed out waiting for %s", description)
}

// simulateStatefulSetController advances rollout status that envtest does not
// provide and follows any generation change made by the workload reconciler.
func simulateStatefulSetController(ctx context.Context, kubeClient client.Client, key types.NamespacedName, revision string) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		statefulSet := &appsv1.StatefulSet{}
		if err := kubeClient.Get(ctx, key, statefulSet); err != nil {
			return err
		}
		status := &statefulSet.Status
		if status.Replicas == 1 && status.ReadyReplicas == 1 && status.ObservedGeneration == statefulSet.Generation && status.CurrentRevision == revision && status.UpdateRevision == revision {
			return nil
		}
		status.Replicas = 1
		status.ReadyReplicas = 1
		status.ObservedGeneration = statefulSet.Generation
		status.CurrentRevision = revision
		status.UpdateRevision = revision
		return kubeClient.Status().Update(ctx, statefulSet)
	})
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func pointer[T any](value T) *T { return &value }

// updateNetworkWithRetry reapplies a test spec edit after concurrent controller status writes.
func updateNetworkWithRetry(ctx context.Context, c client.Client, n *networkv1alpha1.StacksNetwork, change func(*networkv1alpha1.StacksNetwork)) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		if err := c.Get(ctx, client.ObjectKeyFromObject(n), n); err != nil {
			return err
		}
		change(n)
		return c.Update(ctx, n)
	})
}
