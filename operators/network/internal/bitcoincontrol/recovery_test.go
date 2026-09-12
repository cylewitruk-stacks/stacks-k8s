package bitcoincontrol

import (
	"context"
	"testing"
	"time"

	bitcoin "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha2"
	common "github.com/cylewitruk-stacks/stacks-k8s/apis/network/common/v1alpha2"
	api "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha2"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/foundation"
	appsv1 "k8s.io/api/apps/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestDetachedWalletIntentSurvivesArmConflict(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.rpc.extraLoaded = []string{"other"}
	p := f.node.DeepCopy()
	p.Status.Admission.Configuration.BitcoinNode.WalletRefs = ptr.To([]common.NameRef{})
	p.Status.Admission.PolicyDigest = foundation.Digest(p.Status.Admission.Configuration)
	p.Status.Runtime.PolicyDigest = p.Status.Admission.PolicyDigest
	if e := f.c.Status().Update(ctx, p); e != nil {
		t.Fatal(e)
	}
	record := f.readRecord(t)
	record.Status.Observation = &bitcoin.BitcoinObservation{Wallets: []bitcoin.BitcoinWalletObservation{{Wallet: common.Binding{Kind: "BitcoinWallet", Name: "detached", UID: "detached-uid"}, Name: "miner"}, {Wallet: common.Binding{Kind: "BitcoinWallet", Name: "other", UID: "other-uid"}, Name: "other"}}}
	if e := f.c.Status().Update(ctx, record); e != nil {
		t.Fatal(e)
	}
	writer := &failingStatusClient{Client: f.c}
	writer.fail.Store(true)
	f.worker.Client = writer
	if e := f.worker.Step(ctx); e == nil {
		t.Fatal("expected arm conflict")
	}
	retained := f.readRecord(t)
	if retained.Status.PendingWalletRemoval == nil || len(retained.Status.Observation.Wallets) != 2 || f.rpc.count() != 0 {
		t.Fatal("detach intent lost between observation and arm")
	}
	writer.fail.Store(false)
	if e := f.worker.Step(ctx); e != nil {
		t.Fatal(e)
	}
	eventually(t, func() bool { return f.readRecord(t).Status.LastReceipt != nil })
	after := f.readRecord(t)
	if after.Status.PendingWalletRemoval != nil || after.Status.LastReceipt.Request.Method != "UnloadWallet" || f.rpc.count() != 1 {
		t.Fatal("retained cleanup was not accounted exactly once")
	}
	if len(after.Status.Observation.Wallets) != 1 {
		t.Fatal("second detached wallet was forgotten")
	}
	if e := f.worker.Step(ctx); e != nil {
		t.Fatal(e)
	}
	eventually(t, func() bool { return len(f.readRecord(t).Status.Observation.Wallets) == 0 })
	if f.rpc.count() != 2 {
		t.Fatal("detached wallets did not settle one unload each")
	}
}

func TestTerminalDrainSurvivesLaterGenerationWithoutWorkerRestart(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	r := &WorkloadReconciler{Client: f.c, Reader: f.c, Image: "worker:test"}
	req := ctrl.Request{NamespacedName: client.ObjectKeyFromObject(f.node)}
	if _, e := r.Reconcile(ctx, req); e != nil {
		t.Fatal(e)
	}
	if e := f.worker.Step(ctx); e != nil {
		t.Fatal(e)
	}
	root := f.root.DeepCopy()
	root.Spec.Operation = "Stopped"
	if e := f.c.Update(ctx, root); e != nil {
		t.Fatal(e)
	}
	if e := f.worker.Step(ctx); e != nil {
		t.Fatal(e)
	}
	if _, e := r.Reconcile(ctx, req); e != nil {
		t.Fatal(e)
	}
	root.Generation += 5
	if e := f.c.Update(ctx, root); e != nil {
		t.Fatal(e)
	}
	ready, e := CheckDrained(ctx, f.c, f.node)
	if e != nil || !ready {
		t.Fatalf("historical closure lost: %v", e)
	}
	if _, e = r.Reconcile(ctx, req); e != nil {
		t.Fatal(e)
	}
	var deployment appsv1.Deployment
	if e = f.c.Get(ctx, client.ObjectKey{Namespace: f.node.Namespace, Name: controlDeploymentName(f.node)}, &deployment); e != nil {
		t.Fatal(e)
	}
	if ptr.Deref(deployment.Spec.Replicas, 1) != 0 {
		t.Fatal("terminal generation change restarted control worker")
	}
}

