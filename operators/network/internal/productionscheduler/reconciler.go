// Package productionscheduler publishes weighted opportunities without issuing RPCs.
package productionscheduler

import (
	"context"
	"fmt"
	"math/rand/v2"
	"reflect"
	"time"

	bitcoinv1 "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha1"
	networkv1 "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha1"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/naming"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
)

const finalizer = "bitcoin.stacks.org/retain-production-policy"

// Reconciler owns aggregate scheduling and target ledger identities.
type Reconciler struct {
	client.Client
	// APIReader reads current policies and retained identities.
	APIReader client.Reader
	// Now controls cadence anchors.
	Now func() time.Time
	// Draw returns a value in [0, bound), independently for each new opportunity.
	Draw func(bound int) int
}

// SetupWithManager installs a scheduler independent of target RPC workers.
func (r *Reconciler) SetupWithManager(m ctrl.Manager) error {
	if r.Now == nil {
		r.Now = time.Now
	}
	if r.Draw == nil {
		r.Draw = rand.IntN
	}
	return ctrl.NewControllerManagedBy(m).For(&bitcoinv1.BitcoinBlockProduction{}).
		Watches(&networkv1.StacksNetwork{}, handler.EnqueueRequestsFromMapFunc(func(_ context.Context, o client.Object) []ctrl.Request {
			return []ctrl.Request{{NamespacedName: client.ObjectKeyFromObject(o)}}
		})).Complete(r)
}

// Reconcile publishes at most one opportunity, anchored to the current time.
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	p := &bitcoinv1.BitcoinBlockProduction{}
	if err := r.APIReader.Get(ctx, req.NamespacedName, p); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	n := &networkv1.StacksNetwork{}
	err := r.APIReader.Get(ctx, client.ObjectKey{Namespace: p.Namespace, Name: p.Spec.NetworkName}, n)
	if apierrors.IsNotFound(err) || (err == nil && (string(n.UID) != p.Spec.NetworkUID || !n.DeletionTimestamp.IsZero())) {
		if _, err := r.report(ctx, p, "Abandoned", "Owning network removed", 0); err != nil {
			return ctrl.Result{}, err
		}
		base := p.DeepCopy()
		controllerutil.RemoveFinalizer(p, finalizer)
		return ctrl.Result{}, r.Patch(ctx, p, client.MergeFromWithOptions(base, client.MergeFromWithOptimisticLock{}))
	}
	if err != nil {
		return ctrl.Result{}, err
	}
	if !metav1.IsControlledBy(p, n) || n.Status.BitcoinProductionUID != string(p.UID) {
		return r.report(ctx, p, "Waiting", "Policy is not pinned to the owning network", time.Second)
	}
	if !controllerutil.ContainsFinalizer(p, finalizer) {
		base := p.DeepCopy()
		controllerutil.AddFinalizer(p, finalizer)
		return ctrl.Result{RequeueAfter: time.Millisecond}, r.Patch(ctx, p, client.MergeFromWithOptions(base, client.MergeFromWithOptimisticLock{}))
	}
	if !p.DeletionTimestamp.IsZero() {
		return r.report(ctx, p, "Blocked", "Policy retained until network deletion", time.Second)
	}
	if n.Spec.BitcoinBlockProduction == nil {
		p.Status.NextOpportunityAt = nil
		return r.report(ctx, p, "Paused", "New opportunities are paused", time.Second)
	}
	if !reflect.DeepEqual(p.Spec.Policy, *n.Spec.BitcoinBlockProduction) {
		return r.report(ctx, p, "Waiting", "Compiled policy is not current", time.Second)
	}
	newTargets := 0
	for _, target := range p.Spec.Policy.Targets {
		if p.Ledger(target.Name) == nil {
			newTargets++
		}
	}
	if len(p.Status.Targets)+newTargets > 16 {
		return r.report(ctx, p, "Blocked", "Policy exceeds the sixteen retained target identities; remove new targets or use a fresh environment", time.Second)
	}
	// A failed target registration does not stop already pinned targets.
	message := "Weighted policy cadence is active"
	for _, target := range p.Spec.Policy.Targets {
		if err := r.ensureTarget(ctx, p, target.Name); err != nil {
			message = "Some targets are unavailable; their opportunities are not redistributed"
		}
	}
	if n.Spec.Suspended || n.Spec.BitcoinBlockProduction.Paused {
		p.Status.NextOpportunityAt = nil
		return r.report(ctx, p, "Paused", "New opportunities are paused", time.Second)
	}
	now := r.Now()
	interval := time.Duration(p.Spec.Policy.IntervalSeconds) * time.Second
	if interval < time.Second || interval > 24*time.Hour || len(p.Spec.Policy.Targets) == 0 || len(p.Spec.Policy.Targets) > 8 {
		return r.report(ctx, p, "Blocked", "Unsupported policy bounds", time.Second)
	}
	if p.Status.ObservedGeneration != p.Generation || p.Status.NextOpportunityAt == nil {
		next := metav1.NewMicroTime(now.Add(interval))
		p.Status.NextOpportunityAt = &next
		p.Status.ObservedGeneration = p.Generation
		return r.report(ctx, p, "Running", message, interval)
	}
	if now.Before(p.Status.NextOpportunityAt.Time) {
		return r.report(ctx, p, "Running", message, p.Status.NextOpportunityAt.Sub(now))
	}
	index, err := choose(p.Spec.Policy.Targets, r.Draw)
	if err != nil {
		return ctrl.Result{}, err
	}
	next := metav1.NewMicroTime(now.Add(interval))
	p.Status.Opportunities++
	p.Status.NextOpportunityAt = &next
	if record := p.Ledger(p.Spec.Policy.Targets[index].Name); record != nil {
		record.Offered++
		record.Opportunity = &bitcoinv1.ProductionOpportunity{Number: record.Offered, PolicyGeneration: p.Generation, ExpiresAt: next}
	} else {
		p.Status.UnassignedOpportunities++
		p.Status.LastUnassignedTarget = p.Spec.Policy.Targets[index].Name
	}
	return r.report(ctx, p, "Running", message, interval)
}

