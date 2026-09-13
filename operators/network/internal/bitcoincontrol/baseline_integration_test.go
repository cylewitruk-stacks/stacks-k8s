//go:build integration

package bitcoincontrol

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/foundation"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/participantstatus"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

func TestBaselineAPIAllocationCASAndStatusOwnership(t *testing.T) {
	ctx := context.Background()
	c := controlIntegrationClient(t)
	f := baselineFixture(t)
	must := func(e error) {
		t.Helper()
		if e != nil {
			t.Fatal(e)
		}
	}
	must(c.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "test"}}))
	for i, cadence := range []bitcoin.Cadence{
		{Mode: "Fixed", Interval: ptr.To(common.Duration("999ms"))},
		{Mode: "Fixed", Interval: ptr.To(common.Duration("3601s"))},
		{Mode: "Fixed", Interval: ptr.To(common.Duration("invalid"))},
		{
			Mode:            "Uniform",
			MinimumInterval: ptr.To(common.Duration("3s")),
			MaximumInterval: ptr.To(common.Duration("1s")),
		},
	} {
		schedule := &bitcoin.BitcoinBlockSchedule{
			ObjectMeta: metav1.ObjectMeta{Namespace: "test", Name: fmt.Sprintf("invalid-%d", i)},
			Spec:       bitcoin.BitcoinBlockScheduleSpec{Cadence: cadence},
		}
		if err := c.Create(ctx, schedule); !apierrors.IsInvalid(err) {
			t.Fatalf("invalid cadence admitted: %+v: %v", cadence, err)
		}
	}
	root := f.root.DeepCopy()
	root.UID = ""
	root.ResourceVersion = ""
	root.Status = api.StacksNetworkStatus{}
	root.Spec.Participants = root.Spec.Participants[:2]
	for i := range root.Spec.Participants {
		root.Spec.Participants[i].Definition = api.Definition{Ref: &common.NameRef{Name: "definition"}}
	}
	must(c.Create(ctx, root))
	var wallet bitcoin.BitcoinWallet
	must(f.c.Get(ctx, client.ObjectKey{Namespace: "test", Name: "miner"}, &wallet))
	savedWallet := wallet.Status.DeepCopy()
	wallet.UID = ""
	wallet.ResourceVersion = ""
	wallet.Status = common.ResolutionStatus{}
	must(c.Create(ctx, &wallet))
	wallet.Status = *savedWallet
	wallet.Status.ObservedGeneration = wallet.Generation
	wallet.Status.Identity = &common.PublicIdentity{
		Address:   "ST000000000000000000002AMW42H",
		PublicKey: "0279be667ef9dcbbac55a06295ce870b07029bfcdb2dce28d959f2815b16f81798",
	}
	must(c.Status().Update(ctx, &wallet))
	walletPin := binding("BitcoinWallet", &wallet)
	walletPin.Fingerprint = wallet.Status.Digest
	node, production := f.node.DeepCopy(), f.production.DeepCopy()
	for _, p := range []*api.StacksNetworkParticipant{node, production} {
		saved := p.Status.DeepCopy()
		p.UID = ""
		p.ResourceVersion = ""
		p.Spec.NetworkUID = root.UID
		p.OwnerReferences[0].UID = root.UID
		saved.Admission.Dependencies = []common.Binding{walletPin}
		if p.Spec.Kind == "BitcoinBlockProduction" {
			saved.Admission.Configuration.BitcoinBlockProduction.Targets = ptr.To(
				[]bitcoin.ProductionTarget{{NodeRef: common.NameRef{Name: "bitcoin"}, Weight: 1}},
			)
			saved.Admission.Dependencies = append(
				saved.Admission.Dependencies,
				binding("StacksNetworkParticipant", node),
			)
		}
		saved.Admission.PolicyDigest = foundation.Digest(saved.Admission.Configuration)
		p.Spec.Configuration = saved.Admission.Configuration
		p.Status = api.ParticipantStatus{}
		must(c.Create(ctx, p))
		resolved := metav1.Condition{
			Type:               "Resolved",
			Status:             metav1.ConditionTrue,
			ObservedGeneration: p.Generation,
			Reason:             "Admitted",
			Message:            "Admitted",
			LastTransitionTime: metav1.NewTime(f.now),
		}
		eligible := resolved
		eligible.Type = "AdmissionReady"
		must(
			participantstatus.Apply(
				ctx,
				c,
				p,
				api.ParticipantStatus{Admission: saved.Admission, Conditions: []metav1.Condition{resolved, eligible}},
				participantstatus.AggregateManager,
			),
		)
		if saved.Runtime != nil {
			saved.Runtime.PolicyDigest = saved.Admission.PolicyDigest
			saved.Runtime.ObservedGeneration = p.Generation
			ready := resolved
			ready.Type = "WorkloadReady"
			must(
				participantstatus.Apply(
					ctx,
					c,
					p,
					api.ParticipantStatus{Runtime: saved.Runtime, Conditions: []metav1.Condition{ready}},
					"stacks-network-domain-bitcoinnode",
				),
			)
		}
	}
	genesis := &api.StacksGenesis{}
	must(f.c.Get(ctx, client.ObjectKey{Namespace: "test", Name: "genesis"}, genesis))
	genesis.UID = ""
	genesis.ResourceVersion = ""
	genesis.Spec.Source.NetworkUID = root.UID
	genesis.OwnerReferences[0].UID = root.UID
	genesis.Spec.Chain.PoX = api.PoX{RewardCycleLength: 20, PrepareLength: 5}
	genesis.Spec.Chain.Allocations = []api.Allocation{}
	genesis.Spec.Chain.Contracts.SourceHashes = map[string]string{}
	genesis.Spec.Bootstrap.Requirements = []api.BootstrapRequirement{
		{
			Kind:         "BitcoinNode",
			Participant:  binding("StacksNetworkParticipant", node),
			PolicyDigest: node.Status.Admission.PolicyDigest,
			Dependencies: node.Status.Admission.Dependencies,
		},
		{
			Kind:                  "BitcoinBlockProduction",
			Participant:           binding("StacksNetworkParticipant", production),
			PolicyDigest:          production.Status.Admission.PolicyDigest,
			Dependencies:          production.Status.Admission.Dependencies,
			BitcoinPayoutWallet:   &walletPin,
			BitcoinInitialization: production.Status.Admission.Configuration.BitcoinBlockProduction.Initialization.DeepCopy(),
		},
	}
	must(c.Create(ctx, genesis))
	genesisPin := binding("StacksGenesis", genesis)
	genesisPin.Fingerprint = foundation.Digest(genesis.Spec)
	root.Status.GenesisRef = &genesisPin
	root.Status.GenesisDigest = foundation.Digest(genesis.Spec.Chain)
	root.Status.Identities = []api.InstanceIdentity{
		{Name: "bitcoin", UID: node.UID},
		{Name: "production", UID: production.UID},
	}
	must(c.Status().Update(ctx, root))
	refs, initialRef, err := EnsureRecords(ctx, c, c, c.Scheme(), root)
	must(err)
	if len(refs) != 1 || initialRef == nil {
		t.Fatalf("allocator did not create real records: %+v %+v", refs, initialRef)
	}
	initial := &bitcoin.BitcoinInitialization{}
	must(c.Get(ctx, client.ObjectKey{Namespace: "test", Name: initialRef.Name}, initial))
	if initial.Spec.Genesis != *root.Status.GenesisRef {
		t.Fatal("allocator lost immutable genesis fingerprint")
	}
	_, err = currentGate(ctx, c, root, initial)
	must(err)
	root.Status.Bitcoin = &api.BitcoinRuntimeStatus{ExecutionRefs: refs, InitializationRef: initialRef}
	root.Status.Initialization = &api.InitializationStatus{
		GenesisUID:        genesis.UID,
		GenesisDigest:     root.Status.GenesisDigest,
		GateIndex:         2,
		AuthorizedCeiling: 234,
		Completed:         true,
		Gates: []api.GateObservation{
			{Name: "PrepareBitcoin", CompletedAt: ptr.To(metav1.NewTime(f.now))},
			{Name: "EnrollPoX4", CompletedAt: ptr.To(metav1.NewTime(f.now))},
		},
	}
	must(c.Status().Update(ctx, root))
	initial.Status.PreparedAt = ptr.To(metav1.NewTime(f.now))
	must(c.Status().Update(ctx, initial))
	record := &bitcoin.BitcoinExecution{}
	must(c.Get(ctx, client.ObjectKey{Namespace: "test", Name: refs[0].Name}, record))
	record.Status.Observation = f.readRecord(t).Status.Observation.DeepCopy()
	record.Status.Observation.Target.Participant = binding("StacksNetworkParticipant", node)
	record.Status.Observation.Target.PolicyDigest = node.Status.Admission.PolicyDigest
	record.Status.Observation.Wallets[0].Wallet = walletPin
	must(c.Status().Update(ctx, record))
	draws := 0
	s := &Scheduler{
		Client:    c,
		Reader:    c,
		Now:       func() time.Time { return f.now },
		Draw:      func(int64) int64 { draws++; return 0 },
		Freshness: 10 * time.Second,
	}
	req := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(initial)}
	for range 2 {
		_, err = s.Reconcile(ctx, req)
		must(err)
	}
	must(c.Get(ctx, client.ObjectKeyFromObject(initial), initial))
	stale := initial.DeepCopy()
	f.now = f.now.Add(time.Second)
	lost := false
	s.Client = interceptor.NewClient(
		c,
		interceptor.Funcs{
			SubResourceUpdate: func(
				ctx context.Context,
				c client.Client,
				sub string,
				obj client.Object,
				opts ...client.SubResourceUpdateOption,
			) error {
				err := c.SubResource(sub).Update(ctx, obj, opts...)
				if state, ok := obj.(*bitcoin.BitcoinInitialization); ok && err == nil && !lost &&
					state.Status.Baseline.Stage == "Selected" {
					lost = true
					return errors.New("committed selection response lost")
				}
				return err
			},
		},
	)
	if _, err = s.Reconcile(ctx, req); err == nil || !lost {
		t.Fatalf("did not exercise lost selection response: %v", err)
	}
	stale.Status.Reason = "stale-writer"
	if err = c.Status().Update(ctx, stale); !apierrors.IsConflict(err) {
		t.Fatalf("stale scheduler CAS accepted: %v", err)
	}
	for range 3 {
		_, err = s.Reconcile(ctx, req)
		must(err)
	}
	must(c.Get(ctx, client.ObjectKeyFromObject(initial), initial))
	must(c.Get(ctx, client.ObjectKeyFromObject(production), production))
	if draws != 1 || initial.Status.Baseline.Scheduling.Opportunities != 1 ||
		initial.Status.Baseline.Scheduling.Assigned != 1 {
		t.Fatalf("API retry redrew or double assigned: draws %d status %+v", draws, initial.Status)
	}
	if production.Status.Scheduling == nil || production.Status.Scheduling.Assigned != 1 ||
		production.Status.Admission == nil ||
		!meta.IsStatusConditionTrue(production.Status.Conditions, "Resolved") ||
		!participantstatus.Managed(production, SchedulingFieldManager) {
		t.Fatal("scheduling SSA did not preserve aggregate admission")
	}
	// The same real scheduler slot serializes immutable temporary timing activation.
	f.now = time.Now().UTC().Truncate(time.Second)
	request := &bitcoin.BitcoinBlockScheduleOverride{
		ObjectMeta: metav1.ObjectMeta{Name: "temporary", Namespace: root.Namespace},
		Spec: bitcoin.BitcoinBlockScheduleOverrideSpec{
			NetworkUID:    root.UID,
			ProductionRef: common.NameRef{Name: production.Spec.ParticipantName},
			Schedule: &bitcoin.BitcoinBlockScheduleSpec{
				Cadence: bitcoin.Cadence{Mode: "Fixed", Interval: ptr.To(common.Duration("2s"))},
			},
			Duration: common.Duration("5s"),
		},
	}
	must(c.Create(ctx, request))
	lifecycle := &OverrideReconciler{Client: c, Reader: c, Now: func() time.Time { return f.now }}
	reconcileOverrideRequest(t, lifecycle, request)
	s.Client = c
	_, err = s.Reconcile(ctx, req)
	must(err)
	must(c.Get(ctx, client.ObjectKeyFromObject(initial), initial))
	if initial.Status.Override == nil || initial.Status.Override.Override.UID != request.UID {
		t.Fatal("temporary activation not captured")
	}
	admission := initial.Status.Override.DeepCopy()
	lostPublication := false
	lifecycle.Client = interceptor.NewClient(
		c,
		interceptor.Funcs{
			SubResourcePatch: func(
				ctx context.Context,
				c client.Client,
				sub string,
				obj client.Object,
				patch client.Patch,
				opts ...client.SubResourcePatchOption,
			) error {
				err := c.SubResource(sub).Patch(ctx, obj, patch, opts...)
				if err == nil && !lostPublication {
					lostPublication = true
					return errors.New("lost activation publication")
				}
				return err
			},
		},
	)
	_, err = lifecycle.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(request)})
	if err == nil || !lostPublication {
		t.Fatal("lost activation publication not exercised")
	}
	lifecycle.Client = c
	reconcileOverrideRequest(t, lifecycle, request)
	must(c.Get(ctx, client.ObjectKeyFromObject(request), request))
	if request.Status.Admission == nil || !request.Status.Admission.StartedAt.Equal(&admission.StartedAt) ||
		!request.Status.Admission.ExpiresAt.Equal(&admission.ExpiresAt) {
		t.Fatal("activation restarted after lost response")
	}
	changed := request.DeepCopy()
	changed.Spec.Duration = "6s"
	if err := c.Update(ctx, changed); !apierrors.IsInvalid(err) {
		t.Fatalf("immutable override changed: %v", err)
	}
	f.now = admission.ExpiresAt.Time
	reconcileOverrideRequest(t, lifecycle, request)
	_, err = s.Reconcile(ctx, req)
	must(err)
	must(c.Get(ctx, client.ObjectKeyFromObject(initial), initial))
	if initial.Status.Override != nil {
		t.Fatal("expired slot retained timing authority")
	}
}
