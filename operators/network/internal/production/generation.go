package production

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"time"

	actionv1 "github.com/cylewitruk-stacks/stacks-k8s/apis/network/actions/v1alpha1"
	bitcoinv1 "github.com/cylewitruk-stacks/stacks-k8s/apis/network/bitcoin/v1alpha1"
	networkv1 "github.com/cylewitruk-stacks/stacks-k8s/apis/network/v1alpha1"
	"github.com/cylewitruk-stacks/stacks-k8s/operators/network/internal/generation"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// selectAction reserves only an eligible request; unavailable requests never hold baseline work.
func (r *Reconciler) selectAction(ctx context.Context, p *bitcoinv1.BitcoinBlockProduction, n *networkv1.StacksNetwork) (bool, error) {
	if n.Spec.Suspended || n.Spec.BitcoinBlockProduction == nil {
		return false, nil
	}
	target, err := r.admit(ctx, p, n)
	if err != nil {
		return false, nil
	}

	queue := []client.Object{}
	if r.ActionsEnabled {
		list := &actionv1.BitcoinBlockGenerationList{}
		if err := r.APIReader.List(ctx, list, client.InNamespace(p.Namespace), client.Limit(65)); err != nil {
			return false, err
		}
		if len(list.Items) > 64 || list.Continue != "" {
			return false, nil
		}
		for i := range list.Items {
			queue = append(queue, &list.Items[i])
		}
	}
	if r.ReorganizationEnabled {
		list := &actionv1.BitcoinReorganizationList{}
		if err := r.APIReader.List(ctx, list, client.InNamespace(p.Namespace), client.Limit(65)); err != nil {
			return false, err
		}
		if len(list.Items) > 64 || list.Continue != "" {
			return false, nil
		}
		for i := range list.Items {
			queue = append(queue, &list.Items[i])
		}
	}
	sort.Slice(queue, func(i, j int) bool {
		a, b := queue[i].GetCreationTimestamp(), queue[j].GetCreationTimestamp()
		if a.Equal(&b) {
			return queue[i].GetUID() < queue[j].GetUID()
		}
		return a.Before(&b)
	})
	preflight, cancel := context.WithTimeout(ctx, r.RPCTimeout)
	defer cancel()
	for _, item := range queue {
		if action, ok := item.(*actionv1.BitcoinReorganization); ok {
			selected, err := r.reserveReorganization(preflight, p, n, target, action)
			if selected || err != nil {
				return selected, err
			}
			if preflight.Err() != nil {
				return false, nil
			}
			continue
		}
		a := item.(*actionv1.BitcoinBlockGeneration)
		if a.Spec.NetworkRef.Name != n.Name || a.Spec.BitcoinNodeRef.Name != target.identity.Name || generation.Terminal(a.Status.Phase) || a.Status.AdmittedAt != nil || !a.DeletionTimestamp.IsZero() || !r.Now().Before(a.CreationTimestamp.Add(a.Spec.Timeout.Duration)) {
			continue
		}
		if err := r.RPC.Check(preflight, target.endpoint, a.Spec.Address); err != nil {
			if preflight.Err() != nil {
				return false, nil
			}
			continue
		}
		if err := r.APIReader.Get(ctx, client.ObjectKeyFromObject(n), n); err != nil {
			return false, err
		}
		currentTarget, err := r.admit(ctx, p, n)
		if err != nil || currentTarget != target || n.Spec.Suspended || !n.DeletionTimestamp.IsZero() {
			return false, nil
		}
		current := &actionv1.BitcoinBlockGeneration{}
		if err := r.APIReader.Get(ctx, client.ObjectKeyFromObject(a), current); err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}
			return false, err
		}
		if current.UID != a.UID || !reflect.DeepEqual(current.Spec, a.Spec) || generation.Terminal(current.Status.Phase) || current.Status.AdmittedAt != nil || !current.DeletionTimestamp.IsZero() || !r.Now().Before(current.CreationTimestamp.Add(current.Spec.Timeout.Duration)) {
			continue
		}
		before := p.DeepCopy()
		p.Status.Action = &bitcoinv1.GenerationReservation{ActionReservation: bitcoinv1.ActionReservation{Name: a.Name, UID: string(a.UID), Generation: a.Generation, AdmittedAt: metav1.NewTime(r.Now().UTC()), ExpiresAt: metav1.NewTime(a.CreationTimestamp.Add(a.Spec.Timeout.Duration)), Network: actionv1.NetworkIdentity{Name: n.Name, UID: string(n.UID), ObservedGeneration: n.Generation}, Target: target.identity, Policy: actionv1.PolicyIdentity{UID: string(p.UID), Generation: p.Generation, Digest: r.ConfigDigest}, CorrelationID: a.Labels["actions.stacks.org/correlation-id"]}, Spec: a.Spec}
		p.Status.Phase, p.Status.Message = "Reserved", "Finite generation reserved the shared Bitcoin executor"
		return true, r.Status().Patch(ctx, p, client.MergeFromWithOptions(before, client.MergeFromWithOptimisticLock{}))
	}
	return false, nil
}

