//go:build integration

package integration

import (
	"context"
	"testing"
	"time"

	actionv1 "github.com/cylewitruk-stacks/stacks-k8s/apis/network/actions/v1alpha1"
	bitcoinv1 "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// verifyReorganizationAdmission exercises all protocol opt-ins and immutable structural bounds.
func verifyReorganizationAdmission(t *testing.T, ctx context.Context, c client.Client) {
	t.Helper()
	a := &actionv1.BitcoinReorganization{ObjectMeta: metav1.ObjectMeta{Name: "reorg-schema", Namespace: testNamespace}, Spec: actionv1.BitcoinReorganizationSpec{NetworkRef: actionv1.LocalReference{Name: "network"}, BitcoinNodeRef: actionv1.LocalReference{Name: "network-bitcoin"}, Depth: 2, Address: "mipcBbFg9gMiCh81Kj8tqqdgoZub1ZJRfn", Timeout: metav1.Duration{Duration: time.Minute}, BoundaryPolicy: actionv1.ReorganizationBoundaryPolicy{AllowEpochBoundaryCrossing: true, AllowRewardCycleBoundaryCrossing: true, AllowPreparePhaseBoundaryCrossing: true}}}
	must(t, c.Create(ctx, a))
	changed := a.DeepCopy()
	changed.Spec.Depth = 3
	if err := c.Update(ctx, changed); !apierrors.IsInvalid(err) {
		t.Fatalf("mutable depth admitted: %v", err)
	}
	for name, change := range map[string]func(*actionv1.BitcoinReorganization){
		"depth-zero":  func(a *actionv1.BitcoinReorganization) { a.Spec.Depth = 0 },
		"depth-large": func(a *actionv1.BitcoinReorganization) { a.Spec.Depth = 7 },
		"epoch":       func(a *actionv1.BitcoinReorganization) { a.Spec.BoundaryPolicy.AllowEpochBoundaryCrossing = false },
		"reward": func(a *actionv1.BitcoinReorganization) {
			a.Spec.BoundaryPolicy.AllowRewardCycleBoundaryCrossing = false
		},
		"prepare": func(a *actionv1.BitcoinReorganization) {
			a.Spec.BoundaryPolicy.AllowPreparePhaseBoundaryCrossing = false
		},
		"timeout": func(a *actionv1.BitcoinReorganization) { a.Spec.Timeout.Duration = 11 * time.Minute },
	} {
		t.Run("reorganization "+name, func(t *testing.T) {
			b := a.DeepCopy()
			b.Name = name
			b.ResourceVersion = ""
			b.UID = ""
			change(b)
			if err := c.Create(ctx, b); !apierrors.IsInvalid(err) {
				t.Fatalf("invalid reorganization admitted: %v", err)
			}
		})
	}
	a.Status.Phase = "Recovering"
	a.Status.InvalidationAcknowledged = true
	a.Status.ReplacementBlockHashes = []string{"hash"}
	must(t, c.Status().Update(ctx, a))
	must(t, c.Get(ctx, client.ObjectKeyFromObject(a), a))
	if !a.Status.InvalidationAcknowledged || len(a.Status.ReplacementBlockHashes) != 1 {
		t.Fatal("cleanup evidence pruned")
	}
	a.Status.ReplacementBlockHashes = make([]string, 8)
	if err := c.Status().Update(ctx, a); !apierrors.IsInvalid(err) {
		t.Fatalf("oversized receipt history admitted: %v", err)
	}
	// The served ledger rejects simultaneous reservations across the two kinds.
	ledger := &bitcoinv1.BitcoinProductionTarget{ObjectMeta: metav1.ObjectMeta{Name: "exclusive-ledger", Namespace: testNamespace}, Spec: bitcoinv1.BitcoinProductionTargetSpec{NetworkName: "absent", NetworkUID: "network-uid", ProductionUID: "policy-uid", Policy: bitcoinv1.TargetPolicy{Target: "bitcoin", Address: a.Spec.Address, IntervalSeconds: 1}}}
	must(t, c.Create(ctx, ledger))
	common := bitcoinv1.ActionReservation{Name: a.Name, UID: string(a.UID), Generation: a.Generation, AdmittedAt: metav1.Now(), ExpiresAt: metav1.NewTime(time.Now().Add(time.Minute))}
	ledger.Status.Reorganization = &bitcoinv1.ReorganizationReservation{ActionReservation: common, Spec: a.Spec}
	must(t, c.Status().Update(ctx, ledger))
	must(t, c.Get(ctx, client.ObjectKeyFromObject(ledger), ledger))
	ledger.Status.Action = &bitcoinv1.GenerationReservation{ActionReservation: common, Spec: actionv1.BitcoinBlockGenerationSpec{NetworkRef: a.Spec.NetworkRef, BitcoinNodeRef: a.Spec.BitcoinNodeRef, Count: 1, Cadence: actionv1.GenerationCadence{Mode: "Fixed", IntervalSeconds: 1}, Address: a.Spec.Address, Timeout: a.Spec.Timeout}}
	if err := c.Status().Update(ctx, ledger); !apierrors.IsInvalid(err) {
		t.Fatalf("competing action reservations admitted: %v", err)
	}

}