func TestReplacementProducerRetainsBootstrapAndFunding(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.worker.Now = func() time.Time { return f.now }
	if e := f.worker.Step(ctx); e != nil {
		t.Fatal(e)
	}
	initial := f.readInitial(t)
	initial.Status.LastAccountedOffer = 17
	initial.Status.Funded = []bitcoin.BitcoinFundingCount{{WalletUID: "wallet-uid", Outputs: 2}}
	if e := f.c.Status().Update(ctx, initial); e != nil {
		t.Fatal(e)
	}
	root := f.root.DeepCopy()
	root.Spec.Participants[1].Name = "replacement"
	root.Status.Identities[1] = api.InstanceIdentity{Name: "replacement", UID: "replacement-uid"}
	if e := f.c.Status().Update(ctx, root); e != nil {
		t.Fatal(e)
	}
	root.Spec.Participants[1].Name = "replacement"
	if e := f.c.Update(ctx, root); e != nil {
		t.Fatal(e)
	}
	replacement := f.production.DeepCopy()
	replacement.Name = foundation.ParticipantName(string(root.UID), "replacement")
	replacement.UID = "replacement-uid"
	replacement.ResourceVersion = ""
	replacement.Spec.ParticipantName = "replacement"
	if e := f.c.Create(ctx, replacement); e != nil {
		t.Fatal(e)
	}
	s := f.schedulerFor()
	f.reconcile(t, s)
	after := f.readInitial(t)
	if after.Status.Production == nil || after.Status.Production.UID != replacement.UID || after.Spec.Production.UID != f.production.UID || after.Status.LastAccountedOffer != 17 || after.Status.Funded[0].Outputs != 2 {
		t.Fatalf("replacement lost frozen provenance or progress: %+v", after.Status)
	}
	resources, e := WorkerResources(f.node, f.record, after, "worker:test", 1)
	if e != nil {
		t.Fatal(e)
	}
	role := resources[1].(*rbacv1.Role)
	granted := false
	for _, rule := range role.Rules {
		if len(rule.Resources) == 1 && rule.Resources[0] == "stacksnetworkparticipants" {
			for _, name := range rule.ResourceNames {
				if name == f.production.Name {
					t.Fatal("retired producer retained worker read permission")
				}
				granted = granted || name == replacement.Name
			}
		}
	}
	if !granted {
		t.Fatal("current producer lacks worker read permission")
	}
	// An offer issued by the retired producer remains evidence, but cannot authorize a new send.
	old := &bitcoin.BitcoinBlockOffer{Number: 18, Ceiling: 203, Initialization: binding("BitcoinInitialization", after), Production: after.Spec.Production, ExpiresAt: metav1.NewTime(f.now.Add(time.Minute))}
	after.Status.Offer = old.DeepCopy()
	if e = f.worker.authorizeOffer(ctx, admitted{initialization: after, participant: f.node, root: root}, old); e == nil {
		t.Fatal("retired producer offer authorized")
	}
}

func TestUnavailablePeerCannotDelayFirstCeilingDeadline(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.rpc.height = 203
	if e := f.worker.Step(ctx); e != nil {
		t.Fatal(e)
	}
	initial := f.readInitial(t)
	initial.Spec.Nodes = append([]common.Binding{{Kind: "StacksNetworkParticipant", Name: "unavailable", UID: "unavailable-uid"}}, initial.Spec.Nodes...)
	if e := f.c.Update(ctx, initial); e != nil {
		t.Fatal(e)
	}
	f.reconcile(t, f.schedulerFor())
	observed := f.readInitial(t)
	if observed.Status.FirstCeilingObservedAt == nil || !observed.Status.FirstCeilingObservedAt.Equal(&metav1.Time{Time: f.now}) {
		t.Fatal("earlier unavailable peer postponed fresh target ceiling deadline")
	}
	if observed.Status.PreparedAt != nil {
		t.Fatal("missing peer accepted as converged")
	}
}