// reconcileAction maintains one reservation through dispatch, receipt accounting and status acknowledgement.
func (r *Reconciler) reconcileAction(ctx context.Context, p *bitcoinv1.BitcoinBlockProduction, n *networkv1.StacksNetwork) (ctrl.Result, error) {
	record := p.Status.Action
	if !r.ActionsEnabled {
		return r.report(ctx, p, "Blocked", "Restore the action-enabled executor to resolve its retained reservation", 0)
	}
	a := &actionv1.BitcoinBlockGeneration{}
	err := r.APIReader.Get(ctx, client.ObjectKey{Namespace: p.Namespace, Name: record.Name}, a)
	if err != nil && !apierrors.IsNotFound(err) {
		return ctrl.Result{}, err
	}
	same := err == nil && string(a.UID) == record.UID && reflect.DeepEqual(a.Spec, record.Spec)
	if p.Status.DispatchState == "Armed" {
		if phase := r.collectors.phase(p.Status.DispatchID, p.UID); phase != "" {
			return r.report(ctx, p, phase, "Finite generation receipt remains under collection/accounting", time.Second)
		}
		if !record.EffectUncertain {
			before := p.DeepCopy()
			record.EffectUncertain = true
			if err := r.Status().Patch(ctx, p, client.MergeFromWithOptions(before, client.MergeFromWithOptimisticLock{})); err != nil {
				return ctrl.Result{}, err
			}
		}
		return r.report(ctx, p, "Blocked", "Unresolved finite generation retains target exclusion", time.Second)
	}
	if !same || (generation.Terminal(a.Status.Phase) && a.Status.BlocksGenerated == record.BlocksGenerated && a.Status.LastDispatchID == record.LastDispatchID) {
		before := p.DeepCopy()
		p.Status.Action = nil
		p.Status.Phase, p.Status.Message = "Running", "Finite action reservation released; latest baseline will be evaluated"
		return ctrl.Result{RequeueAfter: time.Millisecond}, r.Status().Patch(ctx, p, client.MergeFromWithOptions(before, client.MergeFromWithOptimisticLock{}))
	}
	if record.BlocksGenerated < record.Spec.Count && record.StopReason == "" && (!a.DeletionTimestamp.IsZero() || !r.Now().Before(record.ExpiresAt.Time)) {
		reason := "DeadlineExceeded"
		if !a.DeletionTimestamp.IsZero() {
			reason = "ActionCancelled"
		}
		return r.stopAction(ctx, p, reason)
	}
	if generation.Terminal(a.Status.Phase) || !a.DeletionTimestamp.IsZero() || !r.Now().Before(record.ExpiresAt.Time) || record.BlocksGenerated >= record.Spec.Count || record.StopReason != "" {
		return r.report(ctx, p, "Reserved", "Waiting for action outcome and receipt acknowledgement", time.Second)
	}
	if !controllerutil.ContainsFinalizer(a, generation.Finalizer) || a.Status.AdmittedAt == nil || !reflect.DeepEqual(a.Status.AdmittedTarget, &record.Target) || a.Status.AdmittedPolicy == nil || a.Status.AdmittedPolicy.UID != string(p.UID) {
		return r.report(ctx, p, "Reserved", "Waiting for durable action admission and finalizer", time.Second)
	}
	target, err := r.admitTarget(ctx, p, n, false)
	if err != nil || n.Spec.Suspended {
		return r.report(ctx, p, "Reserved", "Waiting for current admitted target identity", time.Second)
	}
	if target.identity != record.Target || !p.DeletionTimestamp.IsZero() {
		return r.stopAction(ctx, p, "IdentityDiverged")
	}
	if last := record.LastCompletedAt; last != nil {
		remaining := last.Add(time.Duration(record.Spec.IntervalSeconds) * time.Second).Sub(r.Now())
		if remaining > 0 {
			return r.report(ctx, p, "Reserved", "Waiting for the next finite generation interval", min(remaining, time.Second))
		}
	}
	preflight, cancel := context.WithTimeout(ctx, r.RPCTimeout)
	err = r.RPC.Check(preflight, target.endpoint, record.Spec.Address)
	cancel()
	if err != nil {
		if errors.Is(err, errInvalidPreflight) {
			return r.stopAction(ctx, p, "MechanismFailed")
		}
		return r.report(ctx, p, "Reserved", "Waiting for read-only RPC preflight", time.Second)
	}
	// Re-read both request and target after preflight, immediately before the atomic arm.
	if err := r.APIReader.Get(ctx, client.ObjectKeyFromObject(a), a); err != nil {
		return ctrl.Result{RequeueAfter: time.Second}, client.IgnoreNotFound(err)
	}
	if string(a.UID) != record.UID || !reflect.DeepEqual(a.Spec, record.Spec) || generation.Terminal(a.Status.Phase) || !a.DeletionTimestamp.IsZero() || !r.Now().Before(record.ExpiresAt.Time) {
		return ctrl.Result{RequeueAfter: time.Millisecond}, nil
	}
	if err := r.APIReader.Get(ctx, client.ObjectKeyFromObject(n), n); err != nil {
		return ctrl.Result{}, err
	}
	current, err := r.admitTarget(ctx, p, n, false)
	if err != nil || n.Spec.Suspended {
		return r.report(ctx, p, "Reserved", "Waiting for current admitted target identity", time.Second)
	}
	if current != target || !n.DeletionTimestamp.IsZero() {
		return r.stopAction(ctx, p, "IdentityDiverged")
	}
	if err := ctx.Err(); err != nil {
		return ctrl.Result{}, err
	}
	before := p.DeepCopy()
	p.Status.DispatchState = "Armed"
	p.Status.DispatchID = fmt.Sprintf("%s/%s/%s", p.UID, r.ProcessNonce, p.ResourceVersion)
	if !r.collectors.reserve(p.Status.DispatchID, p.UID) {
		p.Status = before.Status
		return r.report(ctx, p, "Reserved", "Receipt capacity unavailable or executor draining", time.Second)
	}
	if record.StartedAt == nil {
		now := metav1.NewTime(r.Now().UTC())
		record.StartedAt = &now
	}
	p.Status.TargetUID, p.Status.PodUID, p.Status.ContainerID = target.actorUID, target.podUID, target.containerID
	p.Status.Phase, p.Status.Message = "Collecting", "Collecting the finite action block receipt"
	if err := r.Status().Patch(ctx, p, client.MergeFromWithOptions(before, client.MergeFromWithOptimisticLock{})); err != nil {
		r.collectors.release(p.Status.DispatchID)
		return ctrl.Result{}, err
	}
	armed := p.DeepCopy()
	armed.Spec.Policy.Address = record.Spec.Address
	r.collectors.collect(armed, target.endpoint)
	return ctrl.Result{RequeueAfter: time.Second}, nil
}

// stopAction records an immutable stop fact for the lifecycle controller to classify.
func (r *Reconciler) stopAction(ctx context.Context, p *bitcoinv1.BitcoinBlockProduction, reason string) (ctrl.Result, error) {
	if p.Status.Action.StopReason == reason {
		return ctrl.Result{RequeueAfter: time.Second}, nil
	}
	before := p.DeepCopy()
	p.Status.Action.StopReason = reason
	return ctrl.Result{RequeueAfter: time.Second}, r.Status().Patch(ctx, p, client.MergeFromWithOptions(before, client.MergeFromWithOptimisticLock{}))
}
