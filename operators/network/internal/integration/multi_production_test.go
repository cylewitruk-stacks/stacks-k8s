//go:build integration

package integration

import (
	"context"
	bitcoinv1 "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha1"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/productionscheduler"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"testing"
	"time"
)

// verifyMultiProduction validates list admission and retained target identities against the API server.
func verifyMultiProduction(t *testing.T, ctx context.Context, c client.Client) {
	const namespace = "multi-production"
	must(t, c.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}))
	n := bitcoinNetwork("multi")
	n.Namespace = namespace
	other := n.Spec.BitcoinNodes[0]
	other.Name = "other"
	n.Spec.BitcoinNodes = append(n.Spec.BitcoinNodes, other)
	n.Spec.BitcoinBlockProduction = &bitcoinv1.ProductionPolicy{IntervalSeconds: 5, Targets: []bitcoinv1.ProductionTarget{{Name: "bitcoin", Weight: 1, Address: "mipcBbFg9gMiCh81Kj8tqqdgoZub1ZJRfn"}, {Name: "other", Weight: 3, Address: "mipcBbFg9gMiCh81Kj8tqqdgoZub1ZJRfn"}}}
	for _, mode := range []string{"duplicate", "zero-weight", "too-many", "missing"} {
		invalid := n.DeepCopy()
		invalid.Name = mode
		switch mode {
		case "duplicate":
			invalid.Spec.BitcoinBlockProduction.Targets[1].Name = "bitcoin"
		case "zero-weight":
			invalid.Spec.BitcoinBlockProduction.Targets[1].Weight = 0
		case "too-many":
			for len(invalid.Spec.BitcoinBlockProduction.Targets) < 9 {
				invalid.Spec.BitcoinBlockProduction.Targets = append(invalid.Spec.BitcoinBlockProduction.Targets, invalid.Spec.BitcoinBlockProduction.Targets[0])
			}
		case "missing":
			invalid.Spec.BitcoinBlockProduction.Targets[1].Name = "absent"
		}
		if err := c.Create(ctx, invalid); !apierrors.IsInvalid(err) {
			t.Fatalf("%s admitted: %v", mode, err)
		}
	}
	must(t, c.Create(ctx, n))
	p := &bitcoinv1.BitcoinBlockProduction{ObjectMeta: metav1.ObjectMeta{Name: n.Name, Namespace: namespace}, Spec: bitcoinv1.BitcoinBlockProductionSpec{NetworkName: n.Name, NetworkUID: string(n.UID), Policy: *n.Spec.BitcoinBlockProduction.DeepCopy()}}
	must(t, controllerutil.SetControllerReference(n, p, c.Scheme()))
	must(t, c.Create(ctx, p))
	n.Status.BitcoinProductionUID = string(p.UID)
	must(t, c.Status().Update(ctx, n))
	now := time.Now().Truncate(time.Microsecond)
	r := &productionscheduler.Reconciler{Client: c, APIReader: c, Now: func() time.Time { return now }, Draw: func(int) int { return 0 }}
	step := func() {
		t.Helper()
		_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(p)})
		must(t, err)
		must(t, c.Get(ctx, client.ObjectKeyFromObject(p), p))
	}
	step()
	step()
	if p.Status.NextOpportunityAt == nil || p.Status.NextOpportunityAt.Sub(now) != 5*time.Second {
		t.Fatal("API round trip shortened the policy interval")
	}

	// Bounded accounting fields must survive the API server and reject invalid values.
	p.Status.UnassignedOpportunities = 1
	p.Status.Opportunities = 1
	p.Status.LastUnassignedTarget = "bitcoin"
	must(t, c.Status().Update(ctx, p))
	must(t, c.Get(ctx, client.ObjectKeyFromObject(p), p))
	if p.Status.UnassignedOpportunities != 1 || p.Status.LastUnassignedTarget != "bitcoin" {
		t.Fatal("unassigned selection evidence was pruned")
	}
	invalidStatus := p.DeepCopy()
	invalidStatus.Status.UnassignedOpportunities = -1
	if err := c.Status().Update(ctx, invalidStatus); !apierrors.IsInvalid(err) {
		t.Fatalf("negative unassigned count admitted: %v", err)
	}
	invalidStatus = p.DeepCopy()
	invalidStatus.Status.LastUnassignedTarget = "invalid/target"
	if err := c.Status().Update(ctx, invalidStatus); !apierrors.IsInvalid(err) {
		t.Fatalf("invalid target name admitted: %v", err)
	}

	first := &bitcoinv1.BitcoinProductionTarget{}
	must(t, c.Get(ctx, client.ObjectKey{Namespace: namespace, Name: "multi-bitcoin"}, first))
	uid := first.UID
	changed := first.DeepCopy()
	changed.Spec.Policy.Target = "other"
	if err := c.Update(ctx, changed); !apierrors.IsInvalid(err) {
		t.Fatalf("target ledger retarget admitted: %v", err)
	}
	first.Status.DispatchState = "Armed"
	first.Status.DispatchID = "unknown"
	must(t, c.Status().Update(ctx, first))
	original := n.Spec.BitcoinBlockProduction.DeepCopy()
	update := func(policy *bitcoinv1.ProductionPolicy) {
		n.Spec.BitcoinBlockProduction = policy
		must(t, c.Update(ctx, n))
		p.Spec.Policy = *policy.DeepCopy()
		must(t, c.Update(ctx, p))
		step()
	}
	removed := original.DeepCopy()
	removed.Targets = removed.Targets[1:]
	update(removed)
	update(original)
	must(t, c.Get(ctx, client.ObjectKeyFromObject(first), first))
	if first.UID != uid || first.Status.DispatchID != "unknown" || p.Ledger("bitcoin").UID != string(uid) {
		t.Fatal("remove/re-add reset an unresolved target")
	}
	// Removing the ledger must not silently acquire a replacement UID.
	must(t, c.Delete(ctx, first))
	step()
	if err := c.Get(ctx, client.ObjectKeyFromObject(first), first); !apierrors.IsNotFound(err) {
		t.Fatalf("pinned target recreated: %v", err)
	}
	// A current policy still offers to the other target despite the missing first ledger.
	r.Draw = func(int) int { return 1 }
	now = now.Add(5 * time.Second)
	step()
	if p.Ledger("other").Offered != 1 {
		t.Fatal("missing target blocked other scheduling")
	}
}
