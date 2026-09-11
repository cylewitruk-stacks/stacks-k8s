//go:build integration

// Package foundationintegration tests the foundation against a real Kubernetes API server.
package foundationintegration

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	stacks "github.com/cylewitruk-stacks/stacks-k8s/apis/network/stacks/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/foundation"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/yaml"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

func TestFoundationSchemasAndFreeze(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	env := &envtest.Environment{CRDDirectoryPaths: []string{filepath.Join("..", "..", "..", "..", "charts", "stacks-network-foundation", "crds")}, ErrorIfCRDPathMissing: true, DownloadBinaryAssets: true, DownloadBinaryAssetsVersion: "1.37.0", BinaryAssetsDirectory: filepath.Join(os.TempDir(), "stacks-network-operator-envtest"), ControlPlaneStartTimeout: 60 * time.Second, ControlPlaneStopTimeout: 60 * time.Second}
	cfg, err := env.Start()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := env.Stop(); err != nil {
			t.Error(err)
		}
	})
	scheme := runtime.NewScheme()
	for _, add := range []func(*runtime.Scheme) error{clientgoscheme.AddToScheme, api.AddToScheme, bitcoin.AddToScheme, stacks.AddToScheme} {
		if err := add(scheme); err != nil {
			t.Fatal(err)
		}
	}
	c, err := client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		t.Fatal(err)
	}
	verifyManagerAndScopedJob(t, ctx, c, cfg, scheme)
	// The complete proposed cohort must survive schema admission without silent field pruning.
	path := filepath.Join("..", "..", "..", "..", "docs", "design", "public-api", "examples", "30-actors.yaml")
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	decoder := yaml.NewYAMLOrJSONDecoder(file, 4096)
	count := 0
	for {
		var raw json.RawMessage
		err := decoder.Decode(&raw)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if len(raw) == 0 {
			continue
		}
		obj := &unstructured.Unstructured{}
		if err := json.Unmarshal(raw, &obj.Object); err != nil {
			t.Fatal(err)
		}
		if obj.GetKind() == "" {
			continue
		}
		if err := c.Create(ctx, obj, client.FieldValidation("Strict")); err != nil {
			t.Fatalf("create %s/%s: %v", obj.GetKind(), obj.GetName(), err)
		}
		count++
	}
	t.Logf("strictly admitted %d objects", count)
	ns := "lab-30"
	key := types.NamespacedName{Namespace: ns, Name: "network"}
	request := ctrl.Request{NamespacedName: key}
	operatorConfig := rest.CopyConfig(cfg)
	operatorConfig.Impersonate = rest.ImpersonationConfig{UserName: "system:serviceaccount:foundation-permissions:foundation"}
	operatorClient, err := client.New(operatorConfig, client.Options{Scheme: scheme})
	if err != nil {
		t.Fatal(err)
	}
	root := &foundation.Reconciler{Client: operatorClient, Reader: operatorClient, Scheme: scheme}
	account := &foundation.IdentityReconciler{Client: operatorClient, Reader: operatorClient, Scheme: scheme, Image: "foundation:test"}
	wallet := &foundation.IdentityReconciler{Client: operatorClient, Reader: operatorClient, Scheme: scheme, Image: "foundation:test", Wallet: true}
	var network api.StacksNetwork
	step := func() {
		t.Helper()
		for i := 0; i < 8; i++ {
			if _, err := root.Reconcile(ctx, request); err != nil {
				t.Fatal(err)
			}
		}
		var accounts stacks.StacksAccountList
		if err := c.List(ctx, &accounts, client.InNamespace(ns)); err != nil {
			t.Fatal(err)
		}
		for _, a := range accounts.Items {
			if _, err := account.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(&a)}); err != nil {
				t.Fatal(err)
			}
		}
		var wallets bitcoin.BitcoinWalletList
		if err := c.List(ctx, &wallets, client.InNamespace(ns)); err != nil {
			t.Fatal(err)
		}
		for _, w := range wallets.Items {
			if _, err := wallet.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(&w)}); err != nil {
				t.Fatal(err)
			}
		}
		// envtest has no kubelet/Job controller. Execute the exact resolver entry point with its recorded binding.
		var jobs batchv1.JobList
		if err := c.List(ctx, &jobs, client.InNamespace(ns)); err != nil {
			t.Fatal(err)
		}
		for _, job := range jobs.Items {
			var input foundation.KeyJobInput
			for _, arg := range job.Spec.Template.Spec.Containers[0].Args {
				if strings.HasPrefix(arg, "--input=") {
					if err := json.Unmarshal([]byte(strings.TrimPrefix(arg, "--input=")), &input); err != nil {
						t.Fatal(err)
					}
				}
			}
			if err := foundation.RunKeyJob(ctx, c, input); err != nil {
				t.Fatalf("resolver %s: %v", job.Name, err)
			}
		}
		if err := c.Get(ctx, key, &network); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 80; i++ {
		step()
		if network.Status.InputDigest != "" {
			break
		}
		if i%10 == 0 {
			t.Logf("pass %d: %s %+v", i, network.Status.Phase, meta.FindStatusCondition(network.Status.Conditions, "Resolved"))
		}
	}
	if network.Status.InputDigest == "" {
		t.Fatalf("resolution did not converge: %+v", network.Status)
	}
	if network.Status.GenesisRef != nil || network.Status.Phase != "Resolving" || !meta.IsStatusConditionTrue(network.Status.Conditions, "Resolved") {
		t.Fatalf("paused root froze or wrong phase: %+v", network.Status)
	}
	var artifacts api.StacksGenesisList
	if err := c.List(ctx, &artifacts, client.InNamespace(ns)); err != nil || len(artifacts.Items) != 0 {
		t.Fatalf("paused artifacts: %v %d", err, len(artifacts.Items))
	}
	// Chain inputs and union selections are rejected at admission, before reconciliation.
	invalid := network.DeepCopy()
	invalid.Spec.Profile = "other"
	if err := c.Update(ctx, invalid); !apierrors.IsInvalid(err) {
		t.Fatalf("profile mutation accepted: %v", err)
	}
	invalid = network.DeepCopy()
	invalid.Spec.Participants[0].Kind = "StacksNode"
	invalid.Spec.Participants[0].Definition = api.Definition{Inline: &api.Configuration{BitcoinNode: &bitcoin.BitcoinNodeSpec{}}}
	if err := c.Update(ctx, invalid); !apierrors.IsInvalid(err) {
		t.Fatalf("wrong inline branch accepted: %v", err)
	}
	invalid = network.DeepCopy()
	invalid.Spec.Participants[0].Kind = "StacksNode"
	invalid.Spec.Participants[0].Definition = api.Definition{Ref: invalid.Spec.Participants[0].Definition.Ref}
	if err := c.Update(ctx, invalid); !apierrors.IsInvalid(err) || !strings.Contains(err.Error(), "kind is immutable") {
		t.Fatalf("kind transition was not rejected by its admission rule: %v", err)
	}
	initialSpec := *network.Spec.DeepCopy()
	network.Spec.Operation = "Running"
	if err := c.Update(ctx, &network); err != nil {
		t.Fatal(err)
	}
	verifyFreshFreezeReads(t, ctx, c, root, request, &network)
	for i := 0; i < 80; i++ {
		step()
		if network.Status.GenesisRef != nil {
			break
		}
	}
	if network.Status.GenesisRef == nil {
		t.Fatalf("freeze failed: %+v", network.Status)
	}
	if network.Status.Phase != "Initializing" || meta.IsStatusConditionTrue(network.Status.Conditions, "Running") {
		t.Fatalf("runtime falsely claimed: %+v", network.Status)
	}
	var genesis api.StacksGenesis
	if err := c.Get(ctx, types.NamespacedName{Namespace: ns, Name: network.Status.GenesisRef.Name}, &genesis); err != nil {
		t.Fatal(err)
	}
	if len(genesis.Spec.Bootstrap.Requirements) != 40 || len(genesis.Spec.Chain.Allocations) != 19 {
		t.Fatalf("cohort/allocations: %d/%d", len(genesis.Spec.Bootstrap.Requirements), len(genesis.Spec.Chain.Allocations))
	}
	var supply uint64
	for _, a := range genesis.Spec.Chain.Allocations {
		var n uint64
		fmt.Sscan(string(a.AmountMicroSTX), &n)
		supply += n
	}
	if supply != 17012000000000000 {
		t.Fatalf("supply %d", supply)
	}
	copy := genesis.DeepCopy()
	copy.Spec.Chain.PoX.PrepareLength++
	if err := c.Update(ctx, copy); !apierrors.IsInvalid(err) {
		t.Fatalf("genesis mutation accepted: %v", err)
	}
	var pods corev1.PodList
	c.List(ctx, &pods, client.InNamespace(ns))
	if len(pods.Items) != 0 {
		t.Fatal("foundation unexpectedly created Pods")
	}
	// Root status loss after creation recovers the same immutable artifact, without a second genesis.
	originalUID := genesis.UID
	network.Status.GenesisRef = nil
	network.Status.GenesisDigest = ""
	if err := c.Status().Update(ctx, &network); err != nil {
		t.Fatal(err)
	}
	// Publication recovery must not claim that subsequently edited intent was validated.
	network.Spec.Participants = append(network.Spec.Participants, api.Participant{Name: "unresolved-during-recovery", Kind: "BitcoinNode", Definition: api.Definition{Ref: &common.NameRef{Name: "absent"}}})
	updateObject(t, ctx, c, &network)
	if _, err := root.Reconcile(ctx, request); err != nil {
		t.Fatal(err)
	}
	if err := c.Get(ctx, key, &network); err != nil {
		t.Fatal(err)
	}
	recovered := meta.FindStatusCondition(network.Status.Conditions, "Resolved")
	if recovered == nil || recovered.Reason != "GenesisRecovered" || recovered.Status != metav1.ConditionUnknown || recovered.ObservedGeneration != network.Generation || network.Status.GenesisRef.UID != originalUID {
		t.Fatalf("recovery claimed new intent resolved: %+v", network.Status)
	}
	network.Spec.Participants = network.Spec.Participants[:len(network.Spec.Participants)-1]
	updateObject(t, ctx, c, &network)
	for i := 0; i < 10; i++ {
		step()
		if network.Status.GenesisRef != nil {
			break
		}
	}
	if network.Status.GenesisRef == nil || network.Status.GenesisRef.UID != originalUID {
		t.Fatal("lost publication did not recover same artifact")
	}
	// A transient definition read must not leave a recovered participant's public status stuck false.
	targetName := "signer-node-01"
	fault := &failDefinitionRead{Reader: operatorClient, name: targetName}
	root.Client = &failDefinitionClient{Client: operatorClient, fault: fault}
	if _, err := root.Reconcile(ctx, request); err != nil {
		t.Fatal(err)
	}
	var participant api.StacksNetworkParticipant
	participantKey := types.NamespacedName{Namespace: ns, Name: foundation.ParticipantName(string(network.UID), targetName)}
	if err := c.Get(ctx, participantKey, &participant); err != nil {
		t.Fatal(err)
	}
	if !meta.IsStatusConditionFalse(participant.Status.Conditions, "Resolved") {
		t.Fatal("read failure did not reach participant status")
	}
	root.Client = operatorClient
	for i := 0; i < 5; i++ {
		step()
		if err := c.Get(ctx, participantKey, &participant); err != nil {
			t.Fatal(err)
		}
		if meta.IsStatusConditionTrue(participant.Status.Conditions, "Resolved") {
			break
		}
	}
	if !meta.IsStatusConditionTrue(participant.Status.Conditions, "Resolved") {
		t.Fatal("recovery left stale unresolved condition")
	}
	verifyFrozenPolicyUpdates(t, ctx, c, root, request, &network, &genesis)
	verifyLateSignerAttachments(t, ctx, c, root, request, &network)
	verifyRetainedDependencyFailure(t, ctx, c, root, request, &network)
	// Removing a participant is destructive; its name cannot be used again.
	removed := network.Spec.Participants[len(network.Spec.Participants)-1]
	network.Spec.Participants = network.Spec.Participants[:len(network.Spec.Participants)-1]
	if err := c.Update(ctx, &network); err != nil {
		t.Fatal(err)
	}
	step()
	var removedObject api.StacksNetworkParticipant
	err = c.Get(ctx, types.NamespacedName{Namespace: ns, Name: foundation.ParticipantName(string(network.UID), removed.Name)}, &removedObject)
	if !apierrors.IsNotFound(err) {
		t.Fatalf("removed instance retained: %v", err)
	}
	network.Spec.Participants = append(network.Spec.Participants, removed)
	if err := c.Update(ctx, &network); err != nil {
		t.Fatal(err)
	}
	step()
	if cond := meta.FindStatusCondition(network.Status.Conditions, "Resolved"); cond == nil || cond.Reason != "NameAlreadyUsed" {
		t.Fatalf("re-add accepted: %+v", network.Status)
	}
	verifyStoppedRemovalAndInstanceLoss(t, ctx, c, scheme)
	verifyResolutionConditions(t, ctx, c, scheme)
	// Deleting the published artifact is catastrophic, never permission to freeze again.
	if err := c.Delete(ctx, &genesis); err != nil {
		t.Fatal(err)
	}
	step()
	if network.Status.Phase != "Failed" {
		t.Fatalf("artifact loss not terminal: %+v", network.Status)
	}
	if err := c.Delete(ctx, &network); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		if _, err := root.Reconcile(ctx, request); err != nil {
			t.Fatal(err)
		}
	}
	if err := c.Get(ctx, key, &network); !apierrors.IsNotFound(err) {
		t.Fatalf("root deletion did not finish: %v", err)
	}
	verifyFundingOptions(t, ctx, c, ns, initialSpec, step, &network)
	verifyProductionReplacement(t, ctx, c, root, request, &network)
	var definitions stacks.StacksNodeList
	if err := c.List(ctx, &definitions, client.InNamespace(ns)); err != nil || len(definitions.Items) == 0 {
		t.Fatal("root deletion removed reusable definitions")
	}
}

type failDefinitionRead struct {
	client.Reader
	name   string
	failed bool
}

func (r *failDefinitionRead) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	if _, ok := obj.(*stacks.StacksNode); ok && key.Name == r.name && !r.failed {
		r.failed = true
		return fmt.Errorf("injected transient API read")
	}
	return r.Reader.Get(ctx, key, obj, opts...)
}

// failDefinitionClient injects a cache/input read failure without changing current-identity reads.
type failDefinitionClient struct {
	client.Client
	fault *failDefinitionRead
}

func (c *failDefinitionClient) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	return c.fault.Get(ctx, key, obj, opts...)
}
