//go:build integration

package integration

import (
	"context"
	"testing"
	"time"

	actionv1 "github.com/cylewitruk-stacks/stacks-k8s/apis/network/actions/v1alpha1"
	bitcoinv1 "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha1"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/actionstatus"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/generation"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/production"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/reorganization"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// verifyActionCancellation uses real deletion metadata and manually schedules both controllers around acknowledgement.
func verifyActionCancellation(t *testing.T, ctx context.Context, c client.Client) {
	// Outside the running topology manager's namespace so only these explicit reconciliations can write.
	const namespace = "action-cancellation"
	must(t, c.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}))
	for _, kind := range []string{"generation", "reorganization"} {
		for _, progress := range []bool{false, true} {
			name := kind + "-idle"
			if progress {
				name += "-receipts"
			}
			t.Run(name, func(t *testing.T) {
				n := bitcoinNetwork(name)
				n.Namespace = namespace
				must(t, c.Create(ctx, n))
				policy := bitcoinv1.ProductionPolicy{Target: "bitcoin", Address: "mipcBbFg9gMiCh81Kj8tqqdgoZub1ZJRfn", IntervalSeconds: 1}
				p := &bitcoinv1.BitcoinBlockProduction{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, Finalizers: []string{"bitcoin.stacks.org/retain-production-ledger"}}, Spec: bitcoinv1.BitcoinBlockProductionSpec{NetworkName: name, NetworkUID: string(n.UID), Policy: policy}}
				must(t, controllerutil.SetControllerReference(n, p, c.Scheme()))
				must(t, c.Create(ctx, p))
				n.Status.BitcoinProductionUID = string(p.UID)
				must(t, c.Status().Update(ctx, n))
				g := &actionv1.BitcoinBlockGeneration{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, Finalizers: []string{actionstatus.Finalizer}}, Spec: actionv1.BitcoinBlockGenerationSpec{NetworkRef: actionv1.LocalReference{Name: name}, BitcoinNodeRef: actionv1.LocalReference{Name: name + "-bitcoin"}, Count: 3, IntervalSeconds: 1, Address: policy.Address, Timeout: metav1.Duration{Duration: time.Minute}}}
				a := &actionv1.BitcoinReorganization{ObjectMeta: g.ObjectMeta, Spec: actionv1.BitcoinReorganizationSpec{NetworkRef: g.Spec.NetworkRef, BitcoinNodeRef: g.Spec.BitcoinNodeRef, Depth: 2, Address: policy.Address, Timeout: g.Spec.Timeout, BoundaryPolicy: actionv1.ReorganizationBoundaryPolicy{AllowEpochBoundaryCrossing: true, AllowRewardCycleBoundaryCrossing: true, AllowPreparePhaseBoundaryCrossing: true}}}
				var object client.Object = g
				if kind == "reorganization" {
					object = a
				}
				must(t, c.Create(ctx, object))
				common := bitcoinv1.ActionReservation{Name: name, UID: string(object.GetUID()), Generation: object.GetGeneration(), AdmittedAt: metav1.Now(), ExpiresAt: metav1.NewTime(object.GetCreationTimestamp().Add(time.Minute)), Network: actionv1.NetworkIdentity{Name: name, UID: string(n.UID), ObservedGeneration: n.Generation}, Policy: actionv1.PolicyIdentity{UID: string(p.UID)}}
				if progress {
					common.StartedAt = common.AdmittedAt.DeepCopy()
					common.BlocksGenerated = 1
					common.LastDispatchID = "accounted"
					common.LastBlockHash = "retained"
					common.LastCompletedAt = common.AdmittedAt.DeepCopy()
				}
				p.Status.DispatchState = "Idle"
				if kind == "generation" {
					p.Status.Action = &bitcoinv1.GenerationReservation{ActionReservation: common, Spec: g.Spec}
				} else {
					p.Status.Reorganization = &bitcoinv1.ReorganizationReservation{ActionReservation: common, Spec: a.Spec, InvalidationAcknowledged: progress, CleanupAcknowledged: progress}
				}
				must(t, c.Status().Update(ctx, p))
				worker := &production.Reconciler{Client: c, APIReader: c, ActionsEnabled: true, ReorganizationEnabled: true, Now: time.Now}
				lifecycle := func() {
					var err error
					if kind == "generation" {
						_, err = (&generation.Reconciler{Client: c, APIReader: c, Now: time.Now}).Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(object)})
					} else {
						_, err = (&reorganization.Reconciler{Client: c, APIReader: c, Now: time.Now}).Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(object)})
					}
					must(t, err)
				}
				execute := func() {
					_, err := worker.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(p)})
					must(t, err)
					must(t, c.Get(ctx, client.ObjectKeyFromObject(p), p))
				}
				lifecycle()
				must(t, c.Get(ctx, client.ObjectKeyFromObject(object), object))
				admittedGeneration := object.GetGeneration()
				must(t, c.Delete(ctx, object))
				must(t, c.Get(ctx, client.ObjectKeyFromObject(object), object))
				if object.GetGeneration() != admittedGeneration+1 || object.GetDeletionTimestamp().IsZero() {
					t.Fatal("expected finalized deletion to advance generation")
				}
				execute()
				execute()
				if p.Status.Action == nil && p.Status.Reorganization == nil {
					t.Fatal("reservation released before cancellation acknowledgement")
				}
				lifecycle()
				must(t, c.Get(ctx, client.ObjectKeyFromObject(object), object))
				status := &g.Status
				if kind == "reorganization" {
					status = &a.Status.BitcoinBlockGenerationStatus
				}
				condition := meta.FindStatusCondition(status.Conditions, "Progressing")
				if status.Phase != "Failed" || condition == nil || condition.Reason != "ActionCancelled" || status.BlocksGenerated != common.BlocksGenerated {
					t.Fatalf("incorrect cancellation evidence: %#v", status)
				}
				if kind == "reorganization" && meta.FindStatusCondition(status.Conditions, "CleanupComplete").Status != metav1.ConditionTrue {
					t.Fatal("known cleanup not preserved")
				}
				execute()
				if p.Status.Action != nil || p.Status.Reorganization != nil {
					t.Fatal("acknowledged cancellation did not release")
				}
				lifecycle()
				if err := c.Get(ctx, client.ObjectKeyFromObject(object), object); !apierrors.IsNotFound(err) {
					t.Fatalf("finalizer retained after acknowledgement: %v", err)
				}
			})
		}
	}
}
