//go:build integration

package publicintegration

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	stacks "github.com/cylewitruk-stacks/stacks-k8s/apis/network/stacks/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/foundation"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/networkruntime"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/participantstatus"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/stacksworker"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

// TestAllocationPublicationGapCannotFailOrCreateWorker exercises the two real API writes independently.
func TestAllocationPublicationGapCannotFailOrCreateWorker(t *testing.T) {
	environment := &envtest.Environment{CRDDirectoryPaths: []string{filepath.Join("..", "..", "..", "..", "charts", "stacks-network-operator", "crds")}, ErrorIfCRDPathMissing: true, DownloadBinaryAssets: true, DownloadBinaryAssetsVersion: "1.37.0", BinaryAssetsDirectory: filepath.Join(os.TempDir(), "stacks-network-operator-envtest")}
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
	for _, add := range []func(*runtime.Scheme) error{clientgoscheme.AddToScheme, api.AddToScheme, bitcoin.AddToScheme, stacks.AddToScheme} {
		if err := add(scheme); err != nil {
			t.Fatal(err)
		}
	}
	c, err := client.New(config, client.Options{Scheme: scheme})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	verifyArtifactDeletionRetention(t, ctx, c, scheme)
	namespace := "allocation-gap"
	must(c.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}))
	policy := api.Configuration{StacksTransactionProduction: &stacks.StacksTransactionProductionSpec{}}
	root := &api.StacksNetwork{ObjectMeta: metav1.ObjectMeta{Name: "network", Namespace: namespace}, Spec: api.StacksNetworkSpec{Operation: "Running", Participants: []api.Participant{{Name: "traffic", Kind: "StacksTransactionProduction", Definition: api.Definition{Inline: &policy}}}}}
	must(c.Create(ctx, root))
	p := &api.StacksNetworkParticipant{ObjectMeta: metav1.ObjectMeta{Name: foundation.ParticipantName(string(root.UID), "traffic"), Namespace: namespace}, Spec: api.StacksNetworkParticipantSpec{NetworkUID: root.UID, ParticipantName: "traffic", Kind: "StacksTransactionProduction", Configuration: policy}}
	must(controllerutil.SetControllerReference(root, p, scheme))
	must(c.Create(ctx, p))
	resolutions := 0
	profile := stacksworker.Profile{}
	domain := &stacksworker.Reconciler{Client: c, Reader: c, Kind: p.Spec.Kind, ResolveProfile: func(context.Context, *api.StacksNetwork, *api.StacksNetworkParticipant) (stacksworker.Profile, error) {
		resolutions++
		return profile, nil
	}}
	aggregate := &networkruntime.Reconciler{Client: c, Reader: c, Scheme: scheme}
	key := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(p)}
	for range 2 {
		_, err := domain.Reconcile(ctx, key)
		must(err)
		must(c.Get(ctx, client.ObjectKeyFromObject(p), p))
		condition := meta.FindStatusCondition(p.Status.Conditions, "WorkloadReady")
		if condition == nil || condition.Status != metav1.ConditionUnknown || condition.Reason != "AllocationPending" {
			t.Fatalf("allocation gap classified as failure: %+v", condition)
		}
		_, err = aggregate.Reconcile(ctx, root)
		must(err)
		if root.Status.Phase == "Failed" || meta.IsStatusConditionTrue(root.Status.Conditions, "Failed") || len(root.Status.Identities) != 0 {
			t.Fatal("gap latched failure or reconstructed unpublished identity")
		}
	}
	var pods corev1.PodList
	must(c.List(ctx, &pods, client.InNamespace(namespace)))
	if len(pods.Items) != 0 || resolutions != 0 || p.Status.Runtime.WorkerCandidate != nil {
		t.Fatal("worker provisioning began before identity publication")
	}
	// Publication is a separate actual status write, after the participant became visible.
	root.Status.Identities = []api.InstanceIdentity{{Name: "traffic", UID: p.UID}}
	must(c.Status().Update(ctx, root))
	must(c.Get(ctx, client.ObjectKeyFromObject(root), root))
	fact := stacksworker.ProjectSession(root, p, nil, nil, time.Now())
	if fact.Failed || fact.Unknown || fact.Reason != "CandidatePending" {
		t.Fatalf("ledger publication did not release allocation hold: %+v", fact)
	}
	// A minimal public freeze artifact supplies only this worker lifecycle's prerequisite.
	genesis := &api.StacksGenesis{ObjectMeta: metav1.ObjectMeta{Name: "genesis", Namespace: namespace}, Spec: api.StacksGenesisSpec{Chain: api.Chain{Profile: "regtest-pox4-pox5-v1", Epochs: []api.Epoch{}, PoX: api.PoX{RewardCycleLength: 20, PrepareLength: 5}, Allocations: []api.Allocation{}, Contracts: api.ContractBindings{SourceHashes: map[string]string{}}}, Source: api.GenesisSource{NetworkUID: root.UID, InputDigest: foundation.Digest(policy)}, Bootstrap: api.Bootstrap{Gates: []api.Gate{}, Requirements: []api.BootstrapRequirement{}}}}
	must(controllerutil.SetControllerReference(root, genesis, scheme))
	must(c.Create(ctx, genesis))
	root.Status.GenesisRef = &common.Binding{Kind: "StacksGenesis", Name: genesis.Name, UID: genesis.UID, Fingerprint: foundation.Digest(genesis.Spec)}
	root.Status.GenesisDigest = foundation.Digest(genesis.Spec.Chain)
	must(c.Status().Update(ctx, root))
	must(participantstatus.Apply(ctx, c, p, api.ParticipantStatus{Admission: &api.Admission{PolicyDigest: foundation.Digest(policy), Configuration: policy}}, participantstatus.AggregateManager))
	configuration := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "bootstrap", Namespace: namespace}, Immutable: ptr.To(true), Data: map[string]string{"input.json": "{}"}}
	must(c.Create(ctx, configuration))
	keySecret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "key", Namespace: namespace}, Immutable: ptr.To(true)}
	must(c.Create(ctx, keySecret))
	profile = stacksworker.Profile{Image: "worker:lifecycle-test", Configuration: common.Binding{Kind: "ConfigMap", Name: configuration.Name, UID: configuration.UID}, Keys: []stacksworker.KeyMount{{Role: "sender", Secret: common.Binding{Kind: "Secret", Name: keySecret.Name, UID: keySecret.UID}, Key: "privateKey"}}}
	_, err = domain.Reconcile(ctx, key)
	must(err)
	must(c.Get(ctx, client.ObjectKeyFromObject(p), p))
	must(c.List(ctx, &pods, client.InNamespace(namespace)))
	if len(pods.Items) != 1 || p.Status.Runtime.WorkerCandidate == nil || p.Status.Runtime.WorkerCandidate.Pod.UID != pods.Items[0].UID || resolutions != 1 {
		t.Fatal("published admission did not converge to one exact inactive candidate")
	}
	must(c.Get(ctx, client.ObjectKeyFromObject(root), root))
	fact = stacksworker.ProjectSession(root, p, &pods.Items[0], nil, time.Now())
	if fact.Failed || !fact.Changed || fact.Reason != "WorkerBound" {
		t.Fatalf("candidate did not converge to durable binding: %+v", fact)
	}
	must(c.Status().Update(ctx, root))
	must(c.Get(ctx, client.ObjectKeyFromObject(root), root))
	if root.Status.Identities[0].Worker == nil || root.Status.Identities[0].Worker.Pod.UID != pods.Items[0].UID || meta.IsStatusConditionTrue(root.Status.Conditions, "Failed") {
		t.Fatal("published worker binding differs or failure remained latched")
	}
}