// choose samples declarations without renormalizing for target availability.
func choose(targets []bitcoinv1.ProductionTarget, draw func(int) int) (int, error) {
	total := 0
	for _, t := range targets {
		if t.Weight < 1 || t.Weight > 1000 {
			return 0, fmt.Errorf("unsupported target weight")
		}
		total += int(t.Weight)
	}
	if total == 0 {
		return 0, fmt.Errorf("empty target set")
	}
	value := draw(total)
	if value < 0 || value >= total {
		return 0, fmt.Errorf("invalid weighted draw")
	}
	for i, t := range targets {
		value -= int(t.Weight)
		if value < 0 {
			return i, nil
		}
	}
	return 0, fmt.Errorf("invalid weighted selection")
}

// ensureTarget creates or updates only the original ledger for a logical target.
func (r *Reconciler) ensureTarget(ctx context.Context, p *bitcoinv1.BitcoinBlockProduction, name string) error {
	record := p.Ledger(name)
	if record == nil && len(p.Status.Targets) >= 16 {
		return fmt.Errorf("retained target limit reached")
	}
	key := client.ObjectKey{Namespace: p.Namespace, Name: naming.Child(p.Spec.NetworkName, name)}
	if record != nil {
		key.Name = record.ResourceName
	}
	t := &bitcoinv1.BitcoinProductionTarget{}
	err := r.APIReader.Get(ctx, key, t)
	if apierrors.IsNotFound(err) && record == nil {
		t = &bitcoinv1.BitcoinProductionTarget{ObjectMeta: metav1.ObjectMeta{Name: key.Name, Namespace: key.Namespace}, Spec: bitcoinv1.BitcoinProductionTargetSpec{NetworkName: p.Spec.NetworkName, NetworkUID: p.Spec.NetworkUID, ProductionUID: string(p.UID), Policy: *p.Spec.Policy.ExecutionPolicy(name)}}
		if err := controllerutil.SetControllerReference(p, t, r.Scheme()); err != nil {
			return err
		}
		if err := r.Create(ctx, t); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	if !metav1.IsControlledBy(t, p) || t.Spec.ProductionUID != string(p.UID) || t.Spec.NetworkUID != p.Spec.NetworkUID || t.Spec.NetworkName != p.Spec.NetworkName || t.Spec.Policy.Target != name || !t.DeletionTimestamp.IsZero() {
		return fmt.Errorf("target ledger identity unavailable")
	}
	if record == nil {
		p.Status.Targets = append(p.Status.Targets, bitcoinv1.TargetLedger{Name: name, ResourceName: t.Name, UID: string(t.UID)})
	} else if !p.Binds(t) {
		return fmt.Errorf("target ledger UID changed")
	}
	desired := p.Spec.Policy.ExecutionPolicy(name)
	if !reflect.DeepEqual(t.Spec.Policy, *desired) {
		base := t.DeepCopy()
		t.Spec.Policy = *desired
		return r.Patch(ctx, t, client.MergeFromWithOptions(base, client.MergeFromWithOptimisticLock{}))
	}
	return nil
}

// report atomically persists scheduling decisions against the original resource version.
func (r *Reconciler) report(ctx context.Context, p *bitcoinv1.BitcoinBlockProduction, phase, message string, wait time.Duration) (ctrl.Result, error) {
	current := &bitcoinv1.BitcoinBlockProduction{}
	if err := r.APIReader.Get(ctx, client.ObjectKeyFromObject(p), current); err != nil {
		return ctrl.Result{}, err
	}
	// Never refresh the decision's CAS base after a concurrent writer changed it.
	if current.ResourceVersion != p.ResourceVersion || current.UID != p.UID {
		return ctrl.Result{}, fmt.Errorf("policy changed during scheduling")
	}
	p.Status.Phase, p.Status.Message = phase, message
	if reflect.DeepEqual(current.Status, p.Status) {
		return ctrl.Result{RequeueAfter: wait}, nil
	}
	return ctrl.Result{RequeueAfter: wait}, r.Status().Patch(ctx, p, client.MergeFromWithOptions(current, client.MergeFromWithOptimisticLock{}))
}
