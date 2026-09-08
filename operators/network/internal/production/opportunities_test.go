package production

import (
	"context"
	"fmt"
	bitcoinv1 "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha1"
	networkv1 "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha1"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/workload"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"testing"
	"time"
)

func TestExpiredAndMissedOpportunitiesNeverCatchUp(t *testing.T) {
	f := fixture(t)
	f.now = f.now.Add(6 * time.Second)
	f.reconcile(t, context.Background())
	if got := f.ledger(t).Status; got.OpportunitiesSkipped != 1 || got.LastSkipReason != "Expired" || f.rpc.sends != 0 {
		t.Fatal(got)
	}
	for i := 0; i < 3; i++ {
		f.offer(t)
	}
	f.reconcile(t, context.Background())
	f.reconcile(t, context.Background())
	if got := f.ledger(t).Status; got.OpportunitiesConsumed != 4 || got.OpportunitiesSkipped != 3 || got.BlocksProduced != 1 || f.rpc.sends != 1 {
		t.Fatal(got)
	}
}

func TestOpportunityExpiryDuringPreflightDoesNotArm(t *testing.T) {
	f := fixture(t)
	f.rpc.check = func(context.Context) error { f.now = f.now.Add(6 * time.Second); return nil }
	f.reconcile(t, context.Background())
	if got := f.ledger(t).Status; got.DispatchID != "" || got.LastSkipReason != "Expired" || f.rpc.sends != 0 {
		t.Fatal(got)
	}
}

func TestReservationSkipsBaselineWithoutChangingActionReceipts(t *testing.T) {
	f := fixture(t)
	a := actionFixture(t, f, "finite", 2)
	admitAction(t, f, a)
	f.offer(t)
	f.reconcile(t, context.Background())
	if got := f.ledger(t).Status; got.OpportunitiesSkipped != 2 || got.LastSkipReason != "Reserved" || got.Action == nil || got.Action.BlocksGenerated != 0 || f.rpc.sends != 0 {
		t.Fatal(got)
	}
	f.reconcile(t, context.Background())
	actionStep(t, f, a)
	if a.Status.BlocksGenerated != 1 || f.ledger(t).Status.BlocksProduced != 0 {
		t.Fatal("baseline opportunity changed action accounting")
	}
}

// secondTarget adds another ready actor under the same aggregate and scheduler.
func secondTarget(t *testing.T, f *productionFixture) *bitcoinv1.BitcoinProductionTarget {
	t.Helper()
	ctx := context.Background()
	actor := f.actor.DeepCopy()
	actor.Name = "network-other"
	actor.UID = "other-actor"
	actor.ResourceVersion = ""
	actor.Spec.ActorName = "other"
	digest, err := workload.SpecDigest(actor.Spec)
	if err != nil {
		t.Fatal(err)
	}
	sts := f.sts.DeepCopy()
	sts.Name = actor.Name
	sts.UID = "other-sts"
	sts.ResourceVersion = ""
	sts.OwnerReferences = nil
	pod := f.pod.DeepCopy()
	pod.Name = actor.Name + "-0"
	pod.UID = "other-pod"
	pod.ResourceVersion = ""
	pod.OwnerReferences = nil
	pod.Status.PodIP = "10.0.0.2"
	if err := controllerutil.SetControllerReference(actor, sts, f.r.Scheme()); err != nil {
		t.Fatal(err)
	}
	if err := controllerutil.SetControllerReference(sts, pod, f.r.Scheme()); err != nil {
		t.Fatal(err)
	}
	actor.Status.Identity.ResourceName = actor.Name
	actor.Status.Identity.SpecDigest = digest
	actor.Status.Identity.StatefulSetName = sts.Name
	actor.Status.Identity.StatefulSetUID = string(sts.UID)
	actor.Status.Identity.PodName = pod.Name
	actor.Status.Identity.PodUID = string(pod.UID)
	target := f.policy.DeepCopy()
	target.Name = actor.Name
	target.UID = "other-ledger"
	target.ResourceVersion = ""
	target.Spec.Policy.Target = "other"
	for _, o := range []client.Object{actor, sts, pod, target} {
		if err := f.r.Create(ctx, o); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.r.Get(ctx, client.ObjectKeyFromObject(f.parent), f.parent); err != nil {
		t.Fatal(err)
	}
	f.parent.Spec.BitcoinBlockProduction.Targets = append(f.parent.Spec.BitcoinBlockProduction.Targets, bitcoinv1.ProductionTarget{Name: "other", Weight: 3, Address: target.Spec.Policy.Address})
	if err := f.r.Update(ctx, f.parent); err != nil {
		t.Fatal(err)
	}
	f.parent.Status.TargetDeclarations.Actors = append(f.parent.Status.TargetDeclarations.Actors, networkv1.TargetDeclaration{Kind: "BitcoinNode", Name: actor.Name, ActorName: "other", SpecDigest: digest})
	if err := f.r.Status().Update(ctx, f.parent); err != nil {
		t.Fatal(err)
	}
	if err := f.r.Get(ctx, client.ObjectKeyFromObject(f.root), f.root); err != nil {
		t.Fatal(err)
	}
	f.root.Spec.Policy = *f.parent.Spec.BitcoinBlockProduction.DeepCopy()
	if err := f.r.Update(ctx, f.root); err != nil {
		t.Fatal(err)
	}
	f.root.Status.Targets = append(f.root.Status.Targets, bitcoinv1.TargetLedger{Name: "other", ResourceName: target.Name, UID: string(target.UID), Offered: 1, Opportunity: &bitcoinv1.ProductionOpportunity{Number: 1, PolicyGeneration: f.root.Generation, ExpiresAt: metav1.NewMicroTime(f.now.Add(5 * time.Second))}})
	if err := f.r.Status().Update(ctx, f.root); err != nil {
		t.Fatal(err)
	}
	return target
}

func TestAmbiguousTargetDoesNotBlockOtherTarget(t *testing.T) {
	f := fixture(t)
	other := secondTarget(t, f)
	f.rpc.generate = func(context.Context) error { return fmt.Errorf("lost response") }
	f.reconcile(t, context.Background())
	blocked := f.ledger(t)
	f.offer(t)
	f.reconcile(t, context.Background())
	if f.ledger(t).Status.LastSkipReason != "Outstanding" {
		t.Fatal("unresolved opportunity not recorded")
	}
	f.rpc.generate = nil
	if _, err := f.r.Reconcile(context.Background(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(other)}); err != nil {
		t.Fatal(err)
	}
	f.waitCollectors(t)
	if err := f.r.Get(context.Background(), client.ObjectKeyFromObject(other), other); err != nil {
		t.Fatal(err)
	}
	if other.Status.BlocksProduced != 1 || f.ledger(t).Status.DispatchID != blocked.Status.DispatchID || f.ledger(t).Status.BlocksProduced != 0 || f.rpc.sends != 2 {
		t.Fatal("target isolation or receipt attribution failed")
	}
}
