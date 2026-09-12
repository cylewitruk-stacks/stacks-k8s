//go:build integration

package bitcoincontrol

import (
	"context"
	"testing"

	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/foundation"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/participantstatus"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestControlResourcesAPIConvergenceAndProductionOwnership(t *testing.T) {
	c := controlIntegrationClient(t)
	ctx := context.Background()
	must := func(e error) {
		t.Helper()
		if e != nil {
			t.Fatal(e)
		}
	}
	f := newFixture(t)
	must(c.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "test"}}))
	root := f.root.DeepCopy()
	root.UID = ""
	root.ResourceVersion = ""
	root.Status = api.StacksNetworkStatus{}
	for i := range root.Spec.Participants {
		root.Spec.Participants[i].Definition = api.Definition{Ref: &common.NameRef{Name: "definition"}}
	}
	must(c.Create(ctx, root))
	for _, p := range []*api.StacksNetworkParticipant{f.node, f.production} {
		saved := p.Status.DeepCopy()
		p.UID = ""
		p.ResourceVersion = ""
		p.Spec.NetworkUID = root.UID
		p.OwnerReferences[0].UID = root.UID
		p.Spec.Configuration = saved.Admission.Configuration
		p.Status = api.ParticipantStatus{}
		must(c.Create(ctx, p))
		must(participantstatus.Apply(ctx, c, p, api.ParticipantStatus{Admission: saved.Admission, Conditions: []metav1.Condition{{Type: "Resolved", Status: metav1.ConditionTrue, ObservedGeneration: p.Generation, Reason: "Admitted", Message: "Admitted", LastTransitionTime: metav1.Now()}, {Type: "AdmissionReady", Status: metav1.ConditionTrue, ObservedGeneration: p.Generation, Reason: "RetainedPolicyEligible", Message: "Retained policy eligible", LastTransitionTime: metav1.Now()}}}, participantstatus.AggregateManager))
		if saved.Runtime != nil {
			must(participantstatus.Apply(ctx, c, p, api.ParticipantStatus{Runtime: saved.Runtime}, "stacks-network-domain-bitcoinnode"))
		}
	}
	record := f.record.DeepCopy()
	record.UID = ""
	record.ResourceVersion = ""
	record.Spec.NetworkUID = root.UID
	record.Spec.Participant = binding("StacksNetworkParticipant", f.node)
	record.OwnerReferences[0].UID = root.UID
	must(c.Create(ctx, record))
	genesis := &api.StacksGenesis{}
	must(f.c.Get(ctx, client.ObjectKey{Namespace: "test", Name: "genesis"}, genesis))
	genesis.UID = ""
	genesis.ResourceVersion = ""
	genesis.Spec.Source.NetworkUID = root.UID
	genesis.Spec.Chain.PoX = api.PoX{RewardCycleLength: 20, PrepareLength: 5}
	genesis.Spec.Chain.Allocations = []api.Allocation{}
	genesis.Spec.Chain.Contracts.SourceHashes = map[string]string{}
	genesis.Spec.Bootstrap.Requirements = []api.BootstrapRequirement{}
	genesis.OwnerReferences[0].UID = root.UID
	must(c.Create(ctx, genesis))
	genesisRef := binding("StacksGenesis", genesis)
	genesisRef.Fingerprint = foundation.Digest(genesis.Spec)
	root.Status.GenesisRef = &genesisRef
	root.Status.GenesisDigest = foundation.Digest(genesis.Spec.Chain)
	initial := f.initial.DeepCopy()
	initial.UID = ""
	initial.ResourceVersion = ""
	initial.Spec.NetworkUID = root.UID
	initial.OwnerReferences[0].UID = root.UID
	initial.Spec.Target = record.Spec.Participant
	initial.Spec.Nodes = []common.Binding{record.Spec.Participant}
	initial.Spec.Production = binding("StacksNetworkParticipant", f.production)
	initial.Spec.Genesis = genesisRef
	must(c.Create(ctx, initial))
	initial.Status.Phase = "Preparing"
	initial.Status.Reason = "CadenceArmed"
	must(c.Status().Update(ctx, initial))
	root.Status.Bitcoin = &api.BitcoinRuntimeStatus{ExecutionRefs: []common.Binding{binding("BitcoinExecution", record)}, InitializationRef: initialBinding(initial)}
	must(c.Status().Update(ctx, root))
	counter := &writeCounter{Client: c}
	r := &WorkloadReconciler{Client: counter, Reader: c, Image: "worker:test"}
	req := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(f.node)}
	_, e := r.Reconcile(ctx, req)
	must(e)
	var deployment appsv1.Deployment
	must(c.Get(ctx, client.ObjectKey{Namespace: "test", Name: controlDeploymentName(f.node)}, &deployment))
	generation := deployment.Generation
	counter.patches = 0
	for range 2 {
		_, e = r.Reconcile(ctx, req)
		must(e)
	}
	must(c.Get(ctx, client.ObjectKeyFromObject(&deployment), &deployment))
	if counter.patches != 0 || deployment.Generation != generation {
		t.Fatalf("unchanged API-defaulted worker patched %d times: %s", counter.patches, counter.lastPatch)
	}
	projection := ProductionStatusReconciler{Client: c, Reader: c}
	_, e = projection.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(f.production)})
	must(e)
	must(c.Get(ctx, client.ObjectKeyFromObject(f.production), f.production))
	if f.production.Status.Admission == nil || !meta.IsStatusConditionTrue(f.production.Status.Conditions, "Resolved") || !meta.IsStatusConditionTrue(f.production.Status.Conditions, "WorkloadReady") || !participantstatus.Managed(f.production, ProductionFieldManager) {
		t.Fatal("production SSA failed to preserve admission or establish own readiness")
	}
	stale := record.DeepCopy()
	record.Status.Phase = "Idle"
	must(c.Status().Update(ctx, record))
	stale.Status.Phase = "Blocked"
	if e = c.Status().Update(ctx, stale); !apierrors.IsConflict(e) {
		t.Fatalf("stale execution CAS accepted: %v", e)
	}
	changed := initial.DeepCopy()
	changed.Spec.MinimumHeight++
	if e = c.Update(ctx, changed); !apierrors.IsInvalid(e) {
		t.Fatalf("frozen initialization mutation accepted: %v", e)
	}
}

// initialBinding returns the exact API-assigned scheduler identity.
func initialBinding(initial *bitcoin.BitcoinInitialization) *common.Binding {
	b := binding("BitcoinInitialization", initial)
	return &b
}
