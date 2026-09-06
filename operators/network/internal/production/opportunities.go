package production

import (
	"context"
	"fmt"
	"reflect"
	"time"

	bitcoinv1 "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha1"
	networkv1 "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// rootPolicy verifies both retained ownership links with an uncached read.
func (r *Reconciler) rootPolicy(ctx context.Context, t *bitcoinv1.BitcoinProductionTarget, n *networkv1.StacksNetwork) (*bitcoinv1.BitcoinBlockProduction, error) {
	p := &bitcoinv1.BitcoinBlockProduction{}
	if err := r.APIReader.Get(ctx, client.ObjectKey{Namespace: t.Namespace, Name: t.Spec.NetworkName}, p); err != nil {
		return nil, fmt.Errorf("production policy cannot be read")
	}
	if p.Spec.NetworkUID != string(n.UID) || n.Status.BitcoinProductionUID != string(p.UID) || !metav1.IsControlledBy(p, n) || !p.Binds(t) {
		return nil, fmt.Errorf("target ledger is not pinned to the owning policy")
	}
	return p, nil
}

// opportunity returns only a current, unconsumed selection for this target.
func (r *Reconciler) opportunity(ctx context.Context, t *bitcoinv1.BitcoinProductionTarget, n *networkv1.StacksNetwork) (*bitcoinv1.ProductionOpportunity, string, error) {
	p, err := r.rootPolicy(ctx, t, n)
	if err != nil {
		return nil, "", err
	}
	record := p.Ledger(t.Spec.Policy.Target)
	if record.Opportunity == nil || record.Offered <= t.Status.OpportunitiesConsumed {
		return nil, "", nil
	}
	offer := record.Opportunity.DeepCopy()
	if n.Spec.BitcoinBlockProduction == nil || n.Spec.Suspended || !p.DeletionTimestamp.IsZero() || n.Spec.BitcoinBlockProduction.Paused || !reflect.DeepEqual(p.Spec.Policy, *n.Spec.BitcoinBlockProduction) || p.Spec.Policy.Target(t.Spec.Policy.Target) == nil || offer.PolicyGeneration != p.Generation || p.Status.ObservedGeneration != p.Generation {
		return offer, "PolicyChanged", nil
	}
	if !r.Now().Before(offer.ExpiresAt.Time) {
		return offer, "Expired", nil
	}
	return offer, "", nil
}

// consume binds a selection to an arm or skip in the same target status write.
func consume(t *bitcoinv1.BitcoinProductionTarget, offer *bitcoinv1.ProductionOpportunity, reason string) {
	if offer == nil || offer.Number <= t.Status.OpportunitiesConsumed {
		return
	}
	missed := offer.Number - t.Status.OpportunitiesConsumed - 1
	t.Status.OpportunitiesSkipped += missed
	if missed > 0 {
		t.Status.LastSkipReason = "Unobserved"
	}
	t.Status.OpportunitiesConsumed = offer.Number
	if reason != "" {
		t.Status.OpportunitiesSkipped++
		t.Status.LastSkipReason = reason
	}
}

// skip persists a consumed opportunity without changing dispatch or reservation facts.
func (r *Reconciler) skip(ctx context.Context, t *bitcoinv1.BitcoinProductionTarget, offer *bitcoinv1.ProductionOpportunity, reason string) (ctrl.Result, error) {
	base := t.DeepCopy()
	consume(t, offer, reason)
	return ctrl.Result{RequeueAfter: time.Millisecond}, r.Status().Patch(ctx, t, client.MergeFromWithOptions(base, client.MergeFromWithOptimisticLock{}))
}

// consumeReserved preserves an existing skip cause when a new action reserves the target.
func consumeReserved(t *bitcoinv1.BitcoinProductionTarget, offer *bitcoinv1.ProductionOpportunity, reason string) {
	if reason == "" {
		reason = "Reserved"
	}
	consume(t, offer, reason)
}
